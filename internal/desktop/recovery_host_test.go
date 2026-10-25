package desktop

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type recoveryTestHost struct {
	ctx          context.Context
	window       application.Window
	probe        func(string)
	activity     recoveryWindowActivity
	queries      atomic.Int64
	shutdown     func()
	windowEvents map[events.WindowEventType]func()
	appEvents    map[events.ApplicationEventType]func()
}

func (h *recoveryTestHost) Context() context.Context {
	if h.ctx != nil {
		return h.ctx
	}
	return context.Background()
}
func (h *recoveryTestHost) Activity() recoveryWindowActivity {
	h.queries.Add(1)
	return h.activity
}
func (h *recoveryTestHost) Window() application.Window { return h.window }
func (h *recoveryTestHost) OnShutdown(fn func())       { h.shutdown = fn }
func (h *recoveryTestHost) OnWindowEvent(event events.WindowEventType, fn func()) {
	h.windowEvents[event] = fn
}
func (h *recoveryTestHost) OnApplicationEvent(event events.ApplicationEventType, fn func()) {
	h.appEvents[event] = fn
}
func (h *recoveryTestHost) Probe(script string) {
	if h.probe != nil {
		h.probe(script)
	}
}
func (*recoveryTestHost) Snapshot(context.Context) ([]byte, error) { return nil, nil }
func (*recoveryTestHost) Reload(string)                            {}
func (*recoveryTestHost) Quit()                                    {}
func (*recoveryTestHost) ShowError()                               {}

type recoveryNavigationWindow struct {
	application.Window
	urls    []string
	reloads int
}

func (w *recoveryNavigationWindow) SetURL(url string) application.Window {
	w.urls = append(w.urls, url)
	return w
}
func (w *recoveryNavigationWindow) Reload() { w.reloads++ }

func TestRecoveryNavigatesOnceToThePreservedLocation(t *testing.T) {
	window := &recoveryNavigationWindow{}
	host := wailsRecoveryHost{window: window}
	url := "http://127.0.0.1:1234?shell=desktop&recovery_thread=th_selected"
	host.Reload(url)
	if len(window.urls) != 1 || window.urls[0] != url || window.reloads != 0 {
		t.Fatalf("recovery issued stale or duplicate navigation: %+v", window)
	}
}

func TestNativeRecoveryLifecycleWiringHasNoHostGUIRequirement(t *testing.T) {
	if !rendererRecoveryAvailable() {
		t.Skip("native renderer adapter unavailable")
	}
	for _, opts := range []Options{{}, {URL: "%", Recovery: &RecoveryOptions{}}, {URL: "https://example.invalid", Recovery: &RecoveryOptions{}}, {URL: "http:", Recovery: &RecoveryOptions{}}} {
		if startRecoveryForHost(nil, opts) != nil {
			t.Fatal("invalid native origin enabled recovery")
		}
	}
	host := &recoveryTestHost{window: &directoryRecordingWindow{}, windowEvents: make(map[events.WindowEventType]func()), appEvents: make(map[events.ApplicationEventType]func())}
	controller := startRecoveryForHost(host, Options{URL: "http://127.0.0.1:1234", Recovery: &RecoveryOptions{SessionID: "session"}})
	if controller == nil || len(host.windowEvents) != 4 || len(host.appEvents) != 3 || host.shutdown == nil {
		t.Fatal("native lifecycle hooks missing")
	}
	for _, fn := range host.windowEvents {
		fn()
	}
	for _, fn := range host.appEvents {
		fn()
	}
	host.shutdown()
}

func TestApplicationCancellationStopsRecoveryBeforeTheNativeShutdownHooks(t *testing.T) {
	if !rendererRecoveryAvailable() {
		t.Skip("native renderer adapter unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probed := make(chan struct{}, 1)
	host := &recoveryTestHost{ctx: ctx, window: &directoryRecordingWindow{}, windowEvents: make(map[events.WindowEventType]func()), appEvents: make(map[events.ApplicationEventType]func()), probe: func(string) { probed <- struct{}{} }}
	startRecoveryForHost(host, Options{URL: "http://127.0.0.1:1234", Recovery: &RecoveryOptions{SessionID: "session"}})
	defer host.shutdown()
	cancel()
	host.appEvents[events.Common.ApplicationStarted]()
	if host.queries.Load() != 0 {
		t.Fatal("late startup queried a cancelled native application")
	}
	select {
	case <-probed:
		t.Fatal("recovery kept running between app cancellation and native shutdown hooks")
	case <-time.After(1200 * time.Millisecond):
	}
}

func TestAWindowCannotBeProbedBeforeItsNativeApplicationStarts(t *testing.T) {
	if !rendererRecoveryAvailable() {
		t.Skip("native renderer adapter unavailable")
	}
	probed := make(chan struct{}, 1)
	host := &recoveryTestHost{window: &directoryRecordingWindow{}, windowEvents: make(map[events.WindowEventType]func()), appEvents: make(map[events.ApplicationEventType]func()), probe: func(string) { probed <- struct{}{} }}
	startRecoveryForHost(host, Options{URL: "http://127.0.0.1:1234", Recovery: &RecoveryOptions{SessionID: "session"}})
	defer host.shutdown()
	select {
	case <-probed:
		t.Fatal("watchdog probed a window before native startup and actual activity were known")
	case <-time.After(1200 * time.Millisecond):
	}
}

func TestNativeStartupSnapshotDoesNotDropAnAlreadyQueuedFirstFocus(t *testing.T) {
	if !rendererRecoveryAvailable() {
		t.Skip("native renderer adapter unavailable")
	}
	probed := make(chan struct{}, 1)
	phases := make(chan RecoveryDiagnostic, 8)
	host := &recoveryTestHost{window: &directoryRecordingWindow{}, windowEvents: make(map[events.WindowEventType]func()), appEvents: make(map[events.ApplicationEventType]func()), probe: func(string) { probed <- struct{}{} }}
	c := startRecoveryForHost(host, Options{URL: "http://127.0.0.1:1234", Recovery: &RecoveryOptions{SessionID: "session", Diagnostic: func(event RecoveryDiagnostic) { phases <- event }}})
	defer host.shutdown()
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	ticks := make(chan time.Time)
	c.now = func() time.Time { return time.Unix(0, clock.Load()) }
	c.ticks = ticks
	if host.queries.Load() != 0 {
		t.Fatal("activity queried before native ApplicationStarted")
	}
	host.windowEvents[events.Common.WindowFocus]()
	host.appEvents[events.Common.ApplicationStarted]()
	host.appEvents[events.Common.ApplicationStarted]()
	select {
	case event := <-phases:
		if event.Phase != "resumed" {
			t.Fatalf("queued initial focus was not applied: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("initial snapshot dropped the window's queued focus")
	}
	select {
	case ticks <- time.Unix(0, clock.Add(int64(time.Second))):
	case <-time.After(time.Second):
		t.Fatal("native startup did not release the monitor")
	}
	select {
	case <-probed:
	case <-time.After(time.Second):
		t.Fatal("initial snapshot dropped the window's first queued focus")
	}
	if host.queries.Load() != 1 {
		t.Fatal("native activity was not captured exactly once after startup")
	}
}
