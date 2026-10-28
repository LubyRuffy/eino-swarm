package timingtest

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/timing"
)

func takeTick(t *testing.T, timer timing.Timer, want time.Time) {
	t.Helper()
	select {
	case got, open := <-timer.C():
		if !open || !got.Equal(want) {
			t.Fatalf("tick=%s open=%v, want %s", got, open, want)
		}
	default:
		t.Fatal("due timer did not deliver its tick")
	}
}

func noTick(t *testing.T, timer timing.Timer) {
	t.Helper()
	select {
	case got, open := <-timer.C():
		t.Fatalf("unexpected tick=%s open=%v", got, open)
	default:
	}
}

func TestRegisteredWakeCannotPrecedeItsTimer(t *testing.T) {
	start := time.Unix(100, 0).UTC()
	clock := New(start)
	created := make(chan timing.Timer, 1)
	go func() { created <- clock.NewTimer(5 * time.Second) }()
	<-clock.Registered()
	// The timer must already be visible even if NewTimer has not returned to
	// the actor which scheduled the wait.
	if !clock.AdvanceNext() {
		t.Fatal("registration wake preceded the actual timer registration")
	}
	timer := <-created
	takeTick(t, timer, start.Add(5*time.Second))
	if timer.Stop() || clock.AdvanceNext() {
		t.Fatal("a fired timer stayed active")
	}
	noTick(t, timer)
}

func TestRegistrationWakeCoalescesWithoutBlockingScheduling(t *testing.T) {
	clock := New(time.Unix(100, 0))
	for i := 0; i < 8; i++ {
		clock.NewTimer(time.Duration(i+1) * time.Second)
	}
	if len(clock.Registered()) != 1 {
		t.Fatal("pending registration wakes did not coalesce")
	}
	<-clock.Registered()
	select {
	case <-clock.Registered():
		t.Fatal("one wake per timer would accumulate stale notifications")
	default:
	}
	clock.NewTimer(9 * time.Second)
	if len(clock.Registered()) != 1 {
		t.Fatal("a later registration did not wake the owner")
	}
}

func TestAdvanceFiresEqualDeadlinesOnceAndKeepsLaterTimersPending(t *testing.T) {
	start := time.Unix(100, 0)
	clock := New(start)
	later := clock.NewTimer(10 * time.Second)
	first := clock.NewTimer(5 * time.Second)
	second := clock.NewTimer(5 * time.Second)
	clock.Advance(4 * time.Second)
	for _, timer := range []timing.Timer{first, second, later} {
		noTick(t, timer)
	}
	if !clock.Now().Equal(start.Add(4 * time.Second)) {
		t.Fatal("explicit advancement did not update the clock")
	}
	if !clock.AdvanceNext() || !clock.Now().Equal(start.Add(5*time.Second)) {
		t.Fatal("AdvanceNext did not choose the earliest deadline")
	}
	takeTick(t, first, start.Add(5*time.Second))
	takeTick(t, second, start.Add(5*time.Second))
	noTick(t, later)
	clock.Advance(8 * time.Second)
	takeTick(t, later, start.Add(10*time.Second))
	for _, timer := range []timing.Timer{first, second, later} {
		noTick(t, timer)
	}
	if clock.AdvanceNext() || !clock.Now().Equal(start.Add(13*time.Second)) {
		t.Fatal("an empty clock advanced or retained a fired timer")
	}
}

func TestCancelledWaitCanStopItsTimerWithoutAdvancingTime(t *testing.T) {
	start := time.Unix(100, 0)
	clock := New(start)
	timer := clock.NewTimer(5 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	<-ctx.Done()
	if !timer.Stop() || timer.Stop() {
		t.Fatal("Stop must remove a live timer exactly once")
	}
	clock.Advance(0)
	if clock.AdvanceNext() || !clock.Now().Equal(start) {
		t.Fatal("a cancelled wait moved the clock")
	}
	noTick(t, timer)
}

func TestNonpositiveTimersFireImmediatelyWithoutStayingActive(t *testing.T) {
	start := time.Unix(100, 0)
	clock := New(start)
	for _, duration := range []time.Duration{0, -time.Second} {
		timer := clock.NewTimer(duration)
		takeTick(t, timer, start)
		if timer.Stop() || clock.AdvanceNext() {
			t.Fatal("an immediately fired timer stayed active")
		}
		noTick(t, timer)
	}
	if len(clock.Registered()) != 1 || !clock.Now().Equal(start) {
		t.Fatal("immediate timers lost notification or moved time")
	}
}

func TestScheduledHistoryKeepsStoppedWaitsAndCannotBeMutatedByItsCaller(t *testing.T) {
	clock := New(time.Unix(100, 0))
	if got := clock.Scheduled(); len(got) != 0 {
		t.Fatalf("new clock has timer history: %v", got)
	}
	first := clock.NewTimer(5 * time.Second)
	first.Stop()
	clock.NewTimer(10 * time.Second)
	clock.NewTimer(0)
	history := clock.Scheduled()
	history[0] = time.Hour
	want := []time.Duration{5 * time.Second, 10 * time.Second, 0}
	if got := clock.Scheduled(); !slices.Equal(got, want) {
		t.Fatalf("scheduled durations=%v, want %v", got, want)
	}
}

func TestBackwardAdvancementCannotChangeTheClock(t *testing.T) {
	start := time.Unix(100, 0)
	clock := New(start)
	defer func() {
		if recover() == nil {
			t.Error("backwards advancement did not refuse")
		}
		if !clock.Now().Equal(start) {
			t.Error("refused advancement changed time")
		}
	}()
	clock.Advance(-time.Nanosecond)
}

func TestConcurrentStopAndFireHaveOnlyOneWinner(t *testing.T) {
	for i := 0; i < 16; i++ {
		clock := New(time.Unix(100, 0))
		timer := clock.NewTimer(time.Second)
		start := make(chan struct{})
		stopped := make(chan bool, 1)
		var workers sync.WaitGroup
		workers.Add(2)
		go func() { defer workers.Done(); <-start; stopped <- timer.Stop() }()
		go func() { defer workers.Done(); <-start; clock.Advance(time.Second) }()
		close(start)
		workers.Wait()
		if <-stopped {
			noTick(t, timer)
		} else {
			takeTick(t, timer, time.Unix(101, 0))
		}
		if timer.Stop() || clock.AdvanceNext() {
			t.Fatal("the stop/fire race retained an active timer")
		}
		noTick(t, timer)
	}
}

func TestConcurrentSchedulingAndInspectionShareOneClockDomain(t *testing.T) {
	start := time.Unix(100, 0)
	clock := New(start)
	const count = 16
	timers := make(chan timing.Timer, count)
	var workers sync.WaitGroup
	workers.Add(count)
	for i := 0; i < count; i++ {
		go func() {
			defer workers.Done()
			timers <- clock.NewTimer(5 * time.Second)
			clock.Now()
			clock.Scheduled()
			clock.Advance(0)
		}()
	}
	workers.Wait()
	close(timers)
	if got := clock.Scheduled(); len(got) != count {
		t.Fatalf("registered history count=%d, want %d", len(got), count)
	}
	if !clock.AdvanceNext() || !clock.Now().Equal(start.Add(5*time.Second)) {
		t.Fatal("concurrent registrations did not share their starting time")
	}
	for timer := range timers {
		takeTick(t, timer, start.Add(5*time.Second))
		noTick(t, timer)
	}
}
