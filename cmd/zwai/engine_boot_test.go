package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/lease"
)

// This offline subprocess publishes the real lease/meta handshake after a
// controlled initialization delay. It never opens a model or shared database.
func runStartupHelper() int {
	fs := flag.NewFlagSet("engine", flag.ContinueOnError)
	dir := fs.String("data-dir", "", "")
	addr := fs.String("addr", "127.0.0.1:0", "")
	mock := fs.Bool("mock", false, "")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return 3
	}
	hold, err := lease.Acquire(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 4
	}
	defer hold.Release()
	_ = os.WriteFile(filepath.Join(*dir, "startup-helper.pid"), []byte(strconv.Itoa(os.Getpid())), 0o600)
	fmt.Fprintln(os.Stderr, "startup fixture acquired lease")
	delay, _ := time.ParseDuration(os.Getenv("ZWAI_ENGINE_STARTUP_DELAY"))
	time.Sleep(delay)
	if code := os.Getenv("ZWAI_ENGINE_STARTUP_EXIT"); code != "" {
		value, _ := strconv.Atoi(code)
		return value
	}
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 5
	}
	defer listener.Close()
	build := lease.ThisBuild(version)
	if err := lease.Publish(*dir, lease.Record{
		PID: os.Getpid(), URL: "http://" + listener.Addr().String(), Mock: *mock,
		Version: build.Version, Dev: build.Dev, Ino: build.Ino,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 6
	}
	_ = http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay, _ := time.ParseDuration(os.Getenv("ZWAI_ENGINE_META_DELAY")); delay > 0 {
			_ = os.WriteFile(filepath.Join(*dir, "startup-helper-meta"), []byte("requested"), 0o600)
			time.Sleep(delay)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": *dir, "mock": *mock, "version": version})
	}))
	return 0
}

// A desktop must open even when legitimate local startup takes longer than
// the old 15-second limit. This exercises an actual child, lock and HTTP probe.
func TestSpawnEngineAcceptsSlowReadinessBeyondFifteenSeconds(t *testing.T) {
	t.Setenv("ZWAI_ENGINE_STARTUP_HELPER", "1")
	t.Setenv("ZWAI_ENGINE_STARTUP_DELAY", "16s")
	dir := t.TempDir()
	t.Cleanup(func() { stopStartupHelper(t, dir) })
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	started := time.Now()
	url, err := spawnEngineContext(ctx, dir, "127.0.0.1:0", true)
	if err != nil || url == "" {
		t.Fatalf("slow local initialization prevented opening the desktop: url=%q err=%v elapsed=%s", url, err, time.Since(started))
	}
	if time.Since(started) < 15*time.Second {
		t.Fatal("fixture never crossed the former startup deadline")
	}
	rec, err := lease.Discover(dir, true)
	if err != nil || rec.PID == os.Getpid() || !lease.SameEngine(rec, lease.ThisBuild(version)) {
		t.Fatalf("readiness did not belong to the launched child: record=%+v err=%v", rec, err)
	}
}

func stopStartupHelper(t *testing.T, dir string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, "startup-helper.pid"))
	if err != nil {
		return
	}
	pid, _ := strconv.Atoi(string(body))
	if pid > 0 && pid != os.Getpid() {
		process, _ := os.FindProcess(pid)
		_ = process.Kill()
	}
	deadline := time.Now().Add(time.Second)
	for lease.Busy(dir) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if lease.Busy(dir) {
		t.Error("startup helper retained its lease after cleanup")
	}
}

