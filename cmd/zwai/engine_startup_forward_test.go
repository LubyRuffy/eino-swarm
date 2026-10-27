package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/lease"
)

// Checkpoint acknowledgement has already frozen the old engine. Its launch
// deadline must not remain an owner that can kill the only replacement.
func TestAcknowledgedReplacementDoesNotGiveTheNewEngineAHandoffDeadline(t *testing.T) {
	dir := acknowledgedStartingEngine(t)
	before := launchEngine
	t.Cleanup(func() { launchEngine = before })
	want := errors.New("fixture stopped after inspecting replacement ownership")
	launchEngine = func(ctx context.Context, _, _ string, _ bool) (string, error) {
		if deadline, ok := ctx.Deadline(); ok {
			t.Errorf("the old handoff deadline still owns the replacement: %s", deadline)
		}
		return "", want
	}
	if _, err := ensureEngine(dir, "127.0.0.1:0", true); !errors.Is(err, want) {
		t.Fatalf("replacement startup = %v", err)
	}
}

// An unready but live replacement remains the only holder after the old
// process exited. A bounded launcher wait may report it, but cannot kill it.
func TestAcknowledgedReplacementKeepsItsStartingChildAtColdWaitLimit(t *testing.T) {
	t.Setenv("ZWAI_ENGINE_STARTUP_HELPER", "1")
	t.Setenv("ZWAI_ENGINE_STARTUP_DELAY", "5s")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	dir := acknowledgedStartingEngine(t)
	childPID := 0
	t.Cleanup(func() { stopStartupHelper(t, dir, childPID) })
	beforeLauncher, beforeWait := launchEngine, engineBootWait
	launchEngine, engineBootWait = spawnEngineContext, 300*time.Millisecond
	t.Cleanup(func() { launchEngine, engineBootWait = beforeLauncher, beforeWait })
	url, err := ensureEngine(dir, "127.0.0.1:0", true)
	var pending *engineStartupPendingError
	if errors.As(err, &pending) {
		childPID = pending.PID
	}
	if url != "" || !errors.Is(err, context.DeadlineExceeded) || pending == nil || pending.PID <= 0 || pending.LogPath != filepath.Join(dir, "engine.log") {
		t.Fatalf("startup did not identify the surviving replacement: %v", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprint(childPID)) || !strings.Contains(err.Error(), filepath.Join(dir, "engine.log")) {
		t.Fatalf("pending startup omitted the process or its log: %v", err)
	}
	// The cold wait includes scheduling the child runtime. Its expiry need not
	// imply that Acquire has run yet; observe that separate boundary explicitly.
	waitRestartCondition(t, 5*time.Second, "the identified replacement acquiring its own lease after the cold wait", func() bool {
		body, err := os.ReadFile(filepath.Join(dir, "startup-helper.pid"))
		return err == nil && string(body) == strconv.Itoa(childPID) && lease.Busy(dir)
	})
}

// The test helper owns a real lock in another process; only its HTTP restart
// acknowledgement is scripted. No model or shared application data is used.
func acknowledgedStartingEngine(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	holder := startLeaseHolder(t, dir)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/engine/restart" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ready": true, "stage": "ready"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": dir, "mock": true, "version": "previous build"})
	}))
	t.Cleanup(srv.Close)
	if err := lease.Publish(dir, lease.Record{PID: holder.Process.Pid, URL: srv.URL, Version: "previous build"}); err != nil {
		t.Fatal(err)
	}
	return dir
}

func stopUnconfirmedStartupAttempt(t *testing.T, dir string, ownedPID ...int) {
	t.Helper()
	if len(ownedPID) > 0 && ownedPID[0] > 0 {
		stopStartupHelperPID(t, ownedPID[0])
		return
	}
	deadline := time.Now().Add(5 * time.Second)
	for startupBudgetGoroutines("spawnEngineContext.func1(") != 0 && time.Now().Before(deadline) {
		body, err := os.ReadFile(filepath.Join(dir, "startup-helper-attempt.pid"))
		if err == nil {
			pid, err := strconv.Atoi(string(body))
			if err != nil || pid <= 0 || pid == os.Getpid() {
				t.Errorf("invalid startup attempt identity %q: %v", body, err)
				return
			}
			stopStartupHelperPID(t, pid)
			break
		}
		time.Sleep(time.Millisecond)
	}
	waitForStartupBudgetGoroutines(t, "spawnEngineContext.func1(", 0)
}

