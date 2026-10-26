//go:build darwin

package desktop

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"image/color"
	"os/exec"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func TestRecoverySuccessorDoesNotRequestItsOwnActivationOnNavigation(t *testing.T) {
	for _, ready := range []func(context.Context) error{nil, func(context.Context) error { return nil }} {
		host := &recoveryTestHost{window: &directoryRecordingWindow{}, windowEvents: make(map[events.WindowEventType]func()), appEvents: make(map[events.ApplicationEventType]func())}
		controller := startRecoveryForHost(host, Options{URL: "http://127.0.0.1:1234", Recovery: &RecoveryOptions{Ready: ready}})
		if host.windowEvents[events.Mac.WebViewDidFinishNavigation] != nil {
			t.Fatal("a successor retained a competing self-activation path")
		}
		controller.shutdown()
	}
}

func TestOwnedRecoverySuccessorWaitsForRegistrationAndRequestsOnlyOnce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	birth := recoverySuccessorBirth{1, 2}
	captures, observations := 0, 0
	err := focusRecoverySuccessor(ctx, 42, "/private/child", func(pid int, path string) (recoverySuccessorBirth, error) {
		captures++
		if pid != 42 || path != "/private/child" {
			t.Fatal("capture changed the owned target")
		}
		return birth, nil
	}, func(actual context.Context, pid int, path string, expected recoverySuccessorBirth) (int, error) {
		if actual != ctx || pid != 42 || path != "/private/child" || expected != birth {
			t.Fatal("registration lost the original context or birth")
		}
		observations++
		if observations < 3 {
			return recoveryFocusPending, nil
		}
		return recoveryFocusRequested, nil
	})
	if err != nil || captures != 1 || observations != 3 {
		t.Fatalf("err=%v captures=%d observations=%d", err, captures, observations)
	}
}

func TestOwnedRecoverySuccessorPreservesRefusalsIdentityAndCancellation(t *testing.T) {
	captureFailure := errors.New("kernel identity unavailable")
	requestFailure := errors.New("main dispatch failed")
	for _, mode := range []string{"invalid-pid", "relative-path", "cancelled", "capture-error", "capture-cancel", "request-error", "identity-changed", "unknown", "refused", "pending-cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			pid, path := 42, "/private/child"
			var want error
			switch mode {
			case "invalid-pid":
				pid = 1
				want = ErrRecoveryFocusIdentity
			case "relative-path":
				path = "child"
				want = ErrRecoveryFocusIdentity
			case "cancelled":
				cancel()
				want = context.Canceled
			case "capture-error":
				want = captureFailure
			case "capture-cancel", "pending-cancel":
				want = context.Canceled
			case "request-error":
				want = requestFailure
			case "identity-changed", "unknown":
				want = ErrRecoveryFocusIdentity
			}
			requests := 0
			err := focusRecoverySuccessor(ctx, pid, path, func(int, string) (recoverySuccessorBirth, error) {
				if mode == "capture-error" {
					return recoverySuccessorBirth{}, captureFailure
				}
				if mode == "capture-cancel" {
					cancel()
				}
				return recoverySuccessorBirth{1, 2}, nil
			}, func(context.Context, int, string, recoverySuccessorBirth) (int, error) {
				requests++
				switch mode {
				case "capture-cancel":
					t.Fatal("cancelled capture reached native dispatch")
				case "request-error":
					return 0, requestFailure
				case "identity-changed":
					return recoveryFocusIdentityChanged, nil
				case "unknown":
					return 99, nil
				case "pending-cancel":
					cancel()
					return recoveryFocusPending, nil
				}
				return recoveryFocusRefused, nil
			})
			if !errors.Is(err, want) {
				t.Fatalf("err=%v want=%v", err, want)
			}
			if requests > 1 {
				t.Fatal("a refusal retried activation")
			}
		})
	}
}

func TestParentActivationRechecksCancellationOnMain(t *testing.T) {
	previousDispatch, previousNative := recoveryFocusOnMain, recoveryFocusNative
	t.Cleanup(func() { recoveryFocusOnMain, recoveryFocusNative = previousDispatch, previousNative })
	var calls int
	recoveryFocusNative = func(pid int, path string, birth recoverySuccessorBirth) int {
		calls++
		if pid != 42 || path != "/child" || birth != (recoverySuccessorBirth{1, 2}) {
			t.Fatal("main dispatch changed target identity")
		}
		return recoveryFocusRequested
	}
	ctx, cancel := context.WithCancel(context.Background())
	queued := make(chan func(), 1)
	recoveryFocusOnMain = func(fn func()) { queued <- fn }
	done := make(chan error, 1)
	go func() {
		_, err := requestRecoverySuccessorActivation(ctx, 42, "/child", recoverySuccessorBirth{1, 2})
		done <- err
	}()
	pending := <-queued
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	pending()
	if calls != 0 {
		t.Fatal("a cancelled queued action activated the child")
	}
	recoveryFocusOnMain = func(fn func()) { fn() }
	status, err := requestRecoverySuccessorActivation(context.Background(), 42, "/child", recoverySuccessorBirth{1, 2})
	if err != nil || status != recoveryFocusRequested || calls != 1 {
		t.Fatalf("status=%d err=%v calls=%d", status, err, calls)
	}
	late, stop := context.WithCancel(context.Background())
	recoveryFocusNative = func(int, string, recoverySuccessorBirth) int { stop(); return recoveryFocusRequested }
	if _, err := requestRecoverySuccessorActivation(late, 42, "/child", recoverySuccessorBirth{1, 2}); !errors.Is(err, context.Canceled) {
		t.Fatal("late cancellation became activation success")
	}
}

