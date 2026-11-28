package update

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	firstHostOperation  = "c37ba427-e20c-4e59-b52a-3180a8116849"
	secondHostOperation = "adf1cf4e-a509-466d-8320-09d0f91f0d23"
)

func TestConfirmedHostUpdateSurvivesLinkLossAndReplaysOnce(t *testing.T) {
	dir := t.TempDir()
	started, finish := make(chan struct{}), make(chan struct{})
	var releases atomic.Int32
	var calls atomic.Int32
	log := slog.New(slog.DiscardHandler)
	opts := HostUpdateOptions{
		Eligible: func() bool { return true },
		Check: func(context.Context, bool) Result {
			return Result{Status: "available", Current: "1.0.0", Offer: &Offer{Version: "1.1.0"}}
		},
		Install: func(ctx context.Context, job *HostJob) error {
			calls.Add(1)
			if err := job.SetStage("downloading"); err != nil {
				return err
			}
			job.ReportDownload(37, 58)
			close(started)
			select {
			case <-finish:
				return job.SetStage("completed")
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	reserve := func() (func(), error) { return func() { releases.Add(1) }, nil }
	svc := NewHostService(t.Context(), dir, "1.0.0", opts, reserve, log)
	t.Cleanup(svc.Close)
	if !svc.Supported() || svc.Check(t.Context(), false).Offer.Version != "1.1.0" {
		t.Fatal("host cannot discover an update")
	}
	link, disconnect := context.WithCancel(t.Context())
	defer disconnect()
	ack, err := svc.Start(strings.ToUpper(firstHostOperation), "1.1.0", func() bool { return link.Err() == nil })
	if err != nil || ack.OperationID != firstHostOperation || ack.Stage != "checking" {
		t.Fatalf("accept = %+v, %v", ack, err)
	}
	disconnect()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("confirmed operation did not survive disconnect")
	}
	progress, err := svc.Status(firstHostOperation)
	if err != nil || progress.Stage != "downloading" || progress.DownloadedBytes != 37 || progress.TotalBytes != 58 {
		t.Fatalf("progress = %+v, %v", progress, err)
	}
	if _, err := svc.Start(secondHostOperation, "1.2.0", func() bool { return true }); !errors.Is(err, ErrBusy) {
		t.Fatalf("parallel attempt: %v", err)
	}
	if _, err := svc.Start(firstHostOperation, "1.2.0", func() bool { return true }); !errors.Is(err, ErrHostUpdateInput) {
		t.Fatalf("rebound receipt to another version: %v", err)
	}
	close(finish)
	<-svc.done
	reopened := NewHostService(t.Context(), dir, "1.1.0", opts, reserve, log)
	t.Cleanup(reopened.Close)
	done, err := reopened.Start(firstHostOperation, "1.1.0", func() bool { return true })
	if err != nil || done.Stage != "completed" || done.DownloadedBytes != 37 || calls.Load() != 1 || releases.Load() != 1 {
		t.Fatalf("replay = %+v, %v, installs=%d releases=%d", done, err, calls.Load(), releases.Load())
	}
	path, err := hostJobPath(dir, firstHostOperation)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("private receipt mode: %v, %v", info, err)
	}
}

func TestHostUpdateRefusesMissingAuthorityAndUnwritableReceipt(t *testing.T) {
	dir := t.TempDir()
	var held, released int
	reserveErr := errors.New("already shutting down")
	var reserveFailure error
	reserve := func() (func(), error) {
		if reserveFailure != nil {
			return nil, reserveFailure
		}
		held++
		return func() { released++ }, nil
	}
	eligible := true
	svc := NewHostService(t.Context(), dir, "1.0.0", HostUpdateOptions{
		Eligible: func() bool { return eligible },
		Check:    func(context.Context, bool) Result { return Result{Status: "error", Message: "private source error"} },
		Install: func(context.Context, *HostJob) error {
			t.Error("a refused operation started installing")
			return errors.New("unexpected install")
		},
	}, reserve, slog.New(slog.DiscardHandler))
	t.Cleanup(svc.Close)
	if got := svc.Check(t.Context(), true); got.Status != "error" || strings.Contains(got.Message, "private") {
		t.Fatalf("failed check = %+v", got)
	}
	allowed := func() bool { return true }
	for _, test := range []struct {
		id, version string
		want        error
	}{
		{"../../outside", "1.1.0", ErrHostUpdateInput},
		{firstHostOperation, "v1.1.0", ErrHostUpdateInput},
		{firstHostOperation, "1.0.0", ErrHostUpdateInput},
	} {
		if _, err := svc.Start(test.id, test.version, allowed); !errors.Is(err, test.want) {
			t.Fatalf("invalid start %q/%q: %v", test.id, test.version, err)
		}
	}
	if _, err := svc.Start(firstHostOperation, "1.1.0", func() bool { return false }); !errors.Is(err, ErrHostUpdateUnauthorized) {
		t.Fatalf("stale link start: %v", err)
	}
	if _, err := svc.Status(firstHostOperation); !errors.Is(err, ErrHostUpdateNotFound) {
		t.Fatalf("refusal recorded an update: %v", err)
	}
	if _, err := svc.Status("../outside"); !errors.Is(err, ErrHostUpdateInput) {
		t.Fatalf("unexpected unsafe operation lookup: %v", err)
	}
	reserveFailure = reserveErr
	if _, err := svc.Start(firstHostOperation, "1.1.0", allowed); !errors.Is(err, reserveErr) {
		t.Fatalf("idle won admission: %v", err)
	}
	reserveFailure = nil
	eligible = false
	if _, err := svc.Start(firstHostOperation, "1.1.0", allowed); !errors.Is(err, ErrHostUpdateUnsupported) {
		t.Fatalf("unsupported executable: %v", err)
	}
	if got := svc.Check(t.Context(), true); got.Status != "unsupported" {
		t.Fatalf("unsupported check = %+v", got)
	}
	eligible = true
	if err := os.WriteFile(filepath.Join(dir, "host-updates"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(firstHostOperation, "1.1.0", allowed); err == nil {
		t.Fatal("accepted an operation without durable state")
	}
	if held != released {
		t.Fatalf("refused operations leaked updater holds: held=%d released=%d", held, released)
	}
}

func TestHostUpdateShutdownJoinsBeforeLosingItsResult(t *testing.T) {
	dir := t.TempDir()
	svc := NewHostService(t.Context(), dir, "1.0.0", HostUpdateOptions{
		Eligible: func() bool { return true },
		Install: func(ctx context.Context, job *HostJob) error {
			if err := job.SetStage("saving"); err != nil {
				return err
			}
			<-ctx.Done()
			return ctx.Err()
		},
	}, func() (func(), error) { return func() {}, nil }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(svc.Close)
	if _, err := svc.Start(firstHostOperation, "1.1.0", func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	svc.Close()
	done, err := svc.Status(firstHostOperation)
	if err != nil || done.Stage != "failed" || done.Error == "" {
		t.Fatalf("shutdown status = %+v, %v", done, err)
	}
	job, err := OpenHostJob(dir, firstHostOperation)
	if err != nil {
		t.Fatal(err)
	}
	if err := job.SetStage("unknown"); err == nil {
		t.Fatal("the operation accepted an invalid stage")
	}
	if err := job.SetStage("installing"); err != nil {
		t.Fatal(err)
	}
	if abandoned, err := svc.Status(firstHostOperation); err != nil || abandoned.Stage != "failed" {
		t.Fatalf("abandoned attempt = %+v, %v", abandoned, err)
	}
	path, _ := hostJobPath(dir, firstHostOperation)
	for _, badReceipt := range []string{`{`, `{"operation_id":"other","version":"1.1.0","stage":"completed"}`} {
		if err := os.WriteFile(path, []byte(badReceipt), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Status(firstHostOperation); err == nil || errors.Is(err, ErrHostUpdateNotFound) {
			t.Fatalf("corrupt receipt treated as missing: %v", err)
		}
	}
}

func TestHostUpdateFailureLeavesASafeReasonForRetry(t *testing.T) {
	for _, stage := range []string{"checking", "downloading", "saving", "installing"} {
		t.Run(stage, func(t *testing.T) {
			svc := NewHostService(t.Context(), t.TempDir(), "1.0.0", HostUpdateOptions{
				Eligible: func() bool { return true },
				Install: func(_ context.Context, job *HostJob) error {
					if err := job.SetStage(stage); err != nil {
						return err
					}
					return errors.New("private install path")
				},
			}, func() (func(), error) { return func() {}, nil }, slog.New(slog.DiscardHandler))
			t.Cleanup(svc.Close)
			if _, err := svc.Start(firstHostOperation, "1.1.0", func() bool { return true }); err != nil {
				t.Fatal(err)
			}
			<-svc.done
			failed, err := svc.Status(firstHostOperation)
			if err != nil || failed.Stage != "failed" || failed.Error == "" || strings.Contains(failed.Error, "private") {
				t.Fatalf("failure at %s cannot be reported safely: %+v, %v", stage, failed, err)
			}
		})
	}
}

func TestHostUpdateCannotPublishAStageItsReceiptCouldNotSave(t *testing.T) {
	path, err := hostJobPath(t.TempDir(), firstHostOperation)
	if err != nil {
		t.Fatal(err)
	}
	// A directory at the receipt destination makes the final publish fail
	// despite successfully creating and syncing this attempt's temp file.
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	job := &HostJob{path: path, info: HostUpdate{OperationID: firstHostOperation, Version: "1.1.0", Stage: "saving"}}
	if err := job.SetStage("restarting"); err == nil || job.Snapshot().Stage != "saving" {
		t.Fatalf("unwritten restart stage leaked into progress: %+v, %v", job.Snapshot(), err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("unpublished private record was not cleaned: %v, %v", entries, err)
	}
}