// A cold-start wait may finish before the child has acquired its lease.
// Cleanup must still reap this launch's child and its original Wait goroutine.
func TestStartupCleanupReapsHelperBeforeLeasePublication(t *testing.T) {
	t.Setenv("ZWAI_ENGINE_STARTUP_HELPER", "1")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	dir := t.TempDir()
	t.Setenv("ZWAI_ENGINE_BEFORE_ACQUIRE_RELEASE_FILE", filepath.Join(dir, "acquire-release"))
	t.Cleanup(func() { stopUnconfirmedStartupAttempt(t, dir) })
	before := engineBootWait
	engineBootWait = 200 * time.Millisecond
	t.Cleanup(func() { engineBootWait = before })
	ctx := context.WithValue(t.Context(), engineStartupPolicyKey{}, engineStartupPolicy{owner: t.Context(), forwardOnly: true})
	_, err := spawnEngineContext(ctx, dir, "127.0.0.1:0", true)
	var pending *engineStartupPendingError
	if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &pending) {
		t.Fatalf("gated child did not outlive the bounded launcher wait: %v", err)
	}
	waitRestartCondition(t, 5*time.Second, "owned helper reaching its pre-acquire gate", func() bool {
		body, err := os.ReadFile(filepath.Join(dir, "startup-helper-attempt.pid"))
		return err == nil && string(body) == strconv.Itoa(pending.PID)
	})
	if _, err := os.Stat(filepath.Join(dir, "startup-helper.pid")); !os.IsNotExist(err) || lease.Busy(dir) {
		t.Fatalf("fixture passed the lease boundary before release: pidErr=%v held=%v", err, lease.Busy(dir))
	}
	stopStartupHelper(t, dir)
	if remaining := startupBudgetGoroutines("spawnEngineContext.func1("); remaining != 0 {
		t.Fatalf("cleanup without a lease pidfile left %d original child waiters", remaining)
	}
}

// A real detached child publishes after the original 30s handoff deadline.
// The launch must still attach to that same process instead of leaving no engine.
func TestAcknowledgedReplacementAttachesAfterTheHandoffDeadline(t *testing.T) {
	t.Setenv("ZWAI_ENGINE_STARTUP_HELPER", "1")
	t.Setenv("ZWAI_ENGINE_STARTUP_DELAY", "31s")
	dir := acknowledgedStartingEngine(t)
	t.Cleanup(func() { stopStartupHelper(t, dir) })
	before := launchEngine
	launchEngine = spawnEngineContext
	t.Cleanup(func() { launchEngine = before })
	began := time.Now()
	url, err := ensureEngine(dir, "127.0.0.1:0", true)
	if err != nil || url == "" {
		t.Fatalf("delayed publication killed or lost the replacement: url=%q err=%v elapsed=%s", url, err, time.Since(began))
	}
	if time.Since(began) <= restartBudget {
		t.Fatal("replacement fixture did not cross the handoff deadline")
	}
	rec, err := lease.Discover(dir, true)
	if err != nil || rec.URL != url || !lease.SameEngine(rec, lease.ThisBuild(version)) {
		t.Fatalf("launcher did not attach to the surviving replacement: record=%+v err=%v", rec, err)
	}
}

func TestAcknowledgedReplacementInterruptStopsItsStartingChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not support sending os.Interrupt")
	}
	t.Setenv("ZWAI_ENGINE_STARTUP_HELPER", "1")
	t.Setenv("ZWAI_ENGINE_STARTUP_DELAY", "5s")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	dir := acknowledgedStartingEngine(t)
	t.Cleanup(func() { stopStartupHelper(t, dir) })
	before := launchEngine
	launchEngine = spawnEngineContext
	t.Cleanup(func() { launchEngine = before })
	done := make(chan error, 1)
	go func() { _, err := ensureEngine(dir, "127.0.0.1:0", true); done <- err }()
	waitRestartCondition(t, 5*time.Second, "replacement acquiring its own lease", func() bool {
		_, err := os.Stat(filepath.Join(dir, "startup-helper.pid"))
		return err == nil && lease.Busy(dir)
	})
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || lease.Busy(dir) {
			t.Fatalf("explicit interruption kept a replacement child: err=%v held=%v", err, lease.Busy(dir))
		}
	case <-time.After(time.Second):
		t.Fatal("replacement ignored launcher interruption")
	}
}

