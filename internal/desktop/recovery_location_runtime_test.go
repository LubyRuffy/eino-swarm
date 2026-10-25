package desktop

import (
	"context"
	"encoding/json"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// A selection acknowledged before the renderer stalls must not wait for the
// five-second paint probe before it can be used to restore the window.
func TestRecoveryUsesLatestLocationBeforeTheNextPaint(t *testing.T) {
	var attempt RecoveryAttempt
	h := newRecoveryHarness(t, 0, func(h *recoveryHarness) {
		h.c.opts.Relaunch = func(_ context.Context, next RecoveryAttempt) error {
			attempt = next
			return nil
		}
	})
	h.advance(1)
	h.phase(t, "ready")
	h.responsive.Store(false)
	body, err := json.Marshal(map[string]any{
		"type": "zwai:recovery-location", "session": "session", "generation": 2, "document": 0,
		"thread_id": "th_selected", "turn_id": "tn_selected",
		"project_id": "pr_selected", "draft_key": "thread:th_selected",
	})
	if err != nil {
		t.Fatal(err)
	}
	h.c.raw(h.c.window, string(body), &application.OriginInfo{Origin: h.c.origin, IsMainFrame: true})
	flushRecoveryLocations(t, h)
	h.advance(40)
	h.action(t, "reload")
	h.action(t, "quit")
	<-h.done
	if attempt.ThreadID != "th_selected" || attempt.ProjectID != "pr_selected" || attempt.DraftKey != "thread:th_selected" {
		t.Fatalf("selection was lost before the next paint: %+v", attempt)
	}
}

func TestRecoveryBootstrapHomeCannotOverwriteTheRequestedScopeOrProveReady(t *testing.T) {
	want := recoveryLocation{ThreadID: "th_selected", ProjectID: "pr_selected", DraftKey: "thread:th_selected"}
	scripts := make(chan string, 10)
	var captures atomic.Int64
	var realProbe func(string)
	h := newRecoveryHarness(t, 0, func(h *recoveryHarness) {
		h.c.windowURL = want.windowURL(h.c.origin + "?shell=desktop&pid=42")
		realProbe = h.c.probe
		h.c.probe = func(script string) { scripts <- script }
		capture := h.c.capture
		h.c.capture = func(ctx context.Context) ([]byte, error) { captures.Add(1); return capture(ctx) }
	})
	h.advance(1)
	oldScript := <-scripts
	realProbe(oldScript) // Initial Home paint is not restoration of the requested scope.
	h.c.raw(h.c.window, recoveryLocationBody(t, recoveryLocation{DraftKey: "new:home"}), &application.OriginInfo{Origin: h.c.origin, IsMainFrame: true})
	h.c.raw(h.c.window, recoveryLocationBody(t, want), &application.OriginInfo{Origin: h.c.origin, IsMainFrame: true})
	flushRecoveryLocations(t, h)
	oldToken := regexp.MustCompile(`"challenge":"([a-f0-9]+)"`).FindStringSubmatch(oldScript)[1]
	h.c.in <- recoveryInput{probe: oldToken, pixels: h.pixels}
	// An acknowledged location is a queue barrier, not a health acknowledgement.
	h.c.raw(h.c.window, recoveryLocationBody(t, want), &application.OriginInfo{Origin: h.c.origin, IsMainFrame: true})
	flushRecoveryLocations(t, h)
	if captures.Load() != 0 {
		t.Fatal("bootstrap Home started a native acceptance capture")
	}
	select {
	case event := <-h.phases:
		t.Fatalf("location/old pixels marked the renderer ready: %+v", event)
	default:
	}
	h.advance(1)
	realProbe(<-scripts)
	if got := h.phase(t, "ready"); got.ThreadID != want.ThreadID {
		t.Fatalf("legacy paint attribution overwrote restored scope: %+v", got)
	}
	if captures.Load() != 1 {
		t.Fatal("fresh paint did not receive its own capture")
	}
	// Once bootstrap has matched, an intentional Home selection is valid.
	h.c.raw(h.c.window, recoveryLocationBody(t, recoveryLocation{DraftKey: "new:home"}), &application.OriginInfo{Origin: h.c.origin, IsMainFrame: true})
	flushRecoveryLocations(t, h)
}

func TestLocationNotificationsDoNotRenewHealthOrReloadDeadlines(t *testing.T) {
	var attempt RecoveryAttempt
	reloaded := make(chan string, 1)
	h := newRecoveryHarness(t, 0, func(h *recoveryHarness) {
		h.c.windowURL = h.c.origin + "?shell=desktop&pid=42"
		h.c.reload = func(raw string) { reloaded <- raw }
		h.c.opts.Relaunch = func(_ context.Context, next RecoveryAttempt) error { attempt = next; return nil }
	})
	h.advance(1)
	h.phase(t, "ready")
	h.responsive.Store(false)
	want := recoveryLocation{ThreadID: "th_new", DraftKey: "thread:th_new"}
	for i := 0; i < 35; i++ {
		body := recoveryLocationBody(t, want)
		if i >= 20 {
			body = strings.Replace(body, `"document":0`, `"document":1`, 1)
		}
		h.c.raw(h.c.window, body, &application.OriginInfo{Origin: h.c.origin, IsMainFrame: true})
		flushRecoveryLocations(t, h)
		h.advance(1)
	}
	h.action(t, "quit")
	<-h.done
	if got := recoveryLocationFromURL(<-reloaded); !got.sameScope(want) || attempt.ThreadID != want.ThreadID {
		t.Fatalf("reload/relaunch lost location: %+v %+v", got, attempt)
	}
}

func TestBackgroundLocationIsRetainedWithoutAHealthAcknowledgement(t *testing.T) {
	var attempt RecoveryAttempt
	h := newRecoveryHarness(t, 0, func(h *recoveryHarness) {
		h.c.opts.Relaunch = func(_ context.Context, next RecoveryAttempt) error { attempt = next; return nil }
	})
	h.advance(1)
	h.phase(t, "ready")
	h.c.setActivity(recoveryFocused, false)
	h.phase(t, "suspended")
	want := recoveryLocation{ThreadID: "th_background", DraftKey: "thread:th_background"}
	h.c.raw(h.c.window, recoveryLocationBody(t, want), &application.OriginInfo{Origin: h.c.origin, IsMainFrame: true})
	flushRecoveryLocations(t, h)
	h.advance(60)
	select {
	case action := <-h.actions:
		t.Fatalf("location activated the watchdog: %s", action)
	default:
	}
	h.responsive.Store(false)
	h.c.setActivity(recoveryFocused, true)
	h.phase(t, "resumed")
	h.advance(35)
	h.action(t, "reload")
	h.action(t, "quit")
	<-h.done
	if attempt.ThreadID != want.ThreadID {
		t.Fatalf("background selection did not survive resumed recovery: %+v", attempt)
	}
}

// Wait for queued inputs to be taken, then use an unbuffered tick as an actor
// barrier. No diagnostic phase or health acknowledgement is invented for tests.
func flushRecoveryLocations(t *testing.T, h *recoveryHarness) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for len(h.c.in) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("recovery actor did not consume its inputs")
		}
		runtime.Gosched()
	}
	select {
	case h.ticks <- time.Unix(0, h.clock.Load()):
	case <-h.done:
	case <-time.After(time.Second):
		t.Fatal("recovery actor did not finish its current input")
	}
}

