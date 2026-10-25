package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type recoveryHarness struct {
	c          *recoveryController
	clock      atomic.Int64
	responsive atomic.Bool
	ticks      chan time.Time
	phases     chan RecoveryDiagnostic
	actions    chan string
	done       chan struct{}
	cancel     context.CancelFunc
	mu         sync.Mutex
	pixels     []byte
}

func newRecoveryHarness(t *testing.T, attempts int, configure ...func(*recoveryHarness)) *recoveryHarness {
	t.Helper()
	h := &recoveryHarness{ticks: make(chan time.Time), phases: make(chan RecoveryDiagnostic, 256), actions: make(chan string, 32), done: make(chan struct{})}
	h.clock.Store(time.Now().UnixNano())
	h.responsive.Store(true)
	initialized := make(chan struct{})
	var initializedOnce sync.Once
	window := &directoryRecordingWindow{}
	h.c = &recoveryController{window: window, initialActivity: recoveryWindowActivity{focused: true}, origin: "http://127.0.0.1:1234", in: make(chan recoveryInput, 32), ticks: h.ticks, now: func() time.Time {
		now := time.Unix(0, h.clock.Load())
		initializedOnce.Do(func() { close(initialized) })
		return now
	}, opts: RecoveryOptions{SessionID: "session", Generation: 2, Attempts: attempts, Diagnostic: func(event RecoveryDiagnostic) { h.phases <- event }}, reload: func(string) { h.actions <- "reload" }, quit: func() { h.actions <- "quit" }, showError: func() { h.actions <- "error" }}
	h.c.probe = func(script string) {
		if !h.responsive.Load() {
			return
		}
		match := regexp.MustCompile(`"challenge":"([a-f0-9]+)"`).FindStringSubmatch(script)
		colours := regexp.MustCompile(`rgb\((\d+),(\d+),(\d+)\)`).FindAllStringSubmatch(script, -1)
		if len(match) != 2 || len(colours) != 2 {
			t.Errorf("invalid render challenge: %s", script)
			return
		}
		inks := [2]color.NRGBA{{A: 255}, {A: 255}}
		for j := range inks {
			r, _ := strconv.Atoi(colours[j][1])
			g, _ := strconv.Atoi(colours[j][2])
			b, _ := strconv.Atoi(colours[j][3])
			inks[j].R, inks[j].G, inks[j].B = byte(r), byte(g), byte(b)
		}
		pixels := probePixels(match[1], inks[0], inks[1], 0, 0, false)
		h.mu.Lock()
		h.pixels = pixels
		h.mu.Unlock()
		body, _ := json.Marshal(map[string]any{"type": "zwai:recovery", "session": "session", "generation": 2, "challenge": match[1], "thread_id": "th_real", "turn_id": "tn_real", "project_id": "pr_real", "draft_key": "thread:th_real"})
		h.c.raw(window, string(body), &application.OriginInfo{Origin: h.c.origin, IsMainFrame: true})
	}
	h.c.capture = func(context.Context) ([]byte, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		return append([]byte(nil), h.pixels...), nil
	}
	for _, setup := range configure {
		setup(h)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	h.c.cancel, h.c.done = cancel, h.done
	t.Cleanup(func() {
		cancel()
		select {
		case <-h.done:
		case <-time.After(time.Second):
			t.Error("recovery loop did not stop")
		}
	})
	go func() { defer close(h.done); h.c.run(ctx) }()
	<-initialized
	return h
}

func (h *recoveryHarness) advance(seconds int) {
	for i := 0; i < seconds; i++ {
		now := time.Unix(0, h.clock.Add(int64(time.Second)))
		select {
		case h.ticks <- now:
		case <-h.done:
			return
		}
	}
}
func (h *recoveryHarness) phase(t *testing.T, want string) RecoveryDiagnostic {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-h.phases:
			if event.Phase == want {
				return event
			}
		case <-timer.C:
			t.Fatalf("missing recovery phase %s", want)
			return RecoveryDiagnostic{}
		}
	}
}
func (h *recoveryHarness) action(t *testing.T, want string) {
	t.Helper()
	select {
	case got := <-h.actions:
		if got != want {
			t.Fatalf("action=%s want=%s", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("missing action %s", want)
	}
}

func TestRecoveryLoopVerifiesPixelsBeforeReadyAndPreservesActualContext(t *testing.T) {
	h := newRecoveryHarness(t, 0)
	h.advance(1)
	event := h.phase(t, "ready")
	if event.ThreadID != "th_real" || event.TurnID != "tn_real" || event.SessionID != "session" || event.Generation != 2 {
		t.Fatalf("wrong ready attribution: %+v", event)
	}
	h.responsive.Store(false)
	var attempt RecoveryAttempt
	h.c.opts.Relaunch = func(_ context.Context, next RecoveryAttempt) error { attempt = next; return nil }
	h.advance(40)
	h.action(t, "reload")
	h.action(t, "quit")
	<-h.done
	if attempt.Generation != 3 || attempt.Attempts != 1 || attempt.ThreadID != "th_real" || attempt.ProjectID != "pr_real" || attempt.DraftKey != "thread:th_real" {
		t.Fatalf("recovery invented lineage or lost selected draft: %+v", attempt)
	}
}

func TestPendingSuccessorNeverStartsASecondRecoveryOwner(t *testing.T) {
	h := newRecoveryHarness(t, 1)
	h.responsive.Store(false)
	h.c.opts.Ready = func(context.Context) error { return nil }
	h.c.opts.Relaunch = func(context.Context, RecoveryAttempt) error { h.actions <- "replace"; return nil }
	h.advance(65)
	select {
	case action := <-h.actions:
		t.Fatalf("uncommitted successor took ownership: %s", action)
	default:
	}
}

func TestRecoveryLoopRefusesEmptySnapshotsAndShowsOneBoundedNativeError(t *testing.T) {
	h := newRecoveryHarness(t, 1)
	h.c.capture = func(context.Context) ([]byte, error) { return nil, errors.New("empty image") }
	var calls atomic.Int64
	h.c.opts.Relaunch = func(context.Context, RecoveryAttempt) error {
		calls.Add(1)
		return errors.New("successor did not become ready")
	}
	h.advance(70)
	h.action(t, "reload")
	h.action(t, "error")
	if calls.Load() != 1 {
		t.Fatalf("exhausted lineage launched %d successors", calls.Load())
	}
	h.phase(t, "exhausted")
	select {
	case event := <-h.phases:
		if event.Phase == "ready" {
			t.Fatal("empty snapshot marked renderer ready")
		}
	default:
	}
}

func TestRecoveryLoopPausesBackgroundAndCancellationReleasesReplacement(t *testing.T) {
	h := newRecoveryHarness(t, 0)
	h.advance(1)
	h.phase(t, "ready")
	h.responsive.Store(false)
	h.c.setActivity(recoveryFocused, false)
	h.phase(t, "suspended")
	h.advance(3600)
	select {
	case action := <-h.actions:
		t.Fatalf("background app restarted: %s", action)
	default:
	}
	h.c.setActivity(recoveryFocused, true)
	h.phase(t, "resumed")
	h.c.opts.Relaunch = func(ctx context.Context, _ RecoveryAttempt) error {
		h.actions <- "replace"
		<-ctx.Done()
		return ctx.Err()
	}
	h.advance(35)
	h.action(t, "reload")
	h.action(t, "replace")
	h.cancel()
	select {
	case <-h.done:
	case <-time.After(time.Second):
		t.Fatal("cancel did not release replacement")
	}
}

func TestRecoveryRawHandlerDropsOversizedAndWrongWindowWithoutBlocking(t *testing.T) {
	c := &recoveryController{window: &directoryRecordingWindow{}, in: make(chan recoveryInput, 1)}
	c.raw(&directoryRecordingWindow{}, "{}", nil)
	c.raw(c.window, fmt.Sprintf("%*s", recoveryMessageLimit+1, ""), nil)
	if len(c.in) != 0 {
		t.Fatal("untrusted input entered recovery queue")
	}
	origin := &application.OriginInfo{Origin: "first", IsMainFrame: true}
	c.raw(c.window, "{}", origin)
	c.setActivity(recoveryFocused, true)
	origin.Origin = "changed"
	c.raw(c.window, "{}", origin)
	if got := <-c.in; got.origin.Origin != "first" {
		t.Fatal("caller mutated queued origin")
	}
}

func TestRecoveryReadyHandshakeCanCommitOrAbortWithoutAnExtraWatchdog(t *testing.T) {
	for _, accept := range []bool{false, true} {
		t.Run(fmt.Sprint(accept), func(t *testing.T) {
			h := newRecoveryHarness(t, 1)
			called := make(chan struct{})
			h.c.opts.Ready = func(context.Context) error {
				close(called)
				if !accept {
					return errors.New("parent did not commit")
				}
				return nil
			}
			h.advance(1)
			select {
			case <-called:
			case <-time.After(time.Second):
				t.Fatal("verified renderer never attempted commit")
			}
			if !accept {
				h.action(t, "quit")
				<-h.done
			}
		})
	}
	aborted := make(chan struct{})
	h := newRecoveryHarness(t, 1, func(h *recoveryHarness) { h.c.opts.ParentDone = aborted })
	close(aborted)
	h.advance(1)
	h.action(t, "quit")
}

func TestRecoveryCleanupFailureAndMissingLauncherNeverCreateUnboundedSuccessors(t *testing.T) {
	for _, cleanupFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(cleanupFailure), func(t *testing.T) {
			h := newRecoveryHarness(t, 0)
			h.responsive.Store(false)
			if cleanupFailure {
				h.c.opts.Relaunch = func(context.Context, RecoveryAttempt) error { return ErrRecoveryCleanup }
			}
			h.advance(80)
			h.action(t, "reload")
			h.action(t, "error")
			h.phase(t, "exhausted")
		})
	}
}

