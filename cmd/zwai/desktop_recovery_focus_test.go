//go:build darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/desktop"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
)

func replaceRecoverySuccessorFocus(t *testing.T, focus func(context.Context, int, string) error) {
	t.Helper()
	previous := focusRecoverySuccessor
	focusRecoverySuccessor = focus
	t.Cleanup(func() { focusRecoverySuccessor = previous })
}

func stubRecoverySuccessorFocus(t *testing.T) {
	t.Helper()
	replaceRecoverySuccessorFocus(t, func(ctx context.Context, _ int, _ string) error { return ctx.Err() })
}

func recoveryFocusChild(t *testing.T, mode string) (*exec.Cmd, string) {
	t.Helper()
	boundary := filepath.Join(t.TempDir(), "protocol")
	command := exec.Command(os.Args[0], "-test.run=^TestDesktopRecoveryFocusProtocolChild$")
	command.Env = append(os.Environ(), "ZWAI_TEST_RECOVERY_FOCUS_CHILD="+mode,
		"ZWAI_TEST_RECOVERY_FOCUS_BOUNDARY="+boundary)
	return command, boundary
}

func TestDesktopRecoveryFocusProtocolChild(t *testing.T) {
	mode := os.Getenv("ZWAI_TEST_RECOVERY_FOCUS_CHILD")
	if mode == "" {
		return
	}
	control, ready := os.NewFile(4, "control"), os.NewFile(3, "ready")
	decoder := json.NewDecoder(control)
	var spec desktopRecoverySpec
	if decoder.Decode(&spec) != nil {
		os.Exit(3)
	}
	boundary := os.Getenv("ZWAI_TEST_RECOVERY_FOCUS_BOUNDARY")
	if os.WriteFile(boundary+".spec", []byte(fmt.Sprint(os.Getpid())), 0600) != nil {
		os.Exit(4)
	}
	if mode == "exit" {
		os.Exit(7)
	}
	if mode == "user_close" {
		os.Exit(0)
	}
	if mode == "wait" {
		time.Sleep(time.Minute)
		os.Exit(8)
	}
	if mode == "focus_then_ready" {
		for {
			if _, err := os.Stat(boundary + ".focus"); err == nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
	}
	pid := os.Getpid()
	if mode == "wrong_pid" {
		pid++
	}
	if json.NewEncoder(ready).Encode(desktopRecoveryReady{SessionID: spec.SessionID, Generation: spec.Generation, Token: spec.Token, PID: pid}) != nil {
		os.Exit(5)
	}
	var commit desktopRecoveryCommit
	if decoder.Decode(&commit) != nil || commit.Token != spec.Token || !commit.Commit {
		os.Exit(6)
	}
	_ = os.WriteFile(boundary+".commit", []byte("committed"), 0600)
	if json.NewEncoder(ready).Encode(desktopRecoveryReady{SessionID: spec.SessionID, Generation: spec.Generation, Token: spec.Token, PID: pid, Committed: true}) != nil {
		os.Exit(9)
	}
	os.Exit(0)
}

func TestRecoveryParentFocusRunsAfterSpecAndStillRequiresRealReadyAndCommit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command, boundary := recoveryFocusChild(t, "focus_then_ready")
	var calls atomic.Int32
	wantedDeadline, _ := ctx.Deadline()
	replaceRecoverySuccessorFocus(t, func(actual context.Context, pid int, executable string) error {
		calls.Add(1)
		if deadline, ok := actual.Deadline(); !ok || deadline != wantedDeadline {
			return errors.New("focus replaced the original deadline")
		}
		if command.Process == nil || pid != command.Process.Pid || executable != command.Path {
			return errors.New("focus did not receive the actual owned process")
		}
		for {
			body, err := os.ReadFile(boundary + ".spec")
			if err == nil {
				if string(body) != fmt.Sprint(pid) {
					return errors.New("focus observed another child spec")
				}
				return os.WriteFile(boundary+".focus", []byte("one request"), 0600)
			}
			select {
			case <-actual.Done():
				return actual.Err()
			case <-time.After(time.Millisecond):
			}
		}
	})
	err := runRecoverySuccessor(ctx, desktopRecoverySpec{SessionID: "session", Generation: 1, Token: "token"}, command)
	if err != nil || calls.Load() != 1 {
		t.Fatalf("parent did not coordinate once after sending the spec: calls=%d error=%v", calls.Load(), err)
	}
	if _, err := os.Stat(boundary + ".commit"); err != nil {
		t.Fatal("focus request replaced the real ready/commit exchange", err)
	}
}

