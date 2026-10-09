package engine

import (
	"errors"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func (e *Engine) pauseScheduledWorkerTimeout(turn *store.Turn, runErr error) bool {
	var exhausted *swarm.WorkerTimeoutLimitError
	if turn == nil || !turn.ScheduleContinue || !errors.As(runErr, &exhausted) {
		return false
	}
	sch := e.scheduleForTurn(turn)
	if sch == nil || sch.Status != store.ScheduleActive || (sch.EveryS <= 0 && sch.Cron == "") {
		return false
	}
	if _, err := e.PatchSchedule(sch.ID, store.SchedulePaused); err != nil {
		e.log.Warn("could not pause a recurring schedule after worker timeout recovery exhausted", "schedule", sch.ID, "err", err)
		return false
	}
	return true
}

func (rt *runtime) resetRestoredWorkerTimeouts() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	for i := range rt.restore {
		rt.restore[i].TimeoutFailures = 0
	}
	for i := range rt.planted {
		rt.planted[i].TimeoutFailures = 0
	}
}
