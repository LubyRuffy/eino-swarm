package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
)

// The first shell can own the lock while an older build is still preparing
// its listener. Waiting for that listener must not skip replacement identity.
func TestEnsureEngineReplacesADifferentBuildThatWasStillStarting(t *testing.T) {
	self := lease.ThisBuild(version)
	for _, tc := range []struct {
		name  string
		build lease.Build
	}{
		{"different version", lease.Build{Version: version + "-older", Dev: self.Dev, Ino: self.Ino}},
		{"same version different file", lease.Build{Version: version, Dev: self.Dev, Ino: self.Ino + 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(stopTestEngine)
			dir := t.TempDir()
			holder := startLeaseHolder(t, dir)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/engine/restart" {
					http.NotFound(w, r)
					return
				}
				if r.URL.Path == "/api/threads" {
					_ = json.NewEncoder(w).Encode(map[string]any{"threads": []any{}})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data_dir": dir, "mock": true, "version": tc.build.Version,
				})
			}))
			defer srv.Close()
			published := make(chan error, 1)
			go func() {
				time.Sleep(150 * time.Millisecond)
				published <- lease.Publish(dir, lease.Record{
					PID: holder.Process.Pid, URL: srv.URL, Mock: true,
					Version: tc.build.Version, Dev: tc.build.Dev, Ino: tc.build.Ino,
				})
			}()
			url, err := ensureEngine(dir, "127.0.0.1:0", true)
			if pubErr := <-published; pubErr != nil {
				t.Fatal(pubErr)
			}
			if err != nil {
				t.Fatal(err)
			}
			if url == srv.URL {
				t.Fatal("startup wait attached to the build the new shell must replace")
			}
			rec, err := lease.Discover(dir, true)
			if err != nil || !lease.SameEngine(rec, self) {
				t.Fatalf("replacement is not the current binary: record=%+v err=%v", rec, err)
			}
		})
	}
}

func TestEnsureEnginePreservesAStartingHolderWithADiscoveryFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mock   bool
		status int
		want   error
	}{
		{"mock mismatch", false, http.StatusOK, lease.ErrMockMismatch},
		{"unreachable", true, http.StatusServiceUnavailable, lease.ErrUnreachable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			holder := startLeaseHolder(t, dir)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/engine/restart" {
					http.NotFound(w, r)
					return
				}
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": dir, "mock": tc.mock})
			}))
			defer srv.Close()
			published := make(chan error, 1)
			go func() {
				time.Sleep(150 * time.Millisecond)
				published <- lease.Publish(dir, lease.Record{PID: holder.Process.Pid, URL: srv.URL, Mock: tc.mock})
			}()
			_, err := ensureEngine(dir, "127.0.0.1:0", true)
			if pubErr := <-published; pubErr != nil {
				t.Fatal(pubErr)
			}
			if !errors.Is(err, tc.want) || !lease.Busy(dir) {
				t.Fatalf("discovery failure must leave the holder alone: err=%v held=%v", err, lease.Busy(dir))
			}
		})
	}
}

func TestEnsureEngineWaitsForAStartingBuildsActiveTurn(t *testing.T) {
	t.Cleanup(stopTestEngine)
	dir := t.TempDir()
	holder := startLeaseHolder(t, dir)
	var quiet atomic.Bool
	checked := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/engine/restart" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/api/threads" {
			running := !quiet.Load()
			_ = json.NewEncoder(w).Encode(map[string]any{"threads": []any{map[string]any{"running": running}}})
			select {
			case checked <- struct{}{}:
			default:
			}
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": dir, "mock": true, "version": version + "-older"})
	}))
	defer srv.Close()
	published := make(chan error, 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		published <- lease.Publish(dir, lease.Record{
			PID: holder.Process.Pid, URL: srv.URL, Mock: true, Version: version + "-older",
		})
	}()
	type result struct {
		url string
		err error
	}
	finished := make(chan result, 1)
	go func() {
		url, err := ensureEngine(dir, "127.0.0.1:0", true)
		finished <- result{url, err}
	}()
	select {
	case <-checked:
	case got := <-finished:
		t.Fatalf("startup identity must check the old turn before attaching: %+v", got)
	case <-time.After(5 * time.Second):
		t.Fatal("startup never reached the active-turn check")
	}
	if !lease.Busy(dir) {
		t.Fatal("the active turn lost its engine lock")
	}
	select {
	case got := <-finished:
		t.Fatalf("the active turn must finish before replacement: %+v", got)
	default:
	}
	quiet.Store(true)
	if err := <-published; err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-finished:
		if got.err != nil || got.url == srv.URL {
			t.Fatalf("quiet old build was not replaced: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("quiet old build was never replaced")
	}
}

func TestEnsureEngineReportsInvalidConfigBeforeStarting(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte("models: ["), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureEngine(dir, "127.0.0.1:0", true); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("invalid config must prevent startup: %v", err)
	}
	if lease.Busy(dir) {
		t.Fatal("invalid config still started an engine")
	}
}

func TestEnsureEnginePreservesAHolderWhenItsTurnStateCannotBeRead(t *testing.T) {
	dir := t.TempDir()
	holder := startLeaseHolder(t, dir)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/engine/restart" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/api/threads" {
			_, _ = w.Write([]byte("{"))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": dir, "mock": true, "version": version + "-older"})
	}))
	defer srv.Close()
	if err := lease.Publish(dir, lease.Record{PID: holder.Process.Pid, URL: srv.URL, Mock: true, Version: version + "-older"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureEngine(dir, "127.0.0.1:0", true); err == nil || !strings.Contains(err.Error(), "list conversations") {
		t.Fatalf("failed turn-state check must prevent replacement: %v", err)
	}
	if !lease.Busy(dir) {
		t.Fatal("failed turn-state check stopped the holder")
	}
}

// The restart budget also bounds an older engine that ignores SIGTERM.
func TestEnsureEngineAutomaticallyStopsAHolderThatIgnoresSIGTERM(t *testing.T) {
	t.Setenv("ZWAI_LEASE_IGNORE_STOP", "1")
	dir := t.TempDir()
	t.Cleanup(stopTestEngine)
	holder := startLeaseHolder(t, dir)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/engine/restart" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/api/threads" {
			_ = json.NewEncoder(w).Encode(map[string]any{"threads": []any{}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": dir, "mock": true, "version": version + "-older"})
	}))
	defer srv.Close()
	if err := lease.Publish(dir, lease.Record{PID: holder.Process.Pid, URL: srv.URL, Mock: true, Version: version + "-older"}); err != nil {
		t.Fatal(err)
	}
	url, err := ensureEngine(dir, "127.0.0.1:0", true)
	if err != nil || url == srv.URL {
		t.Fatalf("automatic restart failed: url=%s err=%v", url, err)
	}
}

func TestEnsureEngineStartsWhenTheUnpublishedHolderExits(t *testing.T) {
	t.Cleanup(stopTestEngine)
	dir := t.TempDir()
	holder := startLeaseHolder(t, dir)
	stopped := make(chan error, 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		stopped <- lease.Stop(holder.Process.Pid)
	}()
	url, err := ensureEngine(dir, "127.0.0.1:0", true)
	if stopErr := <-stopped; stopErr != nil {
		t.Fatal(stopErr)
	}
	if err != nil || url == "" {
		t.Fatalf("dead startup holder prevented replacement: url=%q err=%v", url, err)
	}
	rec, err := lease.Discover(dir, true)
	if err != nil || !lease.SameEngine(rec, lease.ThisBuild(version)) {
		t.Fatalf("replacement identity=%+v err=%v", rec, err)
	}
}
