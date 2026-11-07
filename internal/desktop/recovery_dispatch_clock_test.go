package desktop

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRecoveryRetryTimeoutStartsAtActualProbeDispatch(t *testing.T) {
	for _, lag := range []time.Duration{time.Second, 3 * time.Second} {
		t.Run(lag.String(), func(t *testing.T) {
			var calls atomic.Int64
			dispatched := make(chan time.Time, 8)
			h := newRecoveryHarness(t, 0, func(h *recoveryHarness) {
				h.responsive.Store(false)
				probe := h.c.probe
				h.c.probe = func(script string) {
					probe(script)
					dispatched <- h.c.now()
				}
				h.c.opts.Relaunch = func(context.Context, RecoveryAttempt) error {
					calls.Add(1)
					return context.DeadlineExceeded
				}
			})
			base := h.c.now()
			deliver := func(tick, clock time.Time) {
				t.Helper()
				h.clock.Store(clock.UnixNano())
				select {
				case h.ticks <- tick:
				case <-h.done:
					t.Fatal("recovery owner stopped before the controlled tick")
				case <-time.After(time.Second):
					t.Fatal("recovery owner did not receive the controlled tick")
				}
			}
			probeAt := func(want time.Time) {
				t.Helper()
				select {
				case got := <-dispatched:
					if !got.Equal(want) {
						t.Fatalf("probe dispatched at %s, want %s", got, want)
					}
				case <-time.After(time.Second):
					t.Fatal("controlled tick did not dispatch a probe")
				}
			}

			// Keep every delivery gap at most the health timeout, so this case
			// cannot take the long-clock-gap activity-reset path instead.
			for _, seconds := range []int{1, 21} {
				at := base.Add(time.Duration(seconds) * time.Second)
				deliver(at, at)
				probeAt(at)
			}
			reloadAt := base.Add(recoveryStartupGrace)
			deliver(reloadAt, reloadAt)
			h.action(t, "reload")
			afterReload := reloadAt.Add(time.Second)
			deliver(afterReload, afterReload)
			probeAt(afterReload)
			firstReplacement := reloadAt.Add(recoveryReloadTimeout)
			deliver(firstReplacement, firstReplacement)
			failed := h.phase(t, "replacement_failed")
			if got := calls.Load(); got != 1 {
				t.Fatalf("first replacement calls=%d, want 1", got)
			}

			// This queued tick predates actual dispatch. The offset is explicit,
			// rather than relying on advance's sender/receiver scheduling race.
			retryTick := failed.At.Add(recoveryRetryDelay)
			actualDispatch := retryTick.Add(lag)
			deliver(retryTick, actualDispatch)
			probeAt(actualDispatch)
			oldDeadline := retryTick.Add(recoveryHealthTimeout)
			actualDeadline := actualDispatch.Add(recoveryHealthTimeout)
			for _, at := range []time.Time{oldDeadline, actualDeadline.Add(-time.Nanosecond)} {
				deliver(at, at)
				// A same-value delivery is a completion barrier for the preceding
				// tick only. It advances neither deadline nor fake clock.
				deliver(at, at)
				if got := calls.Load(); got != 1 {
					t.Fatalf("replacement retried before the dispatched probe's deadline: calls=%d age=%s lag=%s",
						got, at.Sub(actualDispatch), lag)
				}
			}
			deliver(actualDeadline, actualDeadline)
			replacing := h.phase(t, "replacing")
			if replacing.Attempt != 2 || replacing.Reason != "callback_missing" ||
				replacing.At.Sub(actualDispatch) < recoveryHealthTimeout ||
				replacing.DurationMS < recoveryHealthTimeout.Milliseconds() {
				t.Fatalf("retry lost its actual dispatch-time boundary: %+v", replacing)
			}
			h.phase(t, "replacement_failed")
			h.phase(t, "exhausted")
			h.action(t, "error")
			if got := calls.Load(); got != 2 {
				t.Fatalf("replacement calls=%d, want exactly 2", got)
			}
		})
	}
}
