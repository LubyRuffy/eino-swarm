//go:build darwin

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/desktop"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
)

func TestRecoveryDiagnosticShutdownCannotPanicOrGrowUnbounded(t *testing.T) {
	spec, _ := recoveryEngineFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer server.Close()
	spec.Engine.URL = server.URL
	report, closeReporter := desktopRecoveryReporter(spec)
	report(desktop.RecoveryDiagnostic{SessionID: spec.SessionID, Phase: "replacement_failed", Reason: strings.Repeat("detail", 200)})
	closeReporter()
	closeReporter()
	for i := 0; i < 100; i++ {
		report(desktop.RecoveryDiagnostic{SessionID: spec.SessionID, Phase: "ready"})
	}
	body, err := os.ReadFile(spec.Journal)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > desktopRecoveryJournalLimit {
		t.Fatal("late diagnostics escaped journal bound")
	}
}

func TestRecoveryJournalRejectsOversizedRowsAndBlockedPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recovery.jsonl")
	if err := appendRecoveryDiagnostic(path, desktop.RecoveryDiagnostic{Reason: strings.Repeat("x", desktopRecoveryJournalLimit)}); err == nil {
		t.Fatal("oversized row accepted")
	}
	if err := appendRecoveryDiagnostic(t.TempDir(), desktop.RecoveryDiagnostic{}); err == nil {
		t.Fatal("directory became a journal")
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte{'x'}, desktopRecoveryJournalLimit), 0600); err != nil {
		t.Fatal(err)
	}
	if err := appendRecoveryDiagnostic(path, desktop.RecoveryDiagnostic{SessionID: "bounded"}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var event desktop.RecoveryDiagnostic
	if json.Unmarshal(bytes.TrimSpace(body), &event) != nil || event.SessionID != "bounded" {
		t.Fatal("invalid old tail corrupted a new journal row")
	}
}

func TestPrivateRecoveryCleanupNeverDeletesAnUnownedDirectory(t *testing.T) {
	parent := t.TempDir()
	private := filepath.Join(parent, "desktop-recovery-unowned")
	_ = os.Mkdir(private, 0700)
	marker := filepath.Join(private, "session")
	_ = os.WriteFile(marker, []byte("other-owner"), 0600)
	for _, spec := range []desktopRecoverySpec{{PrivateDir: "relative"}, {PrivateDir: filepath.Join(parent, "missing")}, {PrivateDir: private, SessionID: "current"}} {
		removeDesktopRecoveryCopy(spec)
	}
	if body, err := os.ReadFile(marker); err != nil || string(body) != "other-owner" {
		t.Fatal("cleanup deleted another session")
	}
	link := filepath.Join(parent, "desktop-recovery-symlink")
	if err := os.Symlink(private, link); err != nil {
		t.Fatal(err)
	}
	removeDesktopRecoveryCopy(desktopRecoverySpec{PrivateDir: link, SessionID: "other-owner"})
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("cleanup followed a recovery-directory symlink")
	}
}

func TestRecoveryURLsPreserveProjectScopeAndRejectMalformedOrigins(t *testing.T) {
	if recoveryWindowURL("%", "", "") != "%" {
		t.Fatal("malformed URL changed")
	}
	if !strings.Contains(recoveryWindowURL("http://127.0.0.1:1234", "", "pr_actual"), "recovery_project=pr_actual") {
		t.Fatal("project scope lost")
	}
	for _, pair := range [][2]string{{"%", "http://127.0.0.1"}, {"http://127.0.0.1", "%"}, {"https://127.0.0.1", "https://127.0.0.1"}} {
		if sameRecoveryOrigin(pair[0], pair[1]) {
			t.Fatal("untrusted origin accepted")
		}
	}
}

