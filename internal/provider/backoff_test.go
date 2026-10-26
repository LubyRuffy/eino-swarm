package provider

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// A healthy endpoint must retain parallel calls, including worker calls.
func TestBackoffHealthyCallsRemainConcurrent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(5*time.Second, 30*time.Second)
		first := mustAcquireBackoff(t, gate, nil)
		second := mustAcquireBackoff(t, gate, nil)
		first.finish(backoffSuccess)
		second.finish(backoffSuccess)
		if gate.failures != 0 || gate.probe {
			t.Fatalf("healthy calls changed endpoint state: %+v", gate)
		}
	})
}

// Failure retries share one cooldown; workers may not each release a request.
func TestBackoffAllowsOnlyOneProbeAndResetsAfterSuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(5*time.Second, 30*time.Second)
		mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
		arrived := make(chan *backoffTicket, 2)
		for range 2 {
			go func() { arrived <- mustAcquireBackoff(t, gate, nil) }()
		}
		synctest.Wait()
		assertNoBackoffTicket(t, arrived)
		time.Sleep(5 * time.Second)
		synctest.Wait()
		probe := <-arrived
		assertNoBackoffTicket(t, arrived)
		probe.finish(backoffSuccess)
		synctest.Wait()
		(<-arrived).finish(backoffSuccess)
		if gate.failures != 0 || gate.probe {
			t.Fatalf("successful probe did not restore healthy concurrency: %+v", gate)
		}
	})
}

func TestBackoffFailuresDoubleUntilCap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(5*time.Second, 30*time.Second)
		mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
		for index, want := range []time.Duration{5, 10, 20, 30, 30} {
			start := time.Now()
			var attempts []int
			var delays []time.Duration
			ticket := mustAcquireBackoff(t, gate, func(attempt int, delay time.Duration) {
				attempts = append(attempts, attempt)
				delays = append(delays, delay)
			})
			if got := time.Since(start); got != want*time.Second {
				t.Fatalf("failure %d waited %s; want %s", index+1, got, want*time.Second)
			}
			if len(attempts) != 1 || attempts[0] != index+1 || delays[0] != want*time.Second {
				t.Fatalf("wait notice = %v / %v, want attempt %d / %s", attempts, delays, index+1, want*time.Second)
			}
			ticket.finish(backoffFailure)
		}
		mustAcquireBackoff(t, gate, nil).finish(backoffSuccess)
		mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
		if got := time.Until(gate.next); got != 5*time.Second {
			t.Fatalf("new outage starts at %s, want initial delay", got)
		}
	})
}

// Replies from pre-outage in-flight calls cannot shorten or amplify an outage.
func TestBackoffIgnoresLateHealthyGenerationResults(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(5*time.Second, 30*time.Second)
		failed := mustAcquireBackoff(t, gate, nil)
		lateSuccess := mustAcquireBackoff(t, gate, nil)
		lateFailure := mustAcquireBackoff(t, gate, nil)
		afterRecovery := mustAcquireBackoff(t, gate, nil)
		failed.finish(backoffFailure)
		lateSuccess.finish(backoffSuccess)
		lateFailure.finish(backoffFailure)
		failed.finish(backoffFailure)
		if gate.failures != 1 || time.Until(gate.next) != 5*time.Second {
			t.Fatalf("late result changed cooldown: failures=%d, next=%s", gate.failures, time.Until(gate.next))
		}
		mustAcquireBackoff(t, gate, nil).finish(backoffSuccess)
		afterRecovery.finish(backoffFailure)
		if gate.failures != 0 {
			t.Fatal("an old in-flight failure reopened an already recovered outage")
		}
	})
}

func TestBackoffCancellationDoesNotReleaseRequestsOrEraseFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(5*time.Second, 30*time.Second)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if ticket, err := gate.acquire(ctx, nil); ticket != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled healthy call = %v, %v", ticket, err)
		}
		mustAcquireBackoff(t, gate, nil).finish(backoffCanceled)
		mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
		for _, probeActive := range []bool{false, true} {
			var probe *backoffTicket
			if probeActive {
				probe = mustAcquireBackoff(t, gate, nil)
			}
			ctx, cancel := context.WithCancel(context.Background())
			result := make(chan error, 1)
			go func() {
				ticket, err := gate.acquire(ctx, nil)
				if ticket != nil {
					t.Error("canceled waiter acquired a request")
				}
				result <- err
			}()
			synctest.Wait()
			cancel()
			synctest.Wait()
			if err := <-result; !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled waiter error = %v", err)
			}
			if probe != nil {
				probe.finish(backoffCanceled)
			}
		}
		if gate.failures != 1 || gate.probe {
			t.Fatalf("cancellation erased outage or stranded probe: %+v", gate)
		}
		start := time.Now()
		mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
		if time.Since(start) != 5*time.Second || gate.failures != 2 || time.Until(gate.next) != 10*time.Second {
			t.Fatalf("replacement probe should retain its cooldown and failure count: %+v", gate)
		}
	})
}

