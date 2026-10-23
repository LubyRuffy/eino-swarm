//go:build darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/desktop"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
)

func TestRecoveryAttachesOnlyToItsOriginalEngineWithoutPreparingFrontend(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": dir, "mock": true, "version": "fixture"})
	}))
	defer server.Close()
	record := lease.Record{PID: os.Getpid(), URL: server.URL, Mock: true, Version: "fixture", Dev: 1, Ino: 2}
	if err := lease.Publish(dir, record); err != nil {
		t.Fatal(err)
	}
	spec := desktopRecoverySpec{SessionID: "session", DataDir: dir, Mock: true, Engine: record, Generation: 1, Attempts: 1}
	if err := verifyDesktopRecoveryTarget(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	other := spec
	other.Engine.PID++
	if err := verifyDesktopRecoveryTarget(context.Background(), other); err == nil {
		t.Fatal("recovery attached to an engine from another generation")
	}
	other = spec
	other.Engine.URL = "http://127.0.0.1:1"
	if err := verifyDesktopRecoveryTarget(context.Background(), other); err == nil {
		t.Fatal("recovery accepted a changed origin")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyDesktopRecoveryTarget(ctx, spec); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	url := recoveryWindowURL(server.URL, "th_actual", "pr_actual")
	if !strings.Contains(url, "pid="+fmt.Sprint(os.Getpid())) || !strings.Contains(url, "recovery_thread=th_actual") || strings.Contains(url, "pid=0") {
		t.Fatalf("successor window has wrong attribution: %s", url)
	}
}

func TestRecoverySuccessorNeedsRealReadyAndCommitAndReapsOnlyItsChild(t *testing.T) {
	if os.Getenv("ZWAI_TEST_RECOVERY_CHILD") != "" {
		return
	}
	for _, mode := range []string{"ready", "wrong_pid", "wait"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
			defer cancel()
			command := exec.Command(os.Args[0], "-test.run=^TestDesktopRecoveryChildProtocol$")
			command.Env = append(os.Environ(), "ZWAI_TEST_RECOVERY_CHILD="+mode)
			spec := desktopRecoverySpec{SessionID: "protocol", Generation: 3, Attempts: 2, Token: "unique"}
			err := runRecoverySuccessor(ctx, spec, command)
			if mode == "ready" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("unready successor was accepted")
			}
			if mode != "ready" && command.ProcessState == nil {
				t.Fatal("failed child was not reaped")
			}
		})
	}
}

func TestDesktopRecoveryChildProtocol(t *testing.T) {
	mode := os.Getenv("ZWAI_TEST_RECOVERY_CHILD")
	if mode == "" {
		return
	}
	control := os.NewFile(4, "control")
	ready := os.NewFile(3, "ready")
	var spec desktopRecoverySpec
	decoder := json.NewDecoder(control)
	if decoder.Decode(&spec) != nil {
		os.Exit(3)
	}
	if mode == "ignore_term" {
		signal.Ignore(syscall.SIGTERM)
		time.Sleep(time.Minute)
		os.Exit(8)
	}
	if mode == "wait" {
		time.Sleep(time.Minute)
		os.Exit(4)
	}
	if mode == "exit" {
		os.Exit(7)
	}
	if mode == "user_close" {
		os.Exit(0)
	}
	if mode == "pause_ready" {
		_ = os.WriteFile(os.Getenv("ZWAI_TEST_RECOVERY_BOUNDARY"), []byte(fmt.Sprint(os.Getpid())), 0600)
		time.Sleep(time.Minute)
		os.Exit(12)
	}
	if mode == "bad_ready" {
		_, _ = ready.Write([]byte("{]"))
		time.Sleep(time.Minute)
		os.Exit(10)
	}
	if mode == "change_lease" {
		changed := spec.Engine
		changed.PID++
		_ = lease.Publish(spec.DataDir, changed)
	}
	pid := os.Getpid()
	if mode == "wrong_pid" {
		pid++
	}
	if mode == "drop_commit" {
		_ = control.Close()
	}
	_ = json.NewEncoder(ready).Encode(desktopRecoveryReady{SessionID: spec.SessionID, Generation: spec.Generation, Token: spec.Token, PID: pid})
	if mode == "drop_commit" {
		time.Sleep(time.Minute)
		os.Exit(9)
	}
	var commit desktopRecoveryCommit
	if decoder.Decode(&commit) != nil || commit.Token != spec.Token || !commit.Commit {
		os.Exit(5)
	}
	if mode == "pause_commit" {
		_ = os.WriteFile(os.Getenv("ZWAI_TEST_RECOVERY_BOUNDARY"), []byte(fmt.Sprint(os.Getpid())), 0600)
		time.Sleep(time.Minute)
		os.Exit(13)
	}
	if mode == "no_commit" {
		time.Sleep(time.Minute)
		os.Exit(6)
	}
	if mode == "close_after_commit" {
		os.Exit(0)
	}
	if mode == "bad_ack" {
		_, _ = ready.Write([]byte("{]"))
		time.Sleep(time.Minute)
		os.Exit(11)
	}
	if mode == "wrong_commit" {
		pid++
	}
	_ = json.NewEncoder(ready).Encode(desktopRecoveryReady{SessionID: spec.SessionID, Generation: spec.Generation, Token: spec.Token, PID: pid, Committed: true})
	os.Exit(0)
}

