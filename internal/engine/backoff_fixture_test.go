package engine

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/timing/timingtest"
)

// backoffFixtureClock advances only timers registered by the retry domain. HTTP
// header/body budgets and the engine's scheduling clock remain real and separate.
type backoffFixtureClock struct {
	*timingtest.ManualClock
	mu      sync.Mutex
	stopped bool
	cancel  context.CancelFunc
	done    chan struct{}
}

func newBackoffFixtureClock(t testing.TB) *backoffFixtureClock {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	clock := &backoffFixtureClock{ManualClock: timingtest.New(time.Now()), cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(clock.done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-clock.Registered():
			}
			for {
				clock.mu.Lock()
				advanced := !clock.stopped && clock.AdvanceNext()
				clock.mu.Unlock()
				if !advanced {
					break
				}
			}
		}
	}()
	t.Cleanup(clock.stop)
	return clock
}

func (clock *backoffFixtureClock) stop() {
	clock.mu.Lock()
	if !clock.stopped {
		clock.stopped = true
		clock.cancel()
	}
	clock.mu.Unlock()
	<-clock.done
}

// Attach before any work starts. Each fixture owns its pool and clock; no
// process-wide delay or clock is changed for unrelated tests.
func useEngineBackoffClock(t testing.TB, e *Engine) *backoffFixtureClock {
	t.Helper()
	clock := newBackoffFixtureClock(t)
	e.pool = provider.NewWithBackoffClock(e.cfg, clock)
	e.answerRecoveryClock = clock
	return clock
}

func assertFixtureBackoffWaits(t testing.TB, e *Engine, want ...time.Duration) {
	t.Helper()
	clock, ok := e.answerRecoveryClock.(*backoffFixtureClock)
	if !ok {
		t.Fatal("fixture has no private retry clock")
	}
	if got := clock.Scheduled(); !reflect.DeepEqual(got, want) {
		t.Fatalf("retry timers=%v, want the unchanged production waits %v", got, want)
	}
}

func TestBackoffFixtureClockAdvancesRegisteredTimersAndJoinsItsPump(t *testing.T) {
	clock := newBackoffFixtureClock(t)
	start := clock.Now()
	for _, delay := range []time.Duration{5 * time.Second, 10 * time.Second} {
		timer := clock.NewTimer(delay)
		select {
		case <-timer.C():
		case <-time.After(time.Second):
			t.Fatal("registered retry timer was not advanced")
		}
	}
	want := start.Add(15 * time.Second)
	if !clock.Now().Equal(want) {
		t.Fatalf("logical time=%s, want %s", clock.Now(), want)
	}
	clock.stop()
	clock.stop() // Cleanup may also stop an already joined pump.
	timer := clock.NewTimer(time.Hour)
	select {
	case <-timer.C():
		t.Fatal("joined fixture pump advanced another timer")
	default:
	}
	if !clock.Now().Equal(want) {
		t.Fatal("stopping the fixture advanced logical time")
	}
}

func TestBackoffFixtureClockStopsWhileNoTimerIsRegistered(t *testing.T) {
	clock := newBackoffFixtureClock(t)
	clock.stop()
	select {
	case <-clock.done:
	default:
		t.Fatal("fixture cleanup returned before its pump exited")
	}
}