func TestForwardReplacementPreservesExplicitCancellationCause(t *testing.T) {
	t.Setenv("ZWAI_ENGINE_STARTUP_HELPER", "1")
	t.Setenv("ZWAI_ENGINE_STARTUP_DELAY", "5s")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	dir := t.TempDir()
	t.Cleanup(func() { stopStartupHelper(t, dir) })
	owner, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	ctx := context.WithValue(owner, engineStartupPolicyKey{}, engineStartupPolicy{owner: owner, forwardOnly: true})
	done := make(chan error, 1)
	go func() { _, err := spawnEngineContext(ctx, dir, "127.0.0.1:0", true); done <- err }()
	waitRestartCondition(t, 5*time.Second, "replacement startup", func() bool { return lease.Busy(dir) })
	want := errors.New("launcher stopped by its owner")
	cancel(want)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, want) || lease.Busy(dir) {
			t.Fatalf("explicit cancellation changed category/cause or kept the child: err=%v held=%v", err, lease.Busy(dir))
		}
	case <-time.After(time.Second):
		t.Fatal("replacement ignored cancellation")
	}
}

func TestForwardReplacementExpiryDoesNotClaimOrStopACompetingHolder(t *testing.T) {
	t.Setenv("ZWAI_ENGINE_STARTUP_HELPER", "1")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	dir := t.TempDir()
	release := filepath.Join(dir, "acquire-release")
	t.Setenv("ZWAI_ENGINE_BEFORE_ACQUIRE_RELEASE_FILE", release)
	holder := startLeaseHolder(t, dir)
	// Register after the holder so a failing assertion stops our delayed child
	// before releasing the competing lock. Otherwise that child could acquire
	// it during cleanup and recreate the directory after TempDir removes it.
	childPID := 0
	t.Cleanup(func() { stopUnconfirmedStartupAttempt(t, dir, childPID) })
	before := engineBootWait
	engineBootWait = 200 * time.Millisecond
	t.Cleanup(func() { engineBootWait = before })
	ctx := context.WithValue(t.Context(), engineStartupPolicyKey{}, engineStartupPolicy{owner: t.Context(), forwardOnly: true})
	_, err := spawnEngineContext(ctx, dir, "127.0.0.1:0", true)
	var pending *engineStartupPendingError
	if errors.As(err, &pending) {
		childPID = pending.PID
	}
	if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &pending) || pending.PID <= 0 || pending.PID == holder.Process.Pid || !strings.Contains(err.Error(), "ownership unconfirmed") || !lease.Busy(dir) {
		t.Fatalf("a failed child claimed or stopped the competing lease: err=%v held=%v", err, lease.Busy(dir))
	}
	waitRestartCondition(t, 5*time.Second, "the identified child waiting before its competing lease attempt", func() bool {
		body, err := os.ReadFile(filepath.Join(dir, "startup-helper-attempt.pid"))
		return err == nil && string(body) == strconv.Itoa(childPID)
	})
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitForStartupBudgetGoroutines(t, "spawnEngineContext.func1(", 0)
	if !lease.Busy(dir) || !strings.Contains(engineLogTail(filepath.Join(dir, "engine.log"), 0), lease.ErrHeld.Error()) {
		t.Fatalf("the losing child did not refuse the competing holder: held=%v log=%q", lease.Busy(dir), engineLogTail(filepath.Join(dir, "engine.log"), 0))
	}
}