func TestQueuedOldDocumentCannotAcknowledgeReloadedHome(t *testing.T) {
	want := recoveryLocation{ThreadID: "th_selected", ProjectID: "pr_selected", DraftKey: "thread:th_selected"}
	var attempt RecoveryAttempt
	h := newRecoveryHarness(t, 0, func(h *recoveryHarness) {
		h.c.opts.Relaunch = func(_ context.Context, next RecoveryAttempt) error { attempt = next; return nil }
		h.c.reload = func(string) {
			// These callbacks can already be queued when the navigation starts.
			h.c.raw(h.c.window, recoveryLocationBody(t, want), &application.OriginInfo{Origin: h.c.origin, IsMainFrame: true})
			home := recoveryLocationBody(t, recoveryLocation{DraftKey: "new:home"})
			h.c.raw(h.c.window, strings.Replace(home, `"document":0`, `"document":1`, 1), &application.OriginInfo{Origin: h.c.origin, IsMainFrame: true})
		}
	})
	h.advance(1)
	h.phase(t, "ready")
	h.responsive.Store(false)
	h.c.raw(h.c.window, recoveryLocationBody(t, want), &application.OriginInfo{Origin: h.c.origin, IsMainFrame: true})
	flushRecoveryLocations(t, h)
	h.advance(35)
	h.action(t, "quit")
	<-h.done
	if attempt.ThreadID != want.ThreadID || attempt.DraftKey != want.DraftKey {
		t.Fatalf("old document released the new bootstrap guard: %+v", attempt)
	}
}

func TestSelectionWhileSnapshotIsPendingStillControlsRecovery(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int64
	var attempt RecoveryAttempt
	h := newRecoveryHarness(t, 0, func(h *recoveryHarness) {
		capture := h.c.capture
		h.c.capture = func(ctx context.Context) ([]byte, error) {
			if calls.Add(1) == 2 {
				close(started)
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return capture(ctx)
		}
		h.c.opts.Relaunch = func(_ context.Context, next RecoveryAttempt) error { attempt = next; return nil }
	})
	h.advance(1)
	h.phase(t, "ready")
	h.advance(5)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("second native snapshot did not start")
	}
	h.responsive.Store(false)
	want := recoveryLocation{ThreadID: "th_during_capture", DraftKey: "thread:th_during_capture"}
	h.c.raw(h.c.window, recoveryLocationBody(t, want), &application.OriginInfo{Origin: h.c.origin, IsMainFrame: true})
	flushRecoveryLocations(t, h)
	h.advance(40)
	h.action(t, "reload")
	h.action(t, "quit")
	<-h.done
	if attempt.ThreadID != want.ThreadID || attempt.DraftKey != want.DraftKey || calls.Load() != 2 {
		t.Fatalf("pending pixels discarded location or faked health: %+v captures=%d", attempt, calls.Load())
	}
}
