// Package timing supplies instance-owned clocks for retry policy waits.
// HTTP deadlines and elapsed request telemetry continue to use real time.
package timing

import "time"

// Timer is one cancellable clock deadline. Its channel must not be closed.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
}

// Clock keeps retry deadlines and their timers in the same time domain.
type Clock interface {
	Now() time.Time
	NewTimer(time.Duration) Timer
}

// RealClock preserves the normal runtime clock and timer behavior.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }

func (RealClock) NewTimer(delay time.Duration) Timer {
	return realTimer{timer: time.NewTimer(delay)}
}

type realTimer struct{ timer *time.Timer }

func (t realTimer) C() <-chan time.Time { return t.timer.C }
func (t realTimer) Stop() bool          { return t.timer.Stop() }

// OrReal makes a nil clock an explicit request for normal runtime timing.
func OrReal(clock Clock) Clock {
	if clock == nil {
		return RealClock{}
	}
	return clock
}
