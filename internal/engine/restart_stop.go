package engine

// interruptThread persists human intent before stopping either a manager or
// its parked workers. A process death must not turn an acknowledged Stop into
// permission to restore an older checkpoint.
func (e *Engine) interruptThread(threadID string) error {
	done, err := e.beginAdmission()
	if err != nil {
		return err
	}
	defer done()
	if _, err := e.store.GetThread(threadID); err != nil {
		return err
	}
	e.mu.Lock()
	rt := e.runtimes[threadID]
	e.mu.Unlock()
	if rt == nil {
		return ErrIdle
	}
	rt.mu.Lock()
	running, parked, turnID := rt.running, rt.parked, rt.turnID
	rt.mu.Unlock()
	if !running && parked == nil {
		return ErrIdle
	}
	if err := e.writeRestartEvent(&restartTarget{rt: rt, turnID: turnID}, KindUserStopRequested, "Stop requested by the user."); err != nil {
		return err
	}
	if running {
		if !rt.interruptTurn(turnID) {
			return ErrBusy
		}
		return nil
	}
	rt.mu.Lock()
	if rt.running || rt.parked != parked || rt.turnID != turnID {
		rt.mu.Unlock()
		return ErrBusy
	}
	rt.recordParkedStop(parked, turnID)
	rt.parked, rt.userStopped = nil, true
	rt.mu.Unlock()
	parked.Cleanup()
	parked.Close()
	return nil
}