func TestOwnedRecoveryCaptureUsesRealKernelBirthAndExecutable(t *testing.T) {
	child := exec.Command("/bin/sleep", "5")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	first, err := captureRecoverySuccessor(child.Process.Pid, "/bin/sleep")
	if err != nil || first.seconds == 0 {
		t.Fatalf("birth=%v err=%v", first, err)
	}
	second, err := captureRecoverySuccessor(child.Process.Pid, "/bin/sleep")
	if err != nil || second != first {
		t.Fatalf("birth changed: %v %v", second, err)
	}
	if _, err := captureRecoverySuccessor(child.Process.Pid, "/bin/cat"); !errors.Is(err, ErrRecoveryFocusIdentity) {
		t.Fatal("different executable was accepted")
	}
	if err := FocusRecoverySuccessor(context.Background(), 0, "/child"); !errors.Is(err, ErrRecoveryFocusIdentity) {
		t.Fatal("invalid public request was accepted")
	}
	// The Go test thread is not AppKit's main thread. The real C boundary must
	// refuse before NSApp access, without opening or activating a host app.
	if status := activateRecoverySuccessor(0, "/missing", recoverySuccessorBirth{}); status != recoveryFocusRefused {
		t.Fatalf("status=%d", status)
	}
}
func TestRecoverySuccessorStillRequiresFocusAndMatchingNoncePixels(t *testing.T) {
	scripts, phases, ready := make(chan string, 4), make(chan RecoveryDiagnostic, 8), make(chan struct{}, 1)
	window := &directoryRecordingWindow{}
	host := &recoveryTestHost{window: window, windowEvents: make(map[events.WindowEventType]func()), appEvents: make(map[events.ApplicationEventType]func()), probe: func(script string) { scripts <- script }}
	controller := startRecoveryForHost(host, Options{URL: "http://127.0.0.1:1234", Recovery: &RecoveryOptions{
		SessionID: "session", Ready: func(context.Context) error { ready <- struct{}{}; return nil },
		Diagnostic: func(event RecoveryDiagnostic) { phases <- event },
	}})
	defer controller.shutdown()
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	ticks, pixels := make(chan time.Time), make(chan []byte, 2)
	controller.now = func() time.Time { return time.Unix(0, clock.Load()) }
	controller.ticks = ticks
	controller.capture = func(ctx context.Context) ([]byte, error) {
		select {
		case image := <-pixels:
			return image, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	advance := func(seconds int) {
		t.Helper()
		for i := 0; i < seconds; i++ {
			select {
			case ticks <- time.Unix(0, clock.Add(int64(time.Second))):
			case <-time.After(time.Second):
				t.Fatal("recovery monitor did not consume its tick")
			}
		}
	}
	phase := func(want string) {
		t.Helper()
		select {
		case event := <-phases:
			if event.Phase != want {
				t.Fatalf("phase=%s want=%s", event.Phase, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing phase %s", want)
		}
	}
	host.appEvents[events.Common.ApplicationStarted]()
	advance(1)
	advance(1)
	if len(scripts) != 0 || len(ready) != 0 {
		t.Fatal("an inactive successor bypassed the real focus/pixel gates")
	}
	host.windowEvents[events.Common.WindowFocus]()
	phase("resumed")
	acknowledge := func(valid bool) {
		t.Helper()
		var script string
		select {
		case script = <-scripts:
		case <-time.After(time.Second):
			t.Fatal("focused successor did not issue a real paint challenge")
		}
		challenge := regexp.MustCompile(`"challenge":"([a-f0-9]+)"`).FindStringSubmatch(script)[1]
		data, _ := hex.DecodeString(challenge)
		first := color.NRGBA{R: 40 + data[0]%64, G: 40 + data[1]%64, B: 40 + data[2]%64, A: 255}
		second := color.NRGBA{R: 255 - first.R, G: 255 - first.G, B: 255 - first.B, A: 255}
		image := probePixels(challenge, first, second, 0, 0, false)
		if !valid {
			image = nil
		}
		pixels <- image
		body := fmt.Sprintf(`{"type":"zwai:recovery","session":"session","generation":0,"challenge":%q}`, challenge)
		controller.raw(window, body, &application.OriginInfo{Origin: controller.origin, IsMainFrame: true})
	}
	advance(1)
	acknowledge(false)
	phase("probe_failed")
	if len(ready) != 0 {
		t.Fatal("focus and a callback accepted an empty snapshot as ready")
	}
	advance(5)
	acknowledge(true)
	phase("ready")
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("matching nonce pixels did not reach the actual successor handshake")
	}
}
