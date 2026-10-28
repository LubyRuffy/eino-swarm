package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/timing"
	"github.com/LubyRuffy/eino-swarm/internal/timing/timingtest"
)

type clockAcquireResult struct {
	ticket *backoffTicket
	err    error
}

type clockWaitNotice struct {
	attempt int
	delay   time.Duration
}

func acquireWithClock(t *testing.T, ctx context.Context, gate *backoffGate) (<-chan clockAcquireResult, <-chan clockWaitNotice) {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	results, notices := make(chan clockAcquireResult, 1), make(chan clockWaitNotice, 4)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticket, err := gate.acquire(ctx, func(attempt int, delay time.Duration, _ uint64) {
			notices <- clockWaitNotice{attempt: attempt, delay: delay}
		})
		results <- clockAcquireResult{ticket: ticket, err: err}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("owned clock admission did not exit after cancellation")
		}
	})
	return results, notices
}

func awaitClockWait(t *testing.T, notices <-chan clockWaitNotice) clockWaitNotice {
	t.Helper()
	select {
	case notice := <-notices:
		return notice
	case <-time.After(time.Second):
		t.Fatal("admission did not register its actual timer and wait notice")
		return clockWaitNotice{}
	}
}

func awaitClockAcquire(t *testing.T, results <-chan clockAcquireResult) clockAcquireResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(time.Second):
		t.Fatal("admission did not react to its clock or cancellation")
		return clockAcquireResult{}
	}
}

func TestInstanceClockKeepsCooldownDeadlineAndCancellationInOneDomain(t *testing.T) {
	clock := timingtest.New(time.Now())
	gate := newBackoffGateWithClock(5*time.Second, 30*time.Second, clock)
	healthy, err := gate.acquire(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	healthy.finish(backoffFailure)
	results, notices := acquireWithClock(t, t.Context(), gate)
	if notice := awaitClockWait(t, notices); notice.attempt != 1 || notice.delay != 5*time.Second {
		t.Fatalf("initial cooldown changed: %+v", notice)
	}
	clock.Advance(4 * time.Second)
	select {
	case result := <-results:
		t.Fatalf("request escaped before its deadline: %+v", result)
	default:
	}
	clock.Advance(time.Second)
	probe := awaitClockAcquire(t, results)
	if probe.err != nil || probe.ticket == nil || !probe.ticket.probe {
		t.Fatalf("due request did not own the probe: %+v", probe)
	}
	probe.ticket.finish(backoffFailure)
	ctx, cancel := context.WithCancel(t.Context())
	results, notices = acquireWithClock(t, ctx, gate)
	if notice := awaitClockWait(t, notices); notice.attempt != 2 || notice.delay != 10*time.Second {
		t.Fatalf("retry no longer doubles its logical cooldown: %+v", notice)
	}
	before := clock.Now()
	cancel()
	if result := awaitClockAcquire(t, results); !errors.Is(result.err, context.Canceled) || result.ticket != nil {
		t.Fatalf("canceled wait became a request: %+v", result)
	}
	if clock.AdvanceNext() || !clock.Now().Equal(before) {
		t.Fatal("a canceled timer advanced the instance clock")
	}
	gate.mu.Lock()
	failures, next := gate.failures, gate.next
	gate.mu.Unlock()
	if failures != 2 || !next.Equal(before.Add(10*time.Second)) {
		t.Fatalf("waiting cancellation changed endpoint health: failures=%d next=%s", failures, next)
	}
}

func TestInstanceClockKeepsOneProbeAndWakesPeersOnProgress(t *testing.T) {
	clock := timingtest.New(time.Now())
	gate := newBackoffGateWithClock(5*time.Second, 30*time.Second, clock)
	healthy, _ := gate.acquire(t.Context(), nil)
	late, _ := gate.acquire(t.Context(), nil)
	healthy.finish(backoffFailure)
	first, firstNotice := acquireWithClock(t, t.Context(), gate)
	second, secondNotice := acquireWithClock(t, t.Context(), gate)
	awaitClockWait(t, firstNotice)
	awaitClockWait(t, secondNotice)
	clock.AdvanceNext()
	var probe clockAcquireResult
	var remaining <-chan clockAcquireResult
	select {
	case probe = <-first:
		remaining = second
	case probe = <-second:
		remaining = first
	case <-time.After(time.Second):
		t.Fatal("no waiter acquired the due probe")
	}
	if probe.err != nil || probe.ticket == nil || !probe.ticket.probe {
		t.Fatalf("due admission did not identify its health owner: %+v", probe)
	}
	late.finish(backoffFailure)
	probe.ticket.progress()
	peer := awaitClockAcquire(t, remaining)
	if peer.err != nil || peer.ticket == nil || peer.ticket.probe {
		t.Fatalf("progress created a second health probe: %+v", peer)
	}
	peer.ticket.finish(backoffFailure)
	probe.ticket.finish(backoffCanceled)
	gate.mu.Lock()
	failures, next := gate.failures, gate.next
	gate.mu.Unlock()
	if failures != 1 || !next.Equal(clock.Now().Add(5*time.Second)) {
		t.Fatalf("canceled probe erased or amplified the outage: failures=%d next=%s", failures, next)
	}
}

func TestPoolClockRemainsInstanceOwnedAcrossAliasesAndInvalidation(t *testing.T) {
	firstClock, secondClock := timingtest.New(time.Now()), timingtest.New(time.Now())
	first := NewWithBackoffClock(config.Default(), firstClock)
	second := NewWithBackoffClock(config.Default(), secondClock)
	first.mu.Lock()
	gate := first.backoffFor("https://clock-fixture.invalid/v1")
	alias := first.backoffFor("https://clock-fixture.invalid/other")
	first.mu.Unlock()
	first.Invalidate()
	first.mu.Lock()
	after := first.backoffFor("https://clock-fixture.invalid/v1")
	first.mu.Unlock()
	second.mu.Lock()
	other := second.backoffFor("https://clock-fixture.invalid/v1")
	second.mu.Unlock()
	if alias != gate || after != gate || gate.clock != firstClock || other == gate || other.clock != secondClock {
		t.Fatal("cache, alias or another pool changed the instance cooldown clock")
	}
	if _, ok := NewWithBackoffClock(config.Default(), nil).backoffClock.(timing.RealClock); !ok {
		t.Fatal("nil pool clock did not retain real runtime timing")
	}
	if _, ok := New(config.Default()).backoffClock.(timing.RealClock); !ok {
		t.Fatal("normal provider construction no longer uses real retry timing")
	}
}
