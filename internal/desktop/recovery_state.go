package desktop

import "time"

const (
	recoveryHealthTimeout = 20 * time.Second
	recoveryStartupGrace  = 30 * time.Second
	recoveryReloadTimeout = 15 * time.Second
	recoveryProbeInterval = 5 * time.Second
	recoveryRetryDelay    = 2 * time.Second
	recoveryStablePeriod  = 5 * time.Minute
	recoveryShutdownWait  = 4 * time.Second
	recoveryMaxAttempts   = 2
)

type recoveryAction uint8

const (
	recoveryNoAction recoveryAction = iota
	recoveryProbe
	recoveryReload
	recoveryReplace
	recoveryExhausted
)

type recoveryState struct {
	active       bool
	attempts     int
	reloadUsed   bool
	stage        recoveryAction
	graceUntil   time.Time
	nextProbe    time.Time
	pendingUntil time.Time
	reloadUntil  time.Time
	stableSince  time.Time
}

func newRecoveryState(now time.Time, attempts int) recoveryState {
	return recoveryState{active: true, attempts: attempts, graceUntil: now.Add(recoveryStartupGrace), nextProbe: now}
}

func (s *recoveryState) healthy(now time.Time) {
	if s.stage == recoveryExhausted {
		return
	}
	s.pendingUntil = time.Time{}
	s.graceUntil = now.Add(recoveryHealthTimeout)
	s.nextProbe = now.Add(recoveryProbeInterval)
	s.stage = recoveryNoAction
	if s.stableSince.IsZero() {
		s.stableSince = now
	}
	if now.Sub(s.stableSince) >= recoveryStablePeriod {
		s.attempts, s.reloadUsed = 0, false
	}
}

func (s *recoveryState) failed(now time.Time) {
	s.pendingUntil = time.Time{}
	s.stableSince = time.Time{}
	s.nextProbe = now.Add(recoveryProbeInterval)
}

func (s *recoveryState) tick(now time.Time) recoveryAction {
	if !s.active || s.stage == recoveryExhausted || s.stage == recoveryReplace {
		return recoveryNoAction
	}
	if s.stage == recoveryReload && !now.Before(s.reloadUntil) {
		return s.replace()
	}
	if s.stage == recoveryProbe && !now.Before(s.nextProbe) {
		return s.replace()
	}
	if !s.pendingUntil.IsZero() && !now.Before(s.pendingUntil) {
		s.failed(now)
	}
	if s.stage != recoveryReload && !now.Before(s.graceUntil) {
		s.stableSince = time.Time{}
		if s.reloadUsed {
			return s.replace()
		}
		s.reloadUsed, s.stage = true, recoveryReload
		s.reloadUntil = now.Add(recoveryReloadTimeout)
		s.pendingUntil = time.Time{}
		s.nextProbe = now.Add(time.Second)
		return recoveryReload
	}
	if s.pendingUntil.IsZero() && !now.Before(s.nextProbe) {
		s.pendingUntil = now.Add(recoveryHealthTimeout)
		s.nextProbe = now.Add(recoveryProbeInterval)
		return recoveryProbe
	}
	return recoveryNoAction
}

func (s *recoveryState) replace() recoveryAction {
	if s.attempts >= recoveryMaxAttempts {
		s.stage = recoveryExhausted
		return recoveryExhausted
	}
	s.attempts++
	s.stage = recoveryReplace
	return recoveryReplace
}

func (s *recoveryState) setActive(active bool, now time.Time) {
	s.active = active
	s.stableSince = time.Time{}
	s.pendingUntil = time.Time{}
	s.nextProbe = now
	s.graceUntil = now.Add(recoveryHealthTimeout)
	if s.stage == recoveryReload {
		s.reloadUntil = now.Add(recoveryReloadTimeout)
	}
}

func (s *recoveryState) replacementFailed(now time.Time) {
	s.stage = recoveryProbe
	s.nextProbe = now.Add(recoveryRetryDelay)
}