func TestRecoveryDiscardsStaleProbeAndTreatsSuspendedClockAsANewGrace(t *testing.T) {
	h := newRecoveryHarness(t, 0)
	h.c.in <- recoveryInput{probe: "old", pixels: make([]byte, recoverySnapshotSide*recoverySnapshotSide*4)}
	h.c.in <- recoveryInput{message: "{}", origin: nil}
	h.advance(1)
	h.phase(t, "ready")
	h.responsive.Store(false)
	now := time.Unix(0, h.clock.Add(int64(time.Hour)))
	h.ticks <- now
	h.advance(10)
	select {
	case action := <-h.actions:
		t.Fatalf("clock suspension triggered %s", action)
	default:
	}
}

func TestRecoveryRealTickerStopsWithoutAWindow(t *testing.T) {
	h := newRecoveryHarness(t, 0)
	h.cancel()
	<-h.done
	h.c.ticks = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.c.run(ctx)
	if !rendererRecoveryAvailable() {
		t.Skip("renderer recovery has no native adapter on this platform")
	}
}

func TestClosingTheSuccessorClosesTheOldShellInsteadOfReopeningIt(t *testing.T) {
	h := newRecoveryHarness(t, 0)
	h.responsive.Store(false)
	var calls atomic.Int64
	h.c.opts.Relaunch = func(context.Context, RecoveryAttempt) error { calls.Add(1); return ErrRecoveryCancelled }
	h.advance(60)
	h.action(t, "reload")
	h.action(t, "quit")
	<-h.done
	if calls.Load() != 1 {
		t.Fatal("user quit restarted another successor")
	}
}

