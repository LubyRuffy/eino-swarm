//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
)

// A slow startup must use the caller's remaining budget, then cancel the
// engine it launched even if startup I/O only unblocks after the caller left.
func TestInProcessStartupDeadlineCancelsTheStillStartingEngine(t *testing.T) {
	for _, name := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL", "ZWAI_MODEL_BASE_URL", "ZWAI_MODEL_API_KEY", "ZWAI_MODEL_NAME"} {
		t.Setenv(name, "")
	}
	stopTestEngine()
	waitForStartupBudgetGoroutines(t, "startInProcessEngineContext.func1(", 0)
	dir := t.TempDir()
	path := filepath.Join(dir, config.FileName)
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	const raw = "remote:\n  enabled: false\nmemory:\n  enabled: false\n  auto_review: false\n"
	var writer *os.File
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			if writer == nil {
				var err error
				writer, err = os.OpenFile(path, os.O_RDWR, 0o600)
				if err != nil {
					t.Error(err)
					return
				}
			}
			// The first read owns the FIFO descriptor; later config reads
			// need a regular file so the real engine can finish teardown.
			if err := os.Remove(path); err != nil {
				t.Error(err)
			}
			if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
				t.Error(err)
			}
			if _, err := writer.WriteString(raw); err != nil {
				t.Error(err)
			}
			if err := writer.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	t.Cleanup(func() {
		release()
		waitForStartupBudgetGoroutines(t, "startInProcessEngineContext.func1(", 0)
	})
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	type result struct {
		url string
		err error
	}
	done := make(chan result, 1)
	started := time.Now()
	go func() {
		url, err := startInProcessEngineContext(ctx, dir, "127.0.0.1:0", true)
		done <- result{url, err}
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0o600)
		if err == nil {
			writer = os.NewFile(uintptr(fd), path)
			break
		}
		if !errors.Is(err, syscall.ENXIO) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	if writer == nil {
		t.Fatal("engine never entered its blocked startup read")
	}
	select {
	case got := <-done:
		if got.url != "" || !errors.Is(got.err, context.DeadlineExceeded) || time.Since(started) > time.Second {
			t.Fatalf("startup escaped the caller deadline: url=%q err=%v elapsed=%s", got.url, got.err, time.Since(started))
		}
	case <-time.After(time.Second):
		t.Fatal("blocked startup ignored the caller deadline")
	}
	if startupBudgetGoroutines("startInProcessEngineContext.func1(") != 1 {
		t.Fatal("the deadline was not observed while the real startup was blocked")
	}
	release()
	waitForStartupBudgetGoroutines(t, "startInProcessEngineContext.func1(", 0)
	if _, err := os.Stat(filepath.Join(dir, "engine.lock")); err != nil {
		t.Fatalf("released startup never reached the real engine lifecycle: %v", err)
	}
	if lease.Busy(dir) {
		t.Fatal("cancelled startup retained its engine lease")
	}
	if _, err := os.Stat(filepath.Join(dir, "engine.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled startup retained a published engine: %v", err)
	}
	if activeEngineStop != nil {
		t.Fatal("failed startup became the active in-process engine")
	}
}

// A failed new child must not make a competing startup extend the shared
// deadline or stop the competing lease holder on its way out.
func TestSpawnStartupDeadlinePreservesTheCompetingHolderAndReapsItsChild(t *testing.T) {
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	waitForStartupBudgetGoroutines(t, "spawnEngineContext.func1(", 0)
	dir := t.TempDir()
	holder := startLeaseHolder(t, dir)
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	url, err := spawnEngineContext(ctx, dir, "127.0.0.1:0", true)
	if url != "" || !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatalf("competing startup escaped the caller deadline: url=%q err=%v elapsed=%s", url, err, time.Since(started))
	}
	waitForStartupBudgetGoroutines(t, "spawnEngineContext.func1(", 0)
	log, err := os.ReadFile(filepath.Join(dir, "engine.log"))
	// Under load, the deadline may kill the child before TestMain prints.
	// The wait-goroutine check above proves reaping in either case.
	if err != nil || (len(log) > 0 && !strings.Contains(string(log), "not an engine")) {
		t.Fatalf("startup did not retain its own diagnostic file: %q %v", log, err)
	}
	if !lease.Busy(dir) {
		t.Fatal("caller cancellation stopped the independent competing holder")
	}
	if err := holder.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for lease.Busy(dir) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if lease.Busy(dir) {
		t.Fatal("the owned fixture left a lease behind after cleanup")
	}
}

// The actual shell entry point must turn Ctrl-C into startup cancellation;
// otherwise its detached child can survive a launcher that disappeared.
func TestEnsureEngineInterruptCancelsTheActualStartupWait(t *testing.T) {
	dir := t.TempDir()
	startLeaseHolder(t, dir)
	done := make(chan error, 1)
	go func() { _, err := ensureEngine(dir, "127.0.0.1:0", true); done <- err }()
	// Confirm the real entry point installed its signal context and entered
	// discovery before signaling this process, so the test cannot kill itself.
	waitForStartupBudgetGoroutines(t, "discoverIfStartingContext(", 1)
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("shell interrupt lost its cancellation cause: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shell Ctrl-C did not cancel the pending startup")
	}
	if !lease.Busy(dir) {
		t.Fatal("shell interruption killed a competing lease holder")
	}
}

// Exercise the real shell signal context all the way through its detached
// child, rather than proving cancellation only on a manually supplied context.
func TestEnsureEngineInterruptStopsItsOwnDetachedStartingChild(t *testing.T) {
	t.Setenv("ZWAI_ENGINE_STARTUP_HELPER", "1")
	t.Setenv("ZWAI_ENGINE_STARTUP_DELAY", "5s")
	dir := t.TempDir()
	t.Cleanup(func() { stopStartupHelper(t, dir) })
	oldLauncher := launchEngine
	launchEngine = spawnEngineContext
	t.Cleanup(func() { launchEngine = oldLauncher })
	done := make(chan error, 1)
	go func() { _, err := ensureEngine(dir, "127.0.0.1:0", true); done <- err }()
	deadline := time.Now().Add(5 * time.Second)
	for !lease.Busy(dir) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !lease.Busy(dir) {
		t.Fatal("shell did not launch its detached child")
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("shell interruption lost its cause: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shell interruption did not stop its detached child")
	}
	if lease.Busy(dir) {
		t.Fatal("shell exited but its detached startup child survived")
	}
}
