package engine

import "time"

// Summary progress is read without copying multi-megabyte worker checkpoints.
// The live stage remains distinct from the count of durable checkpoint writes.
func restartSummaryProgress(targets []*restartTarget) int {
	settled := 0
	for _, target := range targets {
		settled += target.reg.HandoffProgress().Settled
	}
	return settled
}

func (e *Engine) observeRestartSummaries(targets []*restartTarget, done <-chan struct{}, warnAt time.Time) {
	timer := time.NewTimer(max(0, time.Until(warnAt)))
	defer timer.Stop()
	select {
	case <-done:
		return
	case <-timer.C:
	}
	for _, target := range targets {
		progress := target.reg.HandoffProgress()
		if len(progress.Pending) != 0 {
			e.log.Warn("engine restart handoff exceeded half its budget", "thread_id", target.rt.threadID,
				"turn_id", target.turnID, "settled", progress.Settled, "total", progress.Total, "pending_workers", progress.Pending)
		}
	}
	<-done
}
