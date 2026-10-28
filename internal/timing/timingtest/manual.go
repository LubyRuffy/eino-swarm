// Package timingtest provides an explicitly advanced clock for deterministic
// timing tests. It does not start goroutines or change the process clock.
package timingtest

import (
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/timing"
)

// ManualClock advances only when its owner calls Advance or AdvanceNext.
// Registered lets an owner wait for a timer to be registered before advancing.
type ManualClock struct {
	mu         sync.Mutex
	now        time.Time
	timers     map[*manualTimer]struct{}
	scheduled  []time.Duration
	registered chan struct{}
}

type manualTimer struct {
	clock    *ManualClock
	deadline time.Time
	channel  chan time.Time
	active   bool
}

var _ timing.Clock = (*ManualClock)(nil)

// New starts a clock in its own time domain with no pending timers.
func New(start time.Time) *ManualClock {
	return &ManualClock{
		now:        start,
		timers:     make(map[*manualTimer]struct{}),
		registered: make(chan struct{}, 1),
	}
}

func (c *ManualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// NewTimer registers before notifying Registered. Notifications coalesce and
// never block a caller; an owner must inspect the clock after each wake.
// Nonpositive durations fire once at the current instant.
func (c *ManualClock) NewTimer(d time.Duration) timing.Timer {
	c.mu.Lock()
	timer := &manualTimer{
		clock:    c,
		deadline: c.now.Add(max(d, 0)),
		channel:  make(chan time.Time, 1),
		active:   true,
	}
	c.scheduled = append(c.scheduled, d)
	c.timers[timer] = struct{}{}
	if d <= 0 {
		c.fire(timer)
	}
	c.mu.Unlock()
	select {
	case c.registered <- struct{}{}:
	default:
	}
	return timer
}

func (c *ManualClock) Registered() <-chan struct{} { return c.registered }

// Scheduled returns the original requested durations, including stopped and
// immediately fired timers. Changing the returned slice cannot alter history.
func (c *ManualClock) Scheduled() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.scheduled...)
}

// AdvanceNext advances to the earliest active deadline and fires all timers
// due then. With no active timer it returns false without changing time.
func (c *ManualClock) AdvanceNext() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	var next time.Time
	found := false
	for timer := range c.timers {
		if !found || timer.deadline.Before(next) {
			next, found = timer.deadline, true
		}
	}
	if !found {
		return false
	}
	c.now = next
	c.fireDue()
	return true
}

// Advance moves forward by d and fires every due timer. Negative advancement
// panics so a test cannot silently put deadlines in a different time domain.
func (c *ManualClock) Advance(d time.Duration) {
	if d < 0 {
		panic("timingtest: cannot advance the clock backwards")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	c.fireDue()
}

// fireDue and fire require c.mu. A timer fires only once, so its one-slot
// channel cannot block and remains open after firing or cancellation.
func (c *ManualClock) fireDue() {
	for timer := range c.timers {
		if !timer.deadline.After(c.now) {
			c.fire(timer)
		}
	}
}

func (c *ManualClock) fire(timer *manualTimer) {
	timer.active = false
	delete(c.timers, timer)
	timer.channel <- timer.deadline
}

func (t *manualTimer) C() <-chan time.Time { return t.channel }

func (t *manualTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	if !t.active {
		return false
	}
	t.active = false
	delete(t.clock.timers, t)
	return true
}
