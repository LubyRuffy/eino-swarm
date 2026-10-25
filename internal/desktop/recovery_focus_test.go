//go:build darwin

package desktop

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func TestRecoverySuccessorFocusReportsNavigationAndMainDispatchOnce(t *testing.T) {
	var pending []func()
	calls := replaceRecoveryFocus(t, func(fn func()) { pending = append(pending, fn) })
	var logs bytes.Buffer
	host := &recoveryTestHost{window: &directoryRecordingWindow{}, windowEvents: make(map[events.WindowEventType]func()), appEvents: make(map[events.ApplicationEventType]func())}
	controller := startRecoveryForHost(host, Options{URL: "http://127.0.0.1:1234", Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Recovery: &RecoveryOptions{
		SessionID: "focus-session", Generation: 2, Attempts: 1, Ready: func(context.Context) error { return nil },
	}})
	defer controller.shutdown()
	host.windowEvents[events.Mac.WebViewDidFinishNavigation]()
	host.windowEvents[events.Mac.WebViewDidFinishNavigation]()
	if len(pending) != 1 || calls.Load() != 0 {
		t.Fatal("navigation did not queue exactly one main-thread request")
	}
	stages := func() []string {
		t.Helper()
		decoder := json.NewDecoder(bytes.NewReader(logs.Bytes()))
		var stages []string
		for decoder.More() {
			var row map[string]any
			if err := decoder.Decode(&row); err != nil {
				t.Fatal(err)
			}
			if row["msg"] != "desktop recovery focus" || row["session_id"] != "focus-session" || row["generation"] != float64(2) || row["attempt"] != float64(1) || row["shell_pid"] != float64(os.Getpid()) {
				t.Fatalf("focus observation lost its shell identity: %v", row)
			}
			stages = append(stages, row["stage"].(string))
		}
		return stages
	}
	if got := fmt.Sprint(stages()); got != "[navigation queued]" {
		t.Fatalf("focus enqueue stages=%s", got)
	}
	pending[0]()
	if got := fmt.Sprint(stages()); got != "[navigation queued main native_returned]" || calls.Load() != 1 {
		t.Fatalf("focus execution stages=%s calls=%d", got, calls.Load())
	}
}

func replaceRecoveryFocus(t *testing.T, dispatch func(func())) *atomic.Int64 {
	t.Helper()
	previousDispatch, previousNative := recoveryFocusOnMain, recoveryFocusNative
	calls := new(atomic.Int64)
	recoveryFocusOnMain = dispatch
	recoveryFocusNative = func(application.Window) { calls.Add(1) }
	t.Cleanup(func() { recoveryFocusOnMain, recoveryFocusNative = previousDispatch, previousNative })
	return calls
}

