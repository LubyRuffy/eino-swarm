package desktop

import (
	"context"
	"errors"
	"testing"
	"time"
)

// An idle renderer must explain which acknowledgement was missing before
// reload destroys that document. The policy reason alone loses this evidence.
func TestRecoveryReportsThePendingProbeBeforeReload(t *testing.T) {
	for _, pending := range []string{"callback_missing", "snapshot_pending"} {
		t.Run(pending, func(t *testing.T) {
			started := make(chan struct{}, 1)
			h := newRecoveryHarness(t, 0, func(h *recoveryHarness) {
				if pending == "callback_missing" {
					h.responsive.Store(false)
					return
				}
				h.c.capture = func(ctx context.Context) ([]byte, error) {
					started <- struct{}{}
					<-ctx.Done()
					return nil, ctx.Err()
				}
			})
			h.advance(1)
			if pending == "snapshot_pending" {
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("actual controller did not start the acknowledged snapshot")
				}
			}
			h.advance(30)
			failure := h.phase(t, "probe_failed")
			if failure.Reason != pending || failure.DurationMS <= 0 || failure.SessionID != "session" || failure.Generation != 2 {
				t.Fatalf("missing stage, age or lineage: %+v", failure)
			}
			reload := h.phase(t, "reloading")
			if reload.Reason != "renderer_unresponsive" || reload.At.Before(failure.At) {
				t.Fatalf("diagnosis changed policy reason or arrived after reload: failure=%+v reload=%+v", failure, reload)
			}
			h.action(t, "reload")
		})
	}
}

func TestSnapshotFailureKeepsItsCategoryWithoutLoggingNativeErrorContent(t *testing.T) {
	for _, category := range []string{"snapshot_error", "snapshot_mismatch"} {
		t.Run(category, func(t *testing.T) {
			h := newRecoveryHarness(t, 0, func(h *recoveryHarness) {
				h.c.capture = func(context.Context) ([]byte, error) {
					if category == "snapshot_error" {
						return nil, errors.New("private native error content")
					}
					return nil, nil
				}
			})
			h.advance(1)
			if event := h.phase(t, "probe_failed"); event.Reason != category {
				t.Fatalf("snapshot failure was conflated or leaked content: %+v", event)
			}
		})
	}
}

func TestRecoveryPendingSuccessorReportsItsProbeBeforeTheParentDeadline(t *testing.T) {
	for _, boundary := range []string{"callback_missing", "location_pending", "snapshot_pending"} {
		t.Run(boundary, func(t *testing.T) {
			started := make(chan struct{}, 1)
			h := newRecoveryHarness(t, 1, func(h *recoveryHarness) {
				h.c.opts.Ready = func(context.Context) error { h.actions <- "commit"; return nil }
				h.c.opts.Relaunch = func(context.Context, RecoveryAttempt) error { h.actions <- "replace"; return nil }
				switch boundary {
				case "callback_missing":
					h.responsive.Store(false)
				case "location_pending":
					h.c.windowURL = recoveryLocation{ThreadID: "th_missing"}.windowURL(h.c.origin)
				case "snapshot_pending":
					h.c.capture = func(ctx context.Context) ([]byte, error) {
						started <- struct{}{}
						<-ctx.Done()
						return nil, ctx.Err()
					}
				}
			})
			h.advance(1)
			if boundary == "snapshot_pending" {
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("snapshot did not start")
				}
			}
			h.advance(int(recoveryProbeInterval / time.Second))
			event := h.phase(t, "probe_failed")
			if event.Reason != boundary || event.DurationMS <= 0 || event.SessionID != "session" || event.Generation != 2 || event.Attempt != 1 {
				t.Fatalf("missing successor probe boundary or lineage: %+v", event)
			}
			h.advance(35)
			select {
			case action := <-h.actions:
				t.Fatalf("uncommitted successor acted on the diagnostic: %s", action)
			default:
			}
		})
	}
}