func TestSpawnEngineFailureUsesOnlyItsOwnLogAndExitStatus(t *testing.T) {
	dir := t.TempDir()
	const stale = "obsolete provider failure from an earlier engine"
	if err := os.WriteFile(filepath.Join(dir, "engine.log"), []byte(stale+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := spawnEngine(dir, "127.0.0.1:0", true)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(err.Error(), "not an engine") || strings.Contains(err.Error(), stale) {
		t.Fatalf("startup must retain this child's cause without historical failures: %v", err)
	}
}

func TestSpawnEngineCancellationStopsAndReapsItsOwnStartingChild(t *testing.T) {
	t.Setenv("ZWAI_ENGINE_STARTUP_HELPER", "1")
	t.Setenv("ZWAI_ENGINE_STARTUP_DELAY", "5s")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	dir := t.TempDir()
	t.Cleanup(func() { stopStartupHelper(t, dir) })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := spawnEngineContext(ctx, dir, "127.0.0.1:0", true); done <- err }()
	deadline := time.Now().Add(5 * time.Second)
	for !lease.Busy(dir) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !lease.Busy(dir) {
		t.Fatal("child did not enter its delayed startup")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost its cause: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("startup did not respect the parent cancellation")
	}
	if lease.Busy(dir) {
		t.Fatal("failed startup left its own child running")
	}
}

// The cold-start allowance has its own deadline and progress feedback, while
// a shorter caller deadline remains authoritative (covered by the budget tests).
func TestSpawnEngineStartupDeadlineStopsItsChildAndReportsProgress(t *testing.T) {
	t.Setenv("ZWAI_ENGINE_STARTUP_HELPER", "1")
	t.Setenv("ZWAI_ENGINE_STARTUP_DELAY", "5s")
	dir := t.TempDir()
	t.Cleanup(func() { stopStartupHelper(t, dir) })
	oldWait, oldHeartbeat := engineBootWait, engineBootHeartbeat
	engineBootWait, engineBootHeartbeat = 200*time.Millisecond, 20*time.Millisecond
	oldOutput := log.Writer()
	var output bytes.Buffer
	log.SetOutput(&output)
	t.Cleanup(func() {
		engineBootWait, engineBootHeartbeat = oldWait, oldHeartbeat
		log.SetOutput(oldOutput)
	})
	started := time.Now()
	_, err := spawnEngine(dir, "127.0.0.1:0", true)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second || lease.Busy(dir) {
		t.Fatalf("cold startup deadline did not stop its own child: err=%v elapsed=%s held=%v", err, time.Since(started), lease.Busy(dir))
	}
	if !strings.Contains(output.String(), "engine startup: still initializing after") {
		t.Fatalf("slow startup gave no progress feedback: %s", output.String())
	}
}

func TestSpawnEngineRejectsAnExpiredCallerBeforeLaunching(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := spawnEngineContext(ctx, dir, "", false); !errors.Is(err, context.Canceled) {
		t.Fatalf("expired startup returned %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "engine.log")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired caller launched an engine: %v", err)
	}
}

func TestSpawnEngineReportsAnUnwritableLog(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "engine.log"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := spawnEngine(dir, "", false); err == nil {
		t.Fatal("startup proceeded without opening its diagnostic output")
	}
}

func TestEngineLogTailOnlyReadsThisLaunchWithinItsBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine.log")
	if got := engineLogTail(path, 0); got != "" {
		t.Fatalf("missing log = %q", got)
	}
	const old = "previous engine output\n"
	const current = "current child failure\n"
	if err := os.WriteFile(path, []byte(old+current), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := engineLogTail(path, int64(len(old))); got != current {
		t.Fatalf("old output leaked into new launch: %q", got)
	}
	if err := os.WriteFile(path, []byte(old+strings.Repeat("z", 3000)), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := engineLogTail(path, int64(len(old))); got != strings.Repeat("z", 2000) {
		t.Fatalf("log tail was not bounded: %d bytes", len(got))
	}
	if got := engineLogTail(path, 1<<20); got != "" {
		t.Fatalf("a truncated log exposed output from before this launch: %q", got)
	}
}

func TestStartingHolderReportsProgressWithoutExtendingItsCallerDeadline(t *testing.T) {
	dir := t.TempDir()
	startLeaseHolder(t, dir)
	oldHeartbeat := engineBootHeartbeat
	engineBootHeartbeat = 20 * time.Millisecond
	oldOutput := log.Writer()
	var output bytes.Buffer
	log.SetOutput(&output)
	t.Cleanup(func() { engineBootHeartbeat = oldHeartbeat; log.SetOutput(oldOutput) })
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	_, held, err := discoverIfStartingContext(ctx, dir, true)
	if !held || !errors.Is(err, context.DeadlineExceeded) || !lease.Busy(dir) {
		t.Fatalf("holder wait ignored the caller or stopped another process: held=%v err=%v", held, err)
	}
	if !strings.Contains(output.String(), "engine startup: still initializing after") {
		t.Fatalf("waiting on a holder gave no progress feedback: %s", output.String())
	}
}

func TestSpawnEngineReportsAChildThatExitsSuccessfullyWithoutReadiness(t *testing.T) {
	t.Setenv("ZWAI_ENGINE_STARTUP_HELPER", "1")
	t.Setenv("ZWAI_ENGINE_STARTUP_EXIT", "0")
	_, err := spawnEngine(t.TempDir(), "127.0.0.1:0", true)
	if err == nil || !strings.Contains(err.Error(), "child exited successfully before readiness") {
		t.Fatalf("a successful process exit was mistaken for readiness: %v", err)
	}
}

func TestEnsureEngineValidatesItsDetachedChildBeforeAttaching(t *testing.T) {
	t.Setenv("ZWAI_ENGINE_STARTUP_HELPER", "1")
	t.Setenv("ZWAI_ENGINE_STARTUP_DELAY", "50ms")
	dir := t.TempDir()
	t.Cleanup(func() { stopStartupHelper(t, dir) })
	oldLauncher := launchEngine
	launchEngine = spawnEngineContext
	t.Cleanup(func() { launchEngine = oldLauncher })
	url, err := ensureEngine(dir, "127.0.0.1:0", true)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := lease.Discover(dir, true)
	if err != nil || rec.URL != url || rec.PID == os.Getpid() || !lease.SameEngine(rec, lease.ThisBuild(version)) {
		t.Fatalf("shell attached without validating its child's identity: url=%q record=%+v err=%v", url, rec, err)
	}
}

// Cancellation can happen inside the synchronous meta probe. A later positive
// response must not turn that cancelled launch into a surviving detached child.
func TestSpawnEngineCancellationDuringReadinessProbeStopsItsChild(t *testing.T) {
	t.Setenv("ZWAI_ENGINE_STARTUP_HELPER", "1")
	t.Setenv("ZWAI_ENGINE_META_DELAY", "250ms")
	dir := t.TempDir()
	t.Cleanup(func() { stopStartupHelper(t, dir) })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := spawnEngineContext(ctx, dir, "127.0.0.1:0", true); done <- err }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "startup-helper-meta")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("startup did not reach its readiness HTTP probe")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || lease.Busy(dir) {
			t.Fatalf("late readiness overrode cancellation: err=%v held=%v", err, lease.Busy(dir))
		}
	case <-time.After(time.Second):
		t.Fatal("readiness cancellation did not return")
	}
}

