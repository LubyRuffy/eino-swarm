package frontend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

type blockedBuildRunner struct {
	scriptRunner
	id string
}

func (r *blockedBuildRunner) Run(ctx context.Context, root, name string, args ...string) error {
	if len(args) >= 2 && args[0] == npmRunArg && args[1] == npmBuildScript {
		marker := filepath.Join(root, ".build-fixture", r.id)
		if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(marker, []byte("building"), 0o644); err != nil {
			return err
		}
		if r.id == "first" {
			for {
				if _, err := os.Stat(filepath.Join(root, ".build-fixture", "release")); err == nil {
					break
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Millisecond):
				}
			}
		}
	}
	return r.scriptRunner.Run(ctx, root, name, args...)
}

func TestFrontendBuildLockHelperProcess(t *testing.T) {
	root := os.Getenv("ZWAI_FRONTEND_BUILD_FIXTURE_ROOT")
	if root == "" {
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	mustWrite(t, filepath.Join(root, ".build-fixture", "entered-"+os.Getenv("ZWAI_FRONTEND_BUILD_FIXTURE_ID")), "started")
	run := &blockedBuildRunner{scriptRunner: scriptRunner{writeIndex: true}, id: os.Getenv("ZWAI_FRONTEND_BUILD_FIXTURE_ID")}
	if err := ensureBundle(ctx, root, run); err != nil {
		t.Fatal(err)
	}
}

func startBuildFixture(t *testing.T, root, id string) (<-chan error, func()) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestFrontendBuildLockHelperProcess$")
	cmd.Env = append(os.Environ(), "ZWAI_FRONTEND_BUILD_FIXTURE_ROOT="+root, "ZWAI_FRONTEND_BUILD_FIXTURE_ID="+id)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-done
	})
	release := func() {
		t.Helper()
		mustWrite(t, filepath.Join(root, ".build-fixture", "release"), "continue")
	}
	return done, release
}

func waitForBuildMarker(t *testing.T, root, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, ".build-fixture", id)); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("build fixture %s did not start", id)
		}
		time.Sleep(time.Millisecond)
	}
}

// Separate shells share node_modules, dist and the stamp even when they use
// different data directories. A data-dir engine lease cannot serialize them.
func TestConcurrentFrontendBuildProcessesBuildTheSharedCheckoutOnce(t *testing.T) {
	root := writeTree(t)
	first, release := startBuildFixture(t, root, "first")
	t.Cleanup(release)
	waitForBuildMarker(t, root, "first")
	second, _ := startBuildFixture(t, root, "second")
	waitForBuildMarker(t, root, "entered-second")
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(root, ".build-fixture", "second")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("two processes entered the shared npm build before the first finished: %v", err)
	}
	release()
	for i, done := range []<-chan error{first, second} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("build process %d failed: %v", i, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("build process %d did not finish", i)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".build-fixture", "second")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the waiting process failed to recheck the completed bundle")
	}
}

func TestWaitingForAnotherFrontendBuildCanBeCancelled(t *testing.T) {
	root := writeTree(t)
	first, release := startBuildFixture(t, root, "first")
	t.Cleanup(release)
	waitForBuildMarker(t, root, "first")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	run := &scriptRunner{writeIndex: true}
	if err := ensureBundle(ctx, root, run); !errors.Is(err, context.DeadlineExceeded) || len(run.calls) != 0 {
		t.Fatalf("waiting cancellation entered npm or lost its cause: err=%v calls=%v", err, run.calls)
	}
	select {
	case err := <-first:
		t.Fatalf("waiting cancellation ended the other build: %v", err)
	default:
	}
	release()
	select {
	case err := <-first:
		if err != nil {
			t.Fatal(fmt.Errorf("independent build failed after waiter cancelled: %w", err))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("independent build did not finish")
	}
}

func TestConcurrentFrontendCallsRecheckTheBundleAfterWaiting(t *testing.T) {
	root := writeTree(t)
	first := make(chan error, 1)
	go func() {
		first <- ensureBundle(t.Context(), root, &blockedBuildRunner{scriptRunner: scriptRunner{writeIndex: true}, id: "first"})
	}()
	waitForBuildMarker(t, root, "first")
	run := &scriptRunner{writeIndex: true}
	second := make(chan error, 1)
	go func() { second <- ensureBundle(t.Context(), root, run) }()
	time.Sleep(20 * time.Millisecond)
	mustWrite(t, filepath.Join(root, ".build-fixture", "release"), "continue")
	for _, done := range []<-chan error{first, second} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("concurrent build did not finish")
		}
	}
	if len(run.calls) != 0 {
		t.Fatalf("waiting caller rebuilt an already fresh bundle: %v", run.calls)
	}
}

func TestWaitingForTheLocalFrontendBuildSlotCanBeCancelled(t *testing.T) {
	release, err := lockBuild(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := lockBuild(ctx, t.TempDir()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("local build wait ignored cancellation: %v", err)
	}
}

func TestFrontendBuildLockFailureStopsNpmAndReleasesItsLocalSlot(t *testing.T) {
	root := writeTree(t)
	mustWrite(t, filepath.Join(root, ".build-lock"), "existing file")
	run := &scriptRunner{writeIndex: true}
	if err := ensureBundle(t.Context(), root, run); err == nil || len(run.calls) != 0 {
		t.Fatalf("failed build lock entered npm: err=%v calls=%v", err, run.calls)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := ensureBundle(ctx, writeTree(t), &scriptRunner{writeIndex: true}); err != nil {
		t.Fatalf("failed lock retained the local build slot: %v", err)
	}
}