func TestRecoveryParentFocusNilDoesNotManufactureReadyOrAcceptWrongPID(t *testing.T) {
	for _, mode := range []string{"wait", "wrong_pid"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
			defer cancel()
			command, boundary := recoveryFocusChild(t, mode)
			var calls atomic.Int32
			replaceRecoverySuccessorFocus(t, func(context.Context, int, string) error { calls.Add(1); return nil })
			err := runRecoverySuccessor(ctx, desktopRecoverySpec{SessionID: "session", Generation: 1, Token: "token"}, command)
			if err == nil || calls.Load() != 1 || command.ProcessState == nil {
				t.Fatalf("focus accepted missing/foreign ready or leaked child: calls=%d error=%v", calls.Load(), err)
			}
			if _, err := os.Stat(boundary + ".commit"); !os.IsNotExist(err) {
				t.Fatal("invalid ready reached commit", err)
			}
		})
	}
}

func TestRecoveryParentFocusCancellationAndChildExitJoinObservation(t *testing.T) {
	for _, mode := range []string{"wait", "exit", "user_close"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			command, boundary := recoveryFocusChild(t, mode)
			entered, joined := make(chan struct{}), make(chan struct{})
			replaceRecoverySuccessorFocus(t, func(actual context.Context, _ int, _ string) error {
				close(entered)
				defer close(joined)
				<-actual.Done()
				return actual.Err()
			})
			done := make(chan error, 1)
			go func() {
				done <- runRecoverySuccessor(ctx, desktopRecoverySpec{SessionID: "session", Generation: 1, Token: "token"}, command)
			}()
			select {
			case <-entered:
			case err := <-done:
				t.Fatalf("parent skipped its observation: %v", err)
			case <-time.After(time.Second):
				cancel()
				t.Fatal("parent did not enter owned observation")
			}
			if mode == "wait" {
				cancel()
			}
			err := <-done
			select {
			case <-joined:
			default:
				t.Fatal("parent returned with a detached focus observer")
			}
			if mode == "wait" && !errors.Is(err, context.Canceled) {
				t.Fatalf("focus observation hid cancellation: %v", err)
			}
			if mode == "user_close" && !errors.Is(err, desktop.ErrRecoveryCancelled) {
				t.Fatalf("normal child exit became a focus failure: %v", err)
			}
			var exited *exec.ExitError
			if mode == "exit" && !errors.As(err, &exited) {
				t.Fatalf("child failure lost its actual exit cause: %v", err)
			}
			if command.ProcessState == nil {
				t.Fatal("cancelled/exited child was not reaped")
			}
			if _, err := os.Stat(boundary + ".commit"); !os.IsNotExist(err) {
				t.Fatal("pending observation committed an early ready", err)
			}
		})
	}
}

func TestRecoveryParentFocusPendingCannotBlockRealReadyAndJoinsBeforeCommit(t *testing.T) {
	// Guard the whole child protocol, including process startup. The causal
	// gate, rather than cold-start speed, proves Ready bypasses pending focus.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	command, boundary := recoveryFocusChild(t, "focus_then_ready")
	joined := make(chan struct{})
	checked := make(chan error, 1)
	replaceRecoverySuccessorFocus(t, func(actual context.Context, _ int, _ string) error {
		defer close(joined)
		// Only an entered observer releases the child's actual Ready frame,
		// while the observer itself remains pending until the parent cancels it.
		if err := os.WriteFile(boundary+".focus", []byte("release ready"), 0600); err != nil {
			checked <- err
			return err
		}
		<-actual.Done()
		if _, err := os.Stat(boundary + ".commit"); !os.IsNotExist(err) {
			checked <- errors.New("parent committed before cancelling its focus observer")
		} else {
			checked <- nil
		}
		return actual.Err()
	})
	if err := runRecoverySuccessor(ctx, desktopRecoverySpec{SessionID: "session", Generation: 1, Token: "token"}, command); err != nil {
		t.Fatalf("registration observation blocked the real ready frame: %v", err)
	}
	select {
	case <-joined:
	default:
		t.Fatal("real Ready left the focus observation detached")
	}
	if err := <-checked; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(boundary + ".commit"); err != nil {
		t.Fatal("real Ready did not complete the original commit exchange", err)
	}
}

type observedRecoveryFocusLoss struct {
	err       error
	processed chan struct{}
	once      sync.Once
}

func (loss *observedRecoveryFocusLoss) Error() string {
	loss.once.Do(func() { close(loss.processed) })
	return loss.err.Error()
}
func (loss *observedRecoveryFocusLoss) Unwrap() error { return loss.err }

