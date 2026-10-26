package provider

import (
	"context"
	"crypto/rand"
	"sync"
	"time"
)

type backoffOutcome uint8

const (
	backoffCanceled backoffOutcome = iota
	backoffSuccess
	backoffFailure
)

// A gate belongs to an endpoint, so workers cannot turn one outage into many
// independent retry loops. Healthy requests remain concurrent.
type backoffGate struct {
	id         string
	mu         sync.Mutex
	initial    time.Duration
	max        time.Duration
	delay      time.Duration
	failures   int
	next       time.Time
	probe      bool
	probeReady bool
	generation uint64
	changed    chan struct{}
}

type backoffTicket struct {
	gate       *backoffGate
	generation uint64
	probe      bool
	once       sync.Once
}

// The caller supplies the configured positive durations, with max >= initial.
func newBackoffGate(initial, max time.Duration) *backoffGate {
	return &backoffGate{id: rand.Text(), initial: initial, max: max, changed: make(chan struct{})}
}

func (g *backoffGate) acquire(ctx context.Context, onWait func(int, time.Duration, uint64)) (*backoffTicket, error) {
	var notified uint64
	for {
		g.mu.Lock()
		if err := ctx.Err(); err != nil {
			g.mu.Unlock()
			return nil, err
		}
		delay := time.Until(g.next)
		if g.failures == 0 || g.probeReady || (!g.probe && delay <= 0) {
			isProbe := g.failures > 0 && !g.probeReady
			if isProbe {
				g.probe = true
			}
			ticket := &backoffTicket{gate: g, generation: g.generation, probe: isProbe}
			g.mu.Unlock()
			return ticket, nil
		}
		changed, generation, attempt := g.changed, g.generation, g.failures
		g.mu.Unlock()
		if delay > 0 {
			timer := time.NewTimer(delay)
			if onWait != nil && notified != generation {
				notified = generation
				onWait(attempt, delay, generation)
			}
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-changed:
			case <-timer.C:
			}
			timer.Stop()
		} else {
			if onWait != nil && notified != generation {
				notified = generation
				onWait(attempt, 0, generation)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-changed:
			}
		}
	}
}

// Useful decoded output releases concurrency, but the probe still owns health.
// Do not advance the generation: its final result must settle this same outage.
func (t *backoffTicket) progress() {
	if t == nil || t.gate == nil || !t.probe {
		return
	}
	g := t.gate
	g.mu.Lock()
	defer g.mu.Unlock()
	if t.generation != g.generation || !g.probe || g.probeReady {
		return
	}
	g.probeReady = true
	close(g.changed)
	g.changed = make(chan struct{})
}

func (t *backoffTicket) finish(outcome backoffOutcome) {
	if t == nil || t.gate == nil {
		return
	}
	t.once.Do(func() {
		g := t.gate
		g.mu.Lock()
		defer g.mu.Unlock()
		// Only the first failure from the healthy generation and the current
		// probe own endpoint health. A late parallel reply has no authority.
		if t.generation != g.generation || (!t.probe && g.failures > 0) {
			return
		}
		switch outcome {
		case backoffFailure:
			g.failures++
			if g.failures == 1 {
				g.delay = g.initial
			} else if g.delay >= g.max-g.delay {
				g.delay = g.max
			} else {
				g.delay *= 2
			}
			g.next = time.Now().Add(g.delay)
			g.generation++
		case backoffSuccess:
			if !t.probe {
				return
			}
			g.failures, g.delay = 0, 0
			g.next = time.Time{}
			g.generation++
		case backoffCanceled:
			if !t.probe {
				return
			}
			// Cancellation does not prove recovery. Retain the failure streak,
			// and prevent repeated cancellations from opening rapid probes.
			g.next = time.Now().Add(g.delay)
			g.generation++
		}
		g.probe, g.probeReady = false, false
		close(g.changed)
		g.changed = make(chan struct{})
	})
}