func TestAttachOnlyRejectsInvalidLineageBeforeOpeningAWindow(t *testing.T) {
	for _, body := range []string{"{", `{}`, `{"session_id":"session","token":"token","generation":1,"attempts":3}`} {
		if err := runDesktopAttachment(strings.NewReader(body), io.Discard); err == nil {
			t.Fatal("invalid handoff opened a window")
		}
	}
	if err := runDesktopAttach([]string{"--data-dir", "arbitrary"}); err == nil {
		t.Fatal("public arguments bypassed inherited handoff")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyDesktopRecoveryTarget(ctx, desktopRecoverySpec{}); err != context.Canceled {
		t.Fatal("cancelled target validation continued")
	}
}

func TestRecoveryReporterFailureRetainsLocalEvidence(t *testing.T) {
	spec, _ := recoveryEngineFixture(t)
	spec.Engine.URL = "http://127.0.0.1:1"
	report, finish := desktopRecoveryReporter(spec)
	report(desktop.RecoveryDiagnostic{SessionID: spec.SessionID, Phase: "probe_failed", At: time.Now()})
	finish()
	if body, err := os.ReadFile(spec.Journal); err != nil || !bytes.Contains(body, []byte("probe_failed")) {
		t.Fatal("Trace transport failure lost local evidence")
	}
}

func TestRecoveryReapsATermIgnoringChildAndTreatsPostCommitCloseAsCancellation(t *testing.T) {
	stubRecoverySuccessorFocus(t)
	for _, mode := range []string{"ignore_term", "drop_commit", "close_after_commit"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			command := exec.Command(os.Args[0], "-test.run=^TestDesktopRecoveryChildProtocol$")
			command.Env = append(os.Environ(), "ZWAI_TEST_RECOVERY_CHILD="+mode)
			err := runRecoverySuccessor(ctx, desktopRecoverySpec{SessionID: "session", Token: "token", Generation: 1}, command)
			if err == nil || command.ProcessState == nil {
				t.Fatal("unfinished child was accepted or not reaped")
			}
			if mode == "close_after_commit" && !errors.Is(err, desktop.ErrRecoveryCancelled) {
				t.Fatalf("user close was retried: %v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	exit := make(chan error)
	if err, observed := observeRecoveryChildExit(ctx, exit); !errors.Is(err, context.Canceled) || observed {
		t.Fatal("cancelled exit observation was lost")
	}
	exit = make(chan error, 1)
	failure := errors.New("fixture exit")
	exit <- failure
	if err, observed := observeRecoveryChildExit(context.Background(), exit); !errors.Is(err, failure) || !observed {
		t.Fatal("real child failure category was lost")
	}
	if _, err := desktopRecoveryExecutableHash(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing executable hashed")
	}
	if err := appendRecoveryDiagnostic(filepath.Join(t.TempDir(), "journal"), desktop.RecoveryDiagnostic{At: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}); err == nil {
		t.Fatal("invalid time produced an unverifiable journal row")
	}
}

func TestRecoveryDoesNotBuildOrAttachWhenInputsAndPrivateStorageAreInvalid(t *testing.T) {
	spec, _ := recoveryEngineFixture(t)
	source := filepath.Join(t.TempDir(), "source")
	_ = os.WriteFile(source, []byte("#!/bin/sh\nexit 0\n"), 0755)
	for _, mode := range []string{"bad_flag", "bad_config", "missing_engine", "changed_origin", "blocked_cache", "unwritable_cache", "missing_source", "blocked_diagnostics"} {
		t.Run(mode, func(t *testing.T) {
			args := []string{"--mock", "--data-dir", spec.DataDir}
			opts := desktop.Options{URL: spec.Engine.URL}
			cache := t.TempDir()
			executable := source
			switch mode {
			case "bad_flag":
				args = []string{"--unknown"}
			case "bad_config":
				blocked := filepath.Join(t.TempDir(), "file")
				_ = os.WriteFile(blocked, []byte("blocked"), 0600)
				args = []string{"--data-dir", blocked}
			case "missing_engine":
				args = []string{"--mock", "--data-dir", t.TempDir()}
			case "changed_origin":
				opts.URL = "http://127.0.0.1:1"
			case "blocked_cache":
				_ = os.WriteFile(filepath.Join(cache, "zwai"), []byte("blocked"), 0600)
			case "unwritable_cache":
				_ = os.Mkdir(filepath.Join(cache, "zwai"), 0500)
				defer os.Chmod(filepath.Join(cache, "zwai"), 0700)
			case "missing_source":
				executable = filepath.Join(t.TempDir(), "missing")
			case "blocked_diagnostics":
				_ = os.WriteFile(filepath.Join(spec.DataDir, "diagnostics"), []byte("blocked"), 0600)
				defer os.Remove(filepath.Join(spec.DataDir, "diagnostics"))
			}
			if _, _, err := configureDesktopRecoveryAt(opts, args, cache, executable); err == nil {
				t.Fatalf("invalid %s started recovery", mode)
			}
		})
	}
}

func TestRecoveryUsesExistingPresenceAndVerifiesItsPrivateExecutableBeforeRelaunch(t *testing.T) {
	spec, _ := recoveryEngineFixture(t)
	spec.Executable = filepath.Join(spec.PrivateDir, "successor")
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(os.Args[0], "'", "'\\''") + "' -test.run='^TestDesktopRecoveryChildProtocol$'\n"
	if err := os.WriteFile(spec.Executable, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	hash, err := desktopRecoveryExecutableHash(spec.Executable)
	if err != nil {
		t.Fatal(err)
	}
	spec.ExecutableSHA256 = hash
	t.Setenv("ZWAI_TEST_RECOVERY_CHILD", "ready")
	opts, cleanup, err := attachDesktopRecovery(desktop.Options{URL: spec.Engine.URL, Fatal: make(chan error)}, spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := opts.Recovery.Relaunch(context.Background(), desktop.RecoveryAttempt{Generation: 2, Attempts: 2}); err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := os.Stat(filepath.Join(spec.PrivateDir, "session")); err != nil {
		t.Fatal("old owner deleted a committed successor's copy")
	}
	spec.ExecutableSHA256 = "wrong"
	opts, cleanup, err = attachDesktopRecovery(desktop.Options{URL: spec.Engine.URL, Fatal: make(chan error)}, spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := opts.Recovery.Relaunch(context.Background(), desktop.RecoveryAttempt{Generation: 2, Attempts: 2}); err == nil {
		t.Fatal("changed executable was relaunched")
	}
	cleanup()
}

func TestReturningWithoutAStoppedControllerDoesNotDeleteItsRecoveryCopy(t *testing.T) {
	spec, _ := recoveryEngineFixture(t)
	opts, cleanup, err := attachDesktopRecovery(desktop.Options{URL: spec.Engine.URL, Fatal: make(chan error)}, spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := os.Stat(filepath.Join(spec.PrivateDir, "session")); err != nil {
		t.Fatal("return cleanup deleted the copy without proof that its controller stopped")
	}
	opts.Recovery.OnStopped()
	if _, err := os.Stat(spec.PrivateDir); !os.IsNotExist(err) {
		t.Fatal("confirmed controller stop leaked its private copy")
	}
}

func TestNativeQuitReapsItsOwnedSuccessorAtReadyAndCommitBeforeDeletingTheCopy(t *testing.T) {
	for _, stage := range []string{"ready", "commit"} {
		t.Run(stage, func(t *testing.T) {
			spec, _ := recoveryEngineFixture(t)
			spec.Executable = filepath.Join(spec.PrivateDir, "successor")
			script := "#!/bin/sh\nexec '" + strings.ReplaceAll(os.Args[0], "'", "'\\''") + "' -test.run='^TestDesktopRecoveryChildProtocol$'\n"
			if err := os.WriteFile(spec.Executable, []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			var err error
			spec.ExecutableSHA256, err = desktopRecoveryExecutableHash(spec.Executable)
			if err != nil {
				t.Fatal(err)
			}
			boundary := filepath.Join(t.TempDir(), "boundary")
			t.Setenv("ZWAI_TEST_RECOVERY_CHILD", "pause_"+stage)
			t.Setenv("ZWAI_TEST_RECOVERY_BOUNDARY", boundary)
			opts, cleanup, err := attachDesktopRecovery(desktop.Options{URL: spec.Engine.URL, Fatal: make(chan error)}, spec, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			appCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- opts.Recovery.Relaunch(appCtx, desktop.RecoveryAttempt{Generation: 2, Attempts: 2}) }()
			deadline := time.After(3 * time.Second)
			var childPID int
			for childPID == 0 {
				body, readErr := os.ReadFile(boundary)
				if readErr == nil {
					_, _ = fmt.Sscan(string(body), &childPID)
				}
				select {
				case <-deadline:
					t.Fatal("owned successor never reached the handoff boundary")
				case <-time.After(10 * time.Millisecond):
				}
			}
			// Wails cancels the application context before its ordered shutdown hooks.
			cancel()
			opts.OnShutdown()
			cleanup()
			if _, err := os.Stat(spec.PrivateDir); err != nil {
				t.Fatal("native quit removed the copy before controller stop was confirmed")
			}
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("native quit lost its cancellation: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("native quit did not reap its owned successor")
			}
			if err := syscall.Kill(childPID, 0); err != syscall.ESRCH {
				t.Fatalf("owned successor remains after cancellation: pid=%d err=%v", childPID, err)
			}
			opts.Recovery.OnStopped()
			if _, err := os.Stat(spec.PrivateDir); !os.IsNotExist(err) {
				t.Fatal("stopped native shell leaked its private copy")
			}
		})
	}
}

func TestNativeStoppedCallbackFlushesDiagnosticsWithoutReturningFromRun(t *testing.T) {
	spec, _ := recoveryEngineFixture(t)
	opts, cleanup, err := attachDesktopRecovery(desktop.Options{URL: spec.Engine.URL, Fatal: make(chan error)}, spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	opts.Recovery.Diagnostic(desktop.RecoveryDiagnostic{SessionID: spec.SessionID, Phase: "ready", At: time.Now()})
	opts.OnShutdown()
	opts.Recovery.OnStopped()
	if _, err := os.Stat(spec.PrivateDir); !os.IsNotExist(err) {
		t.Fatal("native callback leaked a copy when Run did not return")
	}
	body, err := os.ReadFile(spec.Journal)
	if err != nil || !bytes.Contains(body, []byte(`"phase":"ready"`)) {
		t.Fatalf("native callback lost its pending diagnostic: %s %v", body, err)
	}
}

func TestUnreapedSuccessorRetainsTheCopyEvenAfterItsControllerStops(t *testing.T) {
	spec, _ := recoveryEngineFixture(t)
	spec.Executable = filepath.Join(spec.PrivateDir, "successor")
	if err := os.WriteFile(spec.Executable, []byte("private executable"), 0755); err != nil {
		t.Fatal(err)
	}
	var err error
	spec.ExecutableSHA256, err = desktopRecoveryExecutableHash(spec.Executable)
	if err != nil {
		t.Fatal(err)
	}
	opts, cleanup, err := attachDesktopRecoveryWithLauncher(desktop.Options{URL: spec.Engine.URL, Fatal: make(chan error)}, spec, nil, func(context.Context, desktopRecoverySpec, *exec.Cmd) error { return desktop.ErrRecoveryCleanup })
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if err := opts.Recovery.Relaunch(context.Background(), desktop.RecoveryAttempt{Generation: 2, Attempts: 2}); !errors.Is(err, desktop.ErrRecoveryCleanup) {
		t.Fatalf("unreaped child failure changed: %v", err)
	}
	opts.Recovery.OnStopped()
	if _, err := os.Stat(spec.PrivateDir); err != nil {
		t.Fatal("controller stop deleted the copy of an unreaped successor")
	}
}

func TestAttachOnlyFailedCommitAndCancelledReadyNeverAcquireCleanupOwnership(t *testing.T) {
	for _, failure := range []string{"wrong_commit", "parent_eof", "closed_ready_writer", "cancelled_ready", "changed_engine"} {
		t.Run(failure, func(t *testing.T) {
			spec, _ := recoveryEngineFixture(t)
			var input bytes.Buffer
			_ = json.NewEncoder(&input).Encode(spec)
			if failure != "parent_eof" {
				token := spec.Token
				if failure == "wrong_commit" {
					token = "other"
				}
				_ = json.NewEncoder(&input).Encode(desktopRecoveryCommit{Token: token, Commit: true})
			}
			var output io.Writer = io.Discard
			if failure == "closed_ready_writer" {
				file, err := os.CreateTemp(t.TempDir(), "closed")
				if err != nil {
					t.Fatal(err)
				}
				_ = file.Close()
				output = file
			}
			if failure == "changed_engine" {
				spec.Engine.PID++
				input.Reset()
				_ = json.NewEncoder(&input).Encode(spec)
			}
			previous := openDesktopWindow
			defer func() { openDesktopWindow = previous }()
			openDesktopWindow = func(opts desktop.Options) error {
				ctx := context.Background()
				if failure == "cancelled_ready" {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				return opts.Recovery.Ready(ctx)
			}
			if err := runDesktopAttachment(&input, output); err == nil {
				t.Fatalf("unfinished %s took ownership", failure)
			}
			if _, err := os.Stat(filepath.Join(spec.PrivateDir, "session")); err != nil {
				t.Fatal("uncommitted shell deleted parent copy")
			}
		})
	}
}

func TestRecoveryRejectsCorruptFramesAndAnEngineChangedAfterReady(t *testing.T) {
	for _, mode := range []string{"bad_ready", "bad_ack", "change_lease"} {
		t.Run(mode, func(t *testing.T) {
			spec, _ := recoveryEngineFixture(t)
			command := exec.Command(os.Args[0], "-test.run=^TestDesktopRecoveryChildProtocol$")
			command.Env = append(os.Environ(), "ZWAI_TEST_RECOVERY_CHILD="+mode)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := runRecoverySuccessor(ctx, spec, command); err == nil {
				t.Fatalf("invalid %s was committed", mode)
			}
			if command.ProcessState == nil {
				t.Fatal("invalid successor was not reaped")
			}
		})
	}
	if err := runRecoverySuccessor(context.Background(), desktopRecoverySpec{}, exec.Command(filepath.Join(t.TempDir(), "missing"))); err == nil {
		t.Fatal("failed native launch was accepted")
	}
	command := exec.Command(os.Args[0], "-test.run=^TestDesktopRecoveryChildProtocol$")
	command.Env = append(os.Environ(), "ZWAI_TEST_RECOVERY_CHILD=wait")
	if err := runRecoverySuccessor(context.Background(), desktopRecoverySpec{Engine: lease.Record{StartedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}}, command); err == nil {
		t.Fatal("invalid bootstrap was sent to native child")
	}
	exit := make(chan error, 1)
	exit <- nil
	if err := stopRecoveryChild(nil, exit); err != nil {
		t.Fatal("already reaped child cleanup was not idempotent")
	}
}

func TestRecoveryTargetCancellationAfterMetadataKeepsItsCause(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": dir, "mock": true, "version": "fixture"})
	}))
	defer ts.Close()
	record := lease.Record{PID: os.Getpid(), URL: ts.URL, Mock: true, Version: "fixture", Dev: 1, Ino: 2}
	if err := lease.Publish(dir, record); err != nil {
		t.Fatal(err)
	}
	if err := verifyDesktopRecoveryTarget(ctx, desktopRecoverySpec{DataDir: dir, Mock: true, Engine: record}); !errors.Is(err, context.Canceled) {
		t.Fatalf("metadata reply overrode cancellation: %v", err)
	}
	if err := verifyDesktopRecoveryTarget(context.Background(), desktopRecoverySpec{DataDir: t.TempDir()}); err == nil {
		t.Fatal("missing original engine was accepted")
	}
	if _, err := desktopRecoveryExecutableHash(t.TempDir()); err == nil {
		t.Fatal("directory accepted as private executable")
	}
}

func TestRecoveryReporterStaysNonblockingWhileTraceAndJournalFail(t *testing.T) {
	spec, _ := recoveryEngineFixture(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan struct{})
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			_ = connection.Close()
		}
		close(accepted)
	}()
	spec.Engine.URL = "http://" + listener.Addr().String()
	spec.Journal = t.TempDir()
	report, finish := desktopRecoveryReporter(spec)
	report(desktop.RecoveryDiagnostic{SessionID: spec.SessionID, Phase: "probe_failed", At: time.Now()})
	select {
	case <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("Trace transport was never attempted")
	}
	started := time.Now()
	for i := 0; i < 100; i++ {
		report(desktop.RecoveryDiagnostic{SessionID: spec.SessionID, Phase: "probe_failed", At: time.Now()})
	}
	if time.Since(started) > time.Second {
		t.Fatal("diagnostic failure blocked renderer recovery")
	}
	started = time.Now()
	finish()
	if time.Since(started) > 2*time.Second {
		t.Fatal("reporter kept attempting Trace requests after shutdown")
	}
}
