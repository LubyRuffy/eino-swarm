package main

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/lease"
)

// A checkout build failure must be found while the previous engine still
// serves its clients, before any request freezes its work for replacement.
func TestFrontendPreparationFailurePreservesTheServingEngine(t *testing.T) {
	for _, cancellation := range []bool{false, true} {
		name := "build failed"
		if cancellation {
			name = "cancelled while preparing"
		}
		t.Run(name, func(t *testing.T) {
			if cancellation && runtime.GOOS == "windows" {
				t.Skip("Windows does not support sending os.Interrupt")
			}
			dir := t.TempDir()
			holder := startLeaseHolder(t, dir)
			var handoffs atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/engine/restart" {
					handoffs.Add(1)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": dir, "mock": true, "version": "previous build"})
			}))
			defer srv.Close()
			if err := lease.Publish(dir, lease.Record{PID: holder.Process.Pid, URL: srv.URL, Version: "previous build"}); err != nil {
				t.Fatal(err)
			}
			oldLoader, oldLauncher := loadEngineFrontend, launchEngine
			t.Cleanup(func() { loadEngineFrontend, launchEngine = oldLoader, oldLauncher })
			want := errors.New("frontend build fixture failed")
			loadEngineFrontend = func(ctx context.Context) (fs.FS, error) {
				if !cancellation {
					return nil, want
				}
				process, err := os.FindProcess(os.Getpid())
				if err != nil {
					t.Fatal(err)
				}
				if err := process.Signal(os.Interrupt); err != nil {
					t.Fatal(err)
				}
				select {
				case <-ctx.Done():
				case <-time.After(time.Second):
					t.Fatal("frontend preparation did not receive the launcher's cancellation")
				}
				// A completed load cannot override a concurrent cancellation.
				return nil, nil
			}
			launched := false
			launchEngine = func(context.Context, string, string, bool) (string, error) {
				launched = true
				return "", errors.New("unexpected engine launch")
			}
			if cancellation {
				want = context.Canceled
			}
			url, err := ensureEngine(dir, "127.0.0.1:0", true)
			if url != "" || !errors.Is(err, want) || handoffs.Load() != 0 || launched {
				t.Fatalf("preparation failure reached replacement: url=%q err=%v handoffs=%d launched=%v", url, err, handoffs.Load(), launched)
			}
			rec, err := lease.Discover(dir, true)
			if err != nil || rec.PID != holder.Process.Pid || !lease.Busy(dir) {
				t.Fatalf("the previous engine was lost: record=%+v err=%v", rec, err)
			}
		})
	}
}

func TestFrontendPreparationFinishesBeforeHandoffAndReplacementOwnsItsWait(t *testing.T) {
	dir := t.TempDir()
	holder := startLeaseHolder(t, dir)
	var prepared atomic.Int64
	var handoffAfterPreparation atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/engine/restart" {
			handoffAfterPreparation.Store(prepared.Load() != 0)
			_ = json.NewEncoder(w).Encode(map[string]any{"ready": true, "stage": "ready"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": dir, "mock": true, "version": "previous build"})
	}))
	defer srv.Close()
	if err := lease.Publish(dir, lease.Record{PID: holder.Process.Pid, URL: srv.URL, Version: "previous build"}); err != nil {
		t.Fatal(err)
	}
	oldLoader, oldLauncher := loadEngineFrontend, launchEngine
	t.Cleanup(func() { loadEngineFrontend, launchEngine = oldLoader, oldLauncher })
	loadEngineFrontend = func(context.Context) (fs.FS, error) {
		time.Sleep(200 * time.Millisecond)
		prepared.Store(time.Now().UnixNano())
		return nil, nil
	}
	want := errors.New("fixture stops after observing the replacement budget")
	launchEngine = func(ctx context.Context, _ string, _ string, _ bool) (string, error) {
		policy, _ := ctx.Value(engineStartupPolicyKey{}).(engineStartupPolicy)
		if _, bounded := ctx.Deadline(); bounded || prepared.Load() == 0 || !policy.forwardOnly {
			t.Errorf("acknowledged replacement did not receive its own wait: prepared=%d bounded=%v forwardOnly=%v", prepared.Load(), bounded, policy.forwardOnly)
		}
		return "", want
	}
	_, err := ensureEngine(dir, "127.0.0.1:0", true)
	if !errors.Is(err, want) || !handoffAfterPreparation.Load() {
		t.Fatalf("restart did not follow successful preparation: err=%v prepared=%v", err, prepared.Load())
	}
}