// A child can be descheduled before taking the lock, or while publishing its
// owner receipt. A forward-only wait cannot turn missing evidence into a kill.
func TestForwardReplacementExpiryPreservesChildWhoseOwnershipIsNotYetKnown(t *testing.T) {
	t.Setenv("ZWAI_ENGINE_STARTUP_HELPER", "1")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
	dir := t.TempDir()
	release := filepath.Join(dir, "acquire-release")
	t.Setenv("ZWAI_ENGINE_BEFORE_ACQUIRE_RELEASE_FILE", release)
	childPID := 0
	t.Cleanup(func() { stopStartupHelper(t, dir, childPID) })
	t.Cleanup(func() { stopUnconfirmedStartupAttempt(t, dir, childPID) })
	before := engineBootWait
	engineBootWait = 200 * time.Millisecond
	t.Cleanup(func() { engineBootWait = before })
	ctx := context.WithValue(t.Context(), engineStartupPolicyKey{}, engineStartupPolicy{owner: t.Context(), forwardOnly: true})
	_, err := spawnEngineContext(ctx, dir, "127.0.0.1:0", true)
	var pending *engineStartupPendingError
	if errors.As(err, &pending) {
		childPID = pending.PID
	}
	if !errors.Is(err, context.DeadlineExceeded) || pending == nil || childPID <= 0 {
		t.Fatalf("unconfirmed child was not preserved: %v", err)
	}
	waitRestartCondition(t, 5*time.Second, "the identified child waiting before lease acquisition", func() bool {
		body, err := os.ReadFile(filepath.Join(dir, "startup-helper-attempt.pid"))
		return err == nil && string(body) == strconv.Itoa(childPID)
	})
	if lease.Busy(dir) {
		t.Fatal("the gated unconfirmed child already acquired the lease")
	}
	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var rec lease.Record
	waitRestartCondition(t, 5*time.Second, "unconfirmed child publishing its real lease after the cold wait", func() bool {
		var discoverErr error
		rec, discoverErr = lease.Discover(dir, true)
		return discoverErr == nil
	})
	if !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &pending) || pending.PID != rec.PID || !strings.Contains(err.Error(), "ownership unconfirmed") || !strings.Contains(err.Error(), filepath.Join(dir, "engine.log")) {
		t.Fatalf("missing ownership evidence became a claimed lease or omitted its diagnostics: %v", err)
	}
}

// A successful response can finish decoding at the deadline. At that point
// the acknowledged old process is frozen; only its signal owner can cancel.
func TestReplacementAcknowledgementCrossingTheDeadlineStillStopsTheOldProcess(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		t.Run(fmt.Sprint(interrupted), func(t *testing.T) {
			dir := t.TempDir()
			holder := startLeaseHolder(t, dir)
			owner, cancelOwner := context.WithCancelCause(t.Context())
			defer cancelOwner(nil)
			budget, cancelBudget := context.WithTimeout(owner, 30*time.Millisecond)
			defer cancelBudget()
			ctx := context.WithValue(budget, engineStartupPolicyKey{}, engineStartupPolicy{owner: owner})
			want := errors.New("owner explicitly interrupted the handoff")
			before := http.DefaultClient
			http.DefaultClient = &http.Client{Transport: restartResponseTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Body: restartResponseBody{read: func(b []byte) (int, error) {
					<-budget.Done()
					if interrupted {
						cancelOwner(want)
					}
					return copy(b, `{"ready":true,"stage":"ready"}`), nil
				}}}, nil
			})}
			t.Cleanup(func() { http.DefaultClient = before })
			err := replaceEngine(ctx, lease.Record{PID: holder.Process.Pid, URL: "http://restart-fixture"})
			if !interrupted && (err != nil || lease.Busy(dir)) {
				t.Fatalf("acknowledged deadline stranded the frozen old engine: err=%v held=%v", err, lease.Busy(dir))
			}
			if interrupted && (!errors.Is(err, context.Canceled) || !errors.Is(err, want) || !lease.Busy(dir)) {
				t.Fatalf("explicit interruption lost its cause or stopped the old holder: err=%v held=%v", err, lease.Busy(dir))
			}
		})
	}
}

type restartResponseTransport func(*http.Request) (*http.Response, error)

func (f restartResponseTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type restartResponseBody struct{ read func([]byte) (int, error) }

func (b restartResponseBody) Read(p []byte) (int, error) { return b.read(p) }
func (restartResponseBody) Close() error                 { return nil }