func TestBackoffFinishedOrZeroTicketIsHarmless(t *testing.T) {
	var zero backoffTicket
	zero.finish(backoffFailure)
	var empty *backoffTicket
	empty.finish(backoffFailure)
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(time.Second, 2*time.Second)
		ticket := mustAcquireBackoff(t, gate, nil)
		ticket.finish(backoffSuccess)
		ticket.finish(backoffFailure)
		if gate.failures != 0 {
			t.Fatal("finishing a ticket twice created a phantom failure")
		}
	})
}

func TestBackoffCanceledProbeNotifiesExistingWaitersOfRenewedCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(5*time.Second, 30*time.Second)
		mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
		arrived := make(chan *backoffTicket, 2)
		notices := make(chan time.Duration, 3)
		for range 2 {
			go func() {
				arrived <- mustAcquireBackoff(t, gate, func(attempt int, delay time.Duration) {
					if attempt != 1 {
						t.Errorf("cancellation increased failure attempt to %d", attempt)
					}
					notices <- delay
				})
			}()
		}
		synctest.Wait()
		for range 2 {
			if delay := <-notices; delay != 5*time.Second {
				t.Fatalf("initial cooldown notice=%s", delay)
			}
		}
		time.Sleep(5 * time.Second)
		synctest.Wait()
		(<-arrived).finish(backoffCanceled)
		synctest.Wait()
		assertNoBackoffTicket(t, arrived)
		if delay := <-notices; delay != 5*time.Second {
			t.Fatalf("canceled probe did not renew wait notification: %s", delay)
		}
		time.Sleep(5 * time.Second)
		synctest.Wait()
		(<-arrived).finish(backoffSuccess)
	})
}

func TestBackoffWaitingForActiveProbeReportsUnknownDurationOncePerGeneration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(5*time.Second, 30*time.Second)
		mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
		probe := mustAcquireBackoff(t, gate, nil)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		type notice struct {
			attempt int
			delay   time.Duration
		}
		notices := make(chan notice, 3)
		arrived := make(chan *backoffTicket, 1)
		go func() {
			ticket, _ := gate.acquire(ctx, func(attempt int, delay time.Duration, _ uint64) {
				notices <- notice{attempt: attempt, delay: delay}
			})
			arrived <- ticket
		}()
		synctest.Wait()
		select {
		case got := <-notices:
			if got.attempt != 1 || got.delay != 0 {
				t.Fatalf("active-probe wait notice=%+v, want attempt 1 and unknown duration", got)
			}
		default:
			t.Fatal("a caller waiting for the active probe is missing from Trace")
		}
		probe.finish(backoffFailure)
		synctest.Wait()
		if got := <-notices; got.attempt != 2 || got.delay != 10*time.Second {
			t.Fatalf("next generation must report its new cooldown: %+v", got)
		}
		time.Sleep(10 * time.Second)
		synctest.Wait()
		(<-arrived).finish(backoffSuccess)
		select {
		case duplicate := <-notices:
			t.Fatalf("same wait generation produced a duplicate notice: %+v", duplicate)
		default:
		}
	})
}

func TestBackoffWaitersFollowRepeatedFailuresAndWakeTogetherOnRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(5*time.Second, 30*time.Second)
		mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
		arrived := make(chan *backoffTicket, 8)
		var wg sync.WaitGroup
		for range cap(arrived) {
			wg.Go(func() { arrived <- mustAcquireBackoff(t, gate, nil) })
		}
		synctest.Wait()
		time.Sleep(5 * time.Second)
		synctest.Wait()
		(<-arrived).finish(backoffFailure)
		synctest.Wait()
		assertNoBackoffTicket(t, arrived)
		time.Sleep(9 * time.Second)
		synctest.Wait()
		assertNoBackoffTicket(t, arrived)
		time.Sleep(time.Second)
		synctest.Wait()
		(<-arrived).finish(backoffSuccess)
		wg.Wait()
		for range cap(arrived) - 2 {
			(<-arrived).finish(backoffSuccess)
		}
	})
}

func mustAcquireBackoff(t *testing.T, gate *backoffGate, onWait func(int, time.Duration)) *backoffTicket {
	t.Helper()
	var notify func(int, time.Duration, uint64)
	if onWait != nil {
		notify = func(attempt int, delay time.Duration, _ uint64) { onWait(attempt, delay) }
	}
	ticket, err := gate.acquire(context.Background(), notify)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	return ticket
}

func assertNoBackoffTicket(t *testing.T, tickets <-chan *backoffTicket) {
	t.Helper()
	select {
	case <-tickets:
		t.Fatal("a request escaped the shared endpoint cooldown or active probe")
	default:
	}
}