func TestRendererThatMountsAfterTheFirstProbeIsReadyOnTheNextProbeInterval(t *testing.T) {
	h := newRecoveryHarness(t, 0)
	h.responsive.Store(false)
	firstProbe := make(chan struct{})
	originalProbe := h.c.probe
	var once sync.Once
	h.c.probe = func(script string) { originalProbe(script); once.Do(func() { close(firstProbe) }) }
	var snapshots atomic.Int64
	capture := h.c.capture
	h.c.capture = func(ctx context.Context) ([]byte, error) { snapshots.Add(1); return capture(ctx) }
	h.advance(1)
	<-firstProbe
	h.advance(1)
	h.responsive.Store(true)
	h.advance(4)
	h.phase(t, "ready")
	h.advance(4)
	if snapshots.Load() != 1 {
		t.Fatalf("successful snapshot replayed within one probe interval: %d", snapshots.Load())
	}
}

func TestStartupProbeRetryKeepsTheOneRealSnapshotInFlight(t *testing.T) {
	h := newRecoveryHarness(t, 0)
	var probes atomic.Int64
	probe := h.c.probe
	h.c.probe = func(script string) { probes.Add(1); probe(script) }
	started := make(chan struct{})
	release := make(chan struct{})
	capture := h.c.capture
	h.c.capture = func(ctx context.Context) ([]byte, error) {
		close(started)
		select {
		case <-release:
			return capture(ctx)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	h.advance(1)
	<-started
	h.advance(10)
	if got := probes.Load(); got != 1 {
		t.Fatalf("in-flight snapshot was replaced by %d probes", got)
	}
	close(release)
	h.phase(t, "ready")
}

func TestShutdownWaitsForTheCurrentReadyOrRelaunchOwnerBeforeCleanup(t *testing.T) {
	for _, stage := range []string{"ready", "commit"} {
		t.Run(stage, func(t *testing.T) {
			h := newRecoveryHarness(t, 0)
			entered := make(chan struct{})
			waitForCancellation := func(ctx context.Context) error {
				close(entered)
				<-ctx.Done()
				return ctx.Err()
			}
			if stage == "ready" {
				h.c.opts.Ready = waitForCancellation
			} else {
				h.responsive.Store(false)
				h.c.opts.Relaunch = func(ctx context.Context, _ RecoveryAttempt) error { return waitForCancellation(ctx) }
			}
			var stopped atomic.Int64
			h.c.opts.OnStopped = func() {
				select {
				case <-h.done:
				default:
					t.Error("cleanup ran while recovery still owned its handoff")
				}
				stopped.Add(1)
			}
			if stage == "ready" {
				h.advance(1)
			} else {
				go h.advance(50)
			}
			<-entered
			h.c.shutdown()
			h.c.shutdown()
			if stopped.Load() != 1 {
				t.Fatal("confirmed controller stop did not release resources exactly once")
			}
		})
	}
}

func TestShutdownWithoutAStoppedOwnerRetainsItsPrivateCopy(t *testing.T) {
	h := newRecoveryHarness(t, 0)
	entered := make(chan struct{})
	release := make(chan struct{})
	h.c.opts.Ready = func(context.Context) error { close(entered); <-release; return errors.New("uncooperative owner ended") }
	var stopped atomic.Bool
	h.c.opts.OnStopped = func() { stopped.Store(true) }
	h.advance(1)
	<-entered
	h.c.shutdown()
	if stopped.Load() {
		t.Fatal("timed-out shutdown claimed that the recovery owner stopped")
	}
	close(release)
	<-h.done
	h.c.shutdown()
	if stopped.Load() {
		t.Fatal("an unverified shutdown later bypassed retained-copy ownership")
	}
}

func TestSleepingBackgroundOrMinimizedWindowDoesNotSpendRecoveryBudgetOnWake(t *testing.T) {
	for _, hidden := range []string{"background", "minimized"} {
		t.Run(hidden, func(t *testing.T) {
			h := newRecoveryHarness(t, 1)
			host := &recoveryTestHost{windowEvents: make(map[events.WindowEventType]func()), appEvents: make(map[events.ApplicationEventType]func())}
			bindRecoveryActivity(host, h.c)
			h.advance(1)
			h.phase(t, "ready")
			h.responsive.Store(false)
			if hidden == "background" {
				host.windowEvents[events.Common.WindowLostFocus]()
			} else {
				host.windowEvents[events.Mac.WindowDidMiniaturize]()
			}
			h.phase(t, "suspended")
			host.appEvents[events.Common.SystemWillSleep]()
			host.appEvents[events.Common.SystemDidWake]()
			h.advance(80)
			select {
			case action := <-h.actions:
				t.Fatalf("wake spent hidden-window recovery budget: %s", action)
			default:
			}
			if hidden == "background" {
				host.windowEvents[events.Common.WindowFocus]()
			} else {
				host.windowEvents[events.Mac.WindowDidDeminiaturize]()
			}
			h.phase(t, "resumed")
			h.advance(10)
			select {
			case action := <-h.actions:
				t.Fatalf("foreground return skipped its health grace: %s", action)
			default:
			}
		})
	}
}

func TestFocusAndUnminimizeDoNotOverrideTheOtherHiddenState(t *testing.T) {
	for _, first := range []recoveryActivity{recoveryFocused, recoveryMinimized} {
		t.Run(fmt.Sprint(first), func(t *testing.T) {
			h := newRecoveryHarness(t, 1)
			h.advance(1)
			h.phase(t, "ready")
			h.responsive.Store(false)
			h.c.setActivity(recoveryFocused, false)
			h.phase(t, "suspended")
			h.c.setActivity(recoveryMinimized, true)
			second := recoveryMinimized
			if first == recoveryFocused {
				h.c.setActivity(recoveryFocused, true)
			} else {
				h.c.setActivity(recoveryMinimized, false)
				second = recoveryFocused
			}
			h.advance(80)
			select {
			case action := <-h.actions:
				t.Fatalf("one visibility event overrode the other hidden state: %s", action)
			default:
			}
			h.c.setActivity(second, second == recoveryFocused)
			h.phase(t, "resumed")
			h.advance(10)
			select {
			case action := <-h.actions:
				t.Fatalf("full foreground restoration skipped its grace: %s", action)
			default:
			}
		})
	}
}

func TestCurrentJavaScriptAckWithPreviousNoncePixelsDoesNotMarkTheRendererHealthy(t *testing.T) {
	h := newRecoveryHarness(t, 0)
	capture := h.c.capture
	h.c.capture = func(ctx context.Context) ([]byte, error) {
		pixels, err := capture(ctx)
		// Flipping one whole cell is the preceding nonce with the current palette.
		for y := 0; y < 2; y++ {
			for x := 0; x < 2; x++ {
				i := (y*recoverySnapshotSide + x) * 4
				for channel := 0; channel < 3; channel++ {
					pixels[i+channel] = 255 - pixels[i+channel]
				}
			}
		}
		return pixels, err
	}
	h.advance(1)
	h.phase(t, "probe_failed")
	h.advance(30)
	h.phase(t, "reloading")
	h.action(t, "reload")
}

func TestAnInitiallyBackgroundOrMinimizedWindowNeedsNoLostFocusEventToStaySuspended(t *testing.T) {
	for _, activity := range []recoveryWindowActivity{{focused: false}, {focused: true, minimized: true}} {
		t.Run(fmt.Sprint(activity), func(t *testing.T) {
			h := newRecoveryHarness(t, 1, func(h *recoveryHarness) {
				h.c.initialActivity = activity
				h.responsive.Store(false)
			})
			h.advance(80)
			select {
			case action := <-h.actions:
				t.Fatalf("initially hidden window spent recovery budget without ever losing focus: %s", action)
			default:
			}
			if activity.minimized {
				h.c.setActivity(recoveryMinimized, false)
			} else {
				h.c.setActivity(recoveryFocused, true)
			}
			h.phase(t, "resumed")
			h.advance(10)
			select {
			case action := <-h.actions:
				t.Fatalf("initial foreground entry skipped health grace: %s", action)
			default:
			}
		})
	}
}