func TestRecoverySuccessorWaitsForNavigationBeforeRequestingFocus(t *testing.T) {
	calls := replaceRecoveryFocus(t, func(fn func()) { fn() })
	scripts, phases, ready := make(chan string, 4), make(chan RecoveryDiagnostic, 8), make(chan struct{}, 1)
	window := &directoryRecordingWindow{}
	host := &recoveryTestHost{window: window, windowEvents: make(map[events.WindowEventType]func()), appEvents: make(map[events.ApplicationEventType]func()), probe: func(script string) { scripts <- script }}
	controller := startRecoveryForHost(host, Options{URL: "http://127.0.0.1:1234", Recovery: &RecoveryOptions{
		SessionID: "session", Ready: func(context.Context) error { ready <- struct{}{}; return nil },
		Diagnostic: func(event RecoveryDiagnostic) { phases <- event },
	}})
	defer controller.shutdown()
	if host.windowEvents[events.Mac.WebViewDidFinishNavigation] == nil {
		t.Fatal("an uncommitted successor has no post-navigation focus request while its parent owns the foreground")
	}
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
	if calls.Load() != 0 {
		t.Fatal("application startup requested focus before navigation")
	}
	host.windowEvents[events.Mac.WebViewDidFinishNavigation]()
	host.windowEvents[events.Mac.WebViewDidFinishNavigation]()
	advance(1)
	if calls.Load() != 1 || len(scripts) != 0 || len(ready) != 0 {
		t.Fatal("focus was repeated or the request itself bypassed the real focus/pixel gates")
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
	host.windowEvents[events.Mac.WebViewDidFinishNavigation]()
	if calls.Load() != 1 {
		t.Fatal("committed successor requested another focus")
	}
}

func TestOrdinaryShellNeverRegistersSuccessorFocus(t *testing.T) {
	host := &recoveryTestHost{window: &directoryRecordingWindow{}, windowEvents: make(map[events.WindowEventType]func()), appEvents: make(map[events.ApplicationEventType]func())}
	controller := startRecoveryForHost(host, Options{URL: "http://127.0.0.1:1234", Recovery: &RecoveryOptions{}})
	defer controller.shutdown()
	if host.windowEvents[events.Mac.WebViewDidFinishNavigation] != nil {
		t.Fatal("an ordinary background shell gained an automatic focus request")
	}
}

func TestQueuedSuccessorFocusRechecksCancellationParentAndReadyOnMainThread(t *testing.T) {
	for _, boundary := range []string{"context", "parent", "ready"} {
		t.Run(boundary, func(t *testing.T) {
			var pending []func()
			var logs bytes.Buffer
			calls := replaceRecoveryFocus(t, func(fn func()) { pending = append(pending, fn) })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			parentDone := make(chan struct{})
			expected := errors.New("original ready result")
			host := &recoveryTestHost{ctx: ctx, window: &directoryRecordingWindow{}, windowEvents: make(map[events.WindowEventType]func()), appEvents: make(map[events.ApplicationEventType]func())}
			controller := startRecoveryForHost(host, Options{URL: "http://127.0.0.1:1234", Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Recovery: &RecoveryOptions{
				Ready: func(context.Context) error { return expected }, ParentDone: parentDone,
			}})
			defer controller.shutdown()
			host.windowEvents[events.Mac.WebViewDidFinishNavigation]()
			if len(pending) != 1 || calls.Load() != 0 {
				t.Fatal("focus bypassed main-thread dispatch")
			}
			switch boundary {
			case "context":
				cancel()
			case "parent":
				close(parentDone)
			case "ready":
				if err := controller.opts.Ready(ctx); !errors.Is(err, expected) {
					t.Fatalf("focus lifetime changed the original ready result: %v", err)
				}
			}
			pending[0]()
			host.windowEvents[events.Mac.WebViewDidFinishNavigation]()
			if calls.Load() != 0 || len(pending) != 1 {
				t.Fatal("a stale focus request survived its boundary or retried after refusal")
			}
			stage := "context_cancelled"
			if boundary == "parent" {
				stage = "parent_closed"
			}
			if !strings.Contains(logs.String(), `"stage":"`+stage+`"`) || boundary == "ready" && !strings.Contains(logs.String(), `"stage":"ready_cancelled"`) {
				t.Fatalf("focus cancellation boundary was not observable: %s", &logs)
			}
		})
	}
}

type recoveryFocusNilNativeWindow struct{ application.Window }

func (recoveryFocusNilNativeWindow) NativeWindow() unsafe.Pointer { return nil }

func TestRecoveryFocusWrapperRefusesNilWindowAndHasNoHostGUI(t *testing.T) {
	calls := replaceRecoveryFocus(t, func(fn func()) { fn() })
	var stages []string
	requestRecoverySuccessorFocus(context.Background(), nil, nil, func(stage string) { stages = append(stages, stage) })
	if calls.Load() != 0 {
		t.Fatal("nil window reached the native focus adapter")
	}
	if fmt.Sprint(stages) != "[queued main window_missing]" {
		t.Fatalf("nil native window refusal was not observable: %v", stages)
	}
	// The real two-line cgo entry receives a nil pointer; the SDK guards are
	// exercised separately with fake AppKit objects, never the host NSApplication.
	focusRecoveryWindow(recoveryFocusNilNativeWindow{})
}