// A competing process can publish after discovery but before our child starts.
// Its known mode/health failure must be surfaced immediately, without waiting
// the full cold-start allowance or stopping the process that owns the lease.
func TestSpawnEnginePreservesACompetingHoldersDiscoveryFailure(t *testing.T) {
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
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": dir, "mock": tc.mock})
			}))
			defer srv.Close()
			if err := lease.Publish(dir, lease.Record{PID: holder.Process.Pid, URL: srv.URL, Mock: tc.mock}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
			defer cancel()
			started := time.Now()
			url, err := spawnEngineContext(ctx, dir, "127.0.0.1:0", true)
			if url != "" || !errors.Is(err, tc.want) || time.Since(started) > time.Second {
				t.Fatalf("known holder failure became a startup timeout: url=%q err=%v elapsed=%s", url, err, time.Since(started))
			}
			if !lease.Busy(dir) {
				t.Fatal("discovery failure stopped the independent competing holder")
			}
			waitForStartupBudgetGoroutines(t, "spawnEngineContext.func1(", 0)
		})
	}
}

func startupBudgetGoroutines(function string) int {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return strings.Count(string(buf[:n]), function)
}

func waitForStartupBudgetGoroutines(t *testing.T, function string, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if startupBudgetGoroutines(function) == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("startup goroutines for %s = %d, want %d", function, startupBudgetGoroutines(function), want)
}