func TestRecoveryParentFocusKernelLossCannotReplaceActualChildExit(t *testing.T) {
	for _, mode := range []string{"exit", "user_close"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			command, _ := recoveryFocusChild(t, mode)
			reaped, decoded, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var released sync.Once
			unblock := func() { released.Do(func() { close(release) }) }
			defer unblock()
			previousWait, previousRead, previousLogger := waitRecoverySuccessor, readRecoveryReady, slog.Default()
			t.Cleanup(func() {
				waitRecoverySuccessor, readRecoveryReady = previousWait, previousRead
				slog.SetDefault(previousLogger)
			})
			slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
			// Both OS results are real. Hold only their delivery to reproduce the
			// scheduling gap where a dead PID is observed before Wait/EOF arrives.
			waitRecoverySuccessor = func(child *exec.Cmd) error {
				err := previousWait(child)
				close(reaped)
				<-release
				return err
			}
			readRecoveryReady = func(decoder *json.Decoder, value any) error {
				err := previousRead(decoder, value)
				close(decoded)
				<-release
				return err
			}
			processed := make(chan struct{})
			captured := make(chan error, 1)
			replaceRecoverySuccessorFocus(t, func(actual context.Context, pid int, executable string) error {
				for _, observed := range []<-chan struct{}{reaped, decoded} {
					select {
					case <-observed:
					case <-actual.Done():
						return actual.Err()
					}
				}
				if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
					failure := fmt.Errorf("child kernel PID did not disappear: %v", err)
					captured <- failure
					return &observedRecoveryFocusLoss{err: failure, processed: processed}
				}
				// The actual native capture rejects the reaped child before any
				// main-queue/AppKit action, so this never accesses the host GUI.
				err := desktop.FocusRecoverySuccessor(actual, pid, executable)
				captured <- err
				if !errors.Is(err, desktop.ErrRecoveryFocusIdentity) {
					return &observedRecoveryFocusLoss{err: fmt.Errorf("native capture did not report identity loss: %v", err), processed: processed}
				}
				return &observedRecoveryFocusLoss{err: err, processed: processed}
			})
			done := make(chan error, 1)
			go func() {
				done <- runRecoverySuccessor(ctx, desktopRecoverySpec{SessionID: "session", Generation: 1, Token: "token"}, command)
			}()
			select {
			case <-processed:
			case <-ctx.Done():
				unblock()
				err := <-done
				t.Fatalf("parent did not process the real kernel identity loss before Wait/EOF delivery: %v", err)
			}
			unblock()
			err := <-done
			if actual := <-captured; !errors.Is(actual, desktop.ErrRecoveryFocusIdentity) {
				t.Fatalf("actual native capture did not witness kernel identity loss: %v", actual)
			}
			if errors.Is(err, desktop.ErrRecoveryFocusIdentity) {
				t.Fatalf("auxiliary identity loss replaced actual %s: %v", mode, err)
			}
			if mode == "user_close" && !errors.Is(err, desktop.ErrRecoveryCancelled) {
				t.Fatalf("natural close became restartable: %v", err)
			}
			var exited *exec.ExitError
			if mode == "exit" && !errors.As(err, &exited) {
				t.Fatalf("actual child exit cause lost: %v", err)
			}
		})
	}
}

func TestRecoveryParentFocusFailureCannotManufactureReadyOrRunBeforeSpecEncoding(t *testing.T) {
	for _, mode := range []string{"focus_error", "bad_spec"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
			defer cancel()
			command, _ := recoveryFocusChild(t, "wait")
			failure := errors.New("observed focus identity failure")
			var calls atomic.Int32
			replaceRecoverySuccessorFocus(t, func(context.Context, int, string) error { calls.Add(1); return failure })
			spec := desktopRecoverySpec{SessionID: "session", Generation: 1, Token: "token"}
			if mode == "bad_spec" {
				spec.Engine = lease.Record{StartedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}
			}
			err := runRecoverySuccessor(ctx, spec, command)
			if err == nil || command.ProcessState == nil {
				t.Fatalf("failed startup accepted or leaked child: %v", err)
			}
			if mode == "focus_error" && (!errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1) {
				t.Fatalf("auxiliary focus error replaced the original ready deadline: calls=%d error=%v", calls.Load(), err)
			}
			if mode == "bad_spec" && calls.Load() != 0 {
				t.Fatal("focus ran before the complete spec was encoded")
			}
		})
	}
}
