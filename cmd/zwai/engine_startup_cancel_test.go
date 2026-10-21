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
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/lease"
)

// A synchronous probe can finish after cancellation. Its metadata or error
// cannot override the launcher's cancellation or stop another lease holder.
func TestStartupCancellationSurvivesAFailedCompetingProbe(t *testing.T) {
	for _, entry := range []string{"spawn", "discover"} {
		for _, unavailable := range []bool{false, true} {
			name := entry + "/mode mismatch"
			if unavailable {
				name = entry + "/unreachable"
			}
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				holder := startLeaseHolder(t, dir)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					cancel()
					if unavailable {
						w.WriteHeader(http.StatusServiceUnavailable)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": dir, "mock": false})
				}))
				defer srv.Close()
				if err := lease.Publish(dir, lease.Record{PID: holder.Process.Pid, URL: srv.URL}); err != nil {
					t.Fatal(err)
				}
				var err error
				if entry == "spawn" {
					_, err = spawnEngineContext(ctx, dir, "127.0.0.1:0", true)
				} else {
					_, _, err = discoverIfStartingContext(ctx, dir, true)
				}
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("failed readiness lost caller cancellation: %v", err)
				}
				if !lease.Busy(dir) {
					t.Fatal("cancellation stopped the competing holder")
				}
			})
		}
	}
}

func TestLauncherInterruptDuringReadinessCannotAttachOrChangeItsCause(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not support sending os.Interrupt")
	}
	for _, result := range []string{"ready", "unreachable", "mode mismatch"} {
		t.Run(result, func(t *testing.T) {
			dir := t.TempDir()
			holder := startLeaseHolder(t, dir)
			var launchContext context.Context
			before := loadEngineFrontend
			loadEngineFrontend = func(ctx context.Context) (fs.FS, error) {
				launchContext = ctx
				return nil, nil
			}
			t.Cleanup(func() { loadEngineFrontend = before })
			build := lease.ThisBuild(version)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				process, err := os.FindProcess(os.Getpid())
				if err == nil {
					err = process.Signal(os.Interrupt)
				}
				if err != nil {
					t.Error(err)
					return
				}
				<-launchContext.Done()
				if result == "unreachable" {
					w.WriteHeader(http.StatusServiceUnavailable)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": dir, "mock": result != "mode mismatch"})
			}))
			defer srv.Close()
			if err := lease.Publish(dir, lease.Record{PID: holder.Process.Pid, URL: srv.URL, Mock: true,
				Version: build.Version, Dev: build.Dev, Ino: build.Ino}); err != nil {
				t.Fatal(err)
			}
			url, err := ensureEngine(dir, "127.0.0.1:0", true)
			if url != "" || !errors.Is(err, context.Canceled) {
				t.Fatalf("readiness overrode launcher interruption: url=%q err=%v", url, err)
			}
			if !lease.Busy(dir) {
				t.Fatal("launcher interruption stopped the competing holder")
			}
		})
	}
}