func recoveryEngineFixture(t *testing.T) (desktopRecoverySpec, <-chan desktop.RecoveryDiagnostic) {
	t.Helper()
	dir := t.TempDir()
	events := make(chan desktop.RecoveryDiagnostic, 100)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/meta":
			_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": dir, "mock": true, "version": "fixture"})
		case "/api/presence":
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "pc_0123456789abcdef"})
		case "/api/presence/pc_0123456789abcdef":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(": held\n\n"))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case "/api/desktop/recovery":
			var event desktop.RecoveryDiagnostic
			_ = json.NewDecoder(r.Body).Decode(&event)
			events <- event
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	record := lease.Record{PID: os.Getpid(), URL: server.URL, Mock: true, Version: "fixture", Dev: 1, Ino: 2}
	if err := lease.Publish(dir, record); err != nil {
		t.Fatal(err)
	}
	private, err := os.MkdirTemp(t.TempDir(), "desktop-recovery-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(private, "session"), []byte("session"), 0600); err != nil {
		t.Fatal(err)
	}
	return desktopRecoverySpec{SessionID: "session", Token: "private-token", DataDir: dir, Mock: true, Engine: record, PrivateDir: private, Journal: filepath.Join(dir, "recovery.jsonl"), Generation: 1, Attempts: 1}, events
}

