package lease

import (
	"context"
	"time"
)

// StartGrace is how long a fresh engine stays up before the first shell
// connects. IdleGrace is how long it stays up after the last client, the
// phone, and the last turn are all gone. Neither is a user setting.
const (
	StartGrace = 15 * time.Second
	IdleGrace  = 3 * time.Second
)

// Load is everything that keeps the engine process alive.
type Load struct {
	Clients      int
	PhoneOnline  bool
	RunningTurns int
	Age          time.Duration
	QuietFor     time.Duration
}

// ShouldStop reports whether the engine process should exit. A connected
// shell, a phone on the hub, or a turn still running keeps it. So does the
// start grace, and a quiet stretch shorter than the idle grace.
func ShouldStop(load Load) bool {
	return stopAfter(load, StartGrace, IdleGrace)
}

func stopAfter(load Load, startGrace, idleGrace time.Duration) bool {
	if load.Clients > 0 || load.PhoneOnline || load.RunningTurns > 0 {
		return false
	}
	if load.Age < startGrace {
		return false
	}
	return load.QuietFor >= idleGrace
}

// Watcher polls until the process should exit or ctx is cancelled. Zero
// durations use StartGrace, IdleGrace, and a short poll.
type Watcher struct {
	StartGrace time.Duration
	IdleGrace  time.Duration
	Poll       time.Duration
	Now        func() time.Time
}

// Run blocks. Cancel ctx to stop the engine on purpose (zwai engine's own
// interrupt). Returning without a cancel is the idle exit.
func (w Watcher) Run(ctx context.Context, started time.Time, sample func() (clients int, phone bool, turns int)) {
	start, idle, poll := w.StartGrace, w.IdleGrace, w.Poll
	if start == 0 {
		start = StartGrace
	}
	if idle == 0 {
		idle = IdleGrace
	}
	if poll == 0 {
		poll = 200 * time.Millisecond
	}
	now := w.Now
	if now == nil {
		now = time.Now
	}
	var quietFrom time.Time
	timer := time.NewTimer(poll)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			clients, phone, turns := sample()
			at := now()
			var quiet time.Duration
			if clients > 0 || phone || turns > 0 {
				quietFrom = time.Time{}
			} else {
				if quietFrom.IsZero() {
					quietFrom = at
				}
				quiet = at.Sub(quietFrom)
			}
			if stopAfter(Load{
				Clients: clients, PhoneOnline: phone, RunningTurns: turns,
				Age: at.Sub(started), QuietFor: quiet,
			}, start, idle) {
				return
			}
			timer.Reset(poll)
		}
	}
}