func TestConfiguredRecoveryOwnsPrivateCopyPresenceAndAsyncTrace(t *testing.T) {
	spec, events := recoveryEngineFixture(t)
	var shutdown atomic.Bool
	source := filepath.Join(t.TempDir(), "source")
	if err := os.WriteFile(source, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	opts, cleanup, err := configureDesktopRecoveryAt(desktop.Options{URL: spec.Engine.URL, OnShutdown: func() { shutdown.Store(true) }}, []string{"--mock", "--data-dir", spec.DataDir}, t.TempDir(), source)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Recovery == nil {
		t.Fatal("native shell has no recovery path")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := opts.Recovery.Relaunch(ctx, desktop.RecoveryAttempt{Generation: 1, Attempts: 1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled recovery started a successor: %v", err)
	}
	opts.Recovery.Diagnostic(desktop.RecoveryDiagnostic{SessionID: opts.Recovery.SessionID, Phase: "reloading", Reason: "fixture", At: time.Now()})
	select {
	case event := <-events:
		if event.EnginePID != spec.Engine.PID {
			t.Fatal("Trace diagnostic attributed another engine")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("async Trace diagnostic missing")
	}
	opts.OnShutdown()
	cleanup()
	if !shutdown.Load() {
		t.Fatal("recovery replaced existing shutdown callback")
	}
}

func TestUncommittedSuccessorDoesNotDeleteItsParentsRecoveryCopy(t *testing.T) {
	for _, commit := range []bool{false, true} {
		t.Run(fmt.Sprint(commit), func(t *testing.T) {
			spec, _ := recoveryEngineFixture(t)
			opts, cleanup, err := attachDesktopRecovery(desktop.Options{URL: spec.Engine.URL}, spec, func(context.Context) error {
				if commit {
					return nil
				}
				return errors.New("parent cancelled")
			})
			if err != nil {
				t.Fatal(err)
			}
			err = opts.Recovery.Ready(context.Background())
			if (err == nil) != commit {
				t.Fatal("handoff changed commit result")
			}
			cleanup()
			if _, err := os.Stat(spec.PrivateDir); err != nil {
				t.Fatal("return cleanup bypassed the running controller")
			}
			opts.Recovery.OnStopped()
			_, err = os.Stat(spec.PrivateDir)
			if commit && !os.IsNotExist(err) {
				t.Fatal("committed shell leaked private copy")
			}
			if !commit && err != nil {
				t.Fatal("uncommitted shell deleted parent copy")
			}
		})
	}
}

func TestAttachOnlyEntryRequiresCommitAfterRendererReady(t *testing.T) {
	spec, _ := recoveryEngineFixture(t)
	spec.ThreadID = "th_real"
	spec.DraftKey = "thread:th_real"
	controlReader, controlWriter := io.Pipe()
	readyReader, readyWriter := io.Pipe()
	defer controlReader.Close()
	defer controlWriter.Close()
	defer readyReader.Close()
	defer readyWriter.Close()
	previous := openDesktopWindow
	defer func() { openDesktopWindow = previous }()
	openDesktopWindow = func(opts desktop.Options) error {
		if !strings.Contains(opts.URL, "recovery_draft=thread%3Ath_real") || !strings.Contains(opts.URL, "recovery_thread=th_real") {
			return fmt.Errorf("wrong recovery URL: %s", opts.URL)
		}
		return opts.Recovery.Ready(context.Background())
	}
	done := make(chan error, 1)
	go func() { done <- runDesktopAttachment(controlReader, readyWriter) }()
	if err := json.NewEncoder(controlWriter).Encode(spec); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(readyReader)
	var ready desktopRecoveryReady
	if err := decoder.Decode(&ready); err != nil {
		t.Fatal(err)
	}
	if ready.Committed || ready.PID != os.Getpid() {
		t.Fatal("ready fabricated an early commit")
	}
	if err := json.NewEncoder(controlWriter).Encode(desktopRecoveryCommit{Token: spec.Token, Commit: true}); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&ready); err != nil || !ready.Committed {
		t.Fatalf("missing commit receipt: %+v %v", ready, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryOwnershipAndHandshakeCancellationFailures(t *testing.T) {
	for _, mode := range []string{"exit", "no_commit", "wrong_commit"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			command := exec.Command(os.Args[0], "-test.run=^TestDesktopRecoveryChildProtocol$")
			command.Env = append(os.Environ(), "ZWAI_TEST_RECOVERY_CHILD="+mode)
			err := runRecoverySuccessor(ctx, desktopRecoverySpec{SessionID: "session", Token: "token", Generation: 1}, command)
			if err == nil {
				t.Fatal("unfinished handoff was accepted")
			}
			if command.ProcessState == nil {
				t.Fatal("failed child was not reaped")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	command := exec.Command("unused")
	if err := runRecoverySuccessor(ctx, desktopRecoverySpec{}, command); !errors.Is(err, context.Canceled) || command.Process != nil {
		t.Fatal("pre-start cancellation still launched a child")
	}
}

func TestNormalSuccessorCloseCannotRaceEOFIntoAnotherAutomaticRestart(t *testing.T) {
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		command := exec.Command(os.Args[0], "-test.run=^TestDesktopRecoveryChildProtocol$")
		command.Env = append(os.Environ(), "ZWAI_TEST_RECOVERY_CHILD=user_close")
		err := runRecoverySuccessor(ctx, desktopRecoverySpec{SessionID: "session", Token: "token", Generation: 1}, command)
		cancel()
		if !errors.Is(err, desktop.ErrRecoveryCancelled) {
			t.Fatalf("normal user close became recoverable failure: %v", err)
		}
	}
}

func TestDesktopRecoveryJournalIsBoundedAndContainsOnlyOneSessionFile(t *testing.T) {
	dir := t.TempDir()
	journal := filepath.Join(dir, "recovery.jsonl")
	for i := 0; i < 200; i++ {
		if err := appendRecoveryDiagnostic(journal, desktop.RecoveryDiagnostic{SessionID: "one", Phase: "probe_failed", Reason: strings.Repeat("x", 512), At: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > desktopRecoveryJournalLimit {
		t.Fatalf("diagnostic file grew to %d bytes", len(data))
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var row desktop.RecoveryDiagnostic
		if json.Unmarshal([]byte(line), &row) != nil || row.SessionID != "one" {
			t.Fatalf("journal has partial row: %q", line)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("journal created rotation files: %v %v", entries, err)
	}
}
