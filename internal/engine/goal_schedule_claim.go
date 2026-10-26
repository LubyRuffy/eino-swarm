package engine

import (
	"encoding/json"
	"errors"
	"fmt"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const scheduleClaimGoalFailureCap = 3

func isScheduleClaimMismatch(err error) bool {
	var failure *scheduleAnswerError
	return errors.As(err, &failure) && failure.Code == "no_matching_schedule"
}

func isStoredScheduleClaimMismatch(turn *store.Turn) bool {
	return turn != nil && turn.Status == store.TurnError &&
		turn.Error == (&scheduleAnswerError{Code: "no_matching_schedule"}).Error()
}

// Count durable failures rather than in-memory attempts so replacing the
// engine cannot renew this allowance. Only explicit human/new-goal boundaries
// reset it; the manager's reopen_goal does not grant more attempts.
func (e *Engine) scheduleClaimGoalFailures(th *store.Thread, last *store.Turn) (int, error) {
	turns, err := e.store.ListTurns(th.ID)
	if err != nil {
		return 0, err
	}
	current := len(turns) - 1
	for current >= 0 && turns[current].ID != last.ID {
		current--
	}
	if current < 0 || !isStoredScheduleClaimMismatch(&turns[current]) {
		return 0, fmt.Errorf("missing durable schedule claim terminal state for turn %s", last.ID)
	}
	var resetSeq int64
	for _, kind := range []string{KindGoal, KindGoalResumed} {
		events, err := e.store.ListEventsOfKind(th.ID, kind, 0, -1)
		if err != nil {
			return 0, err
		}
		for _, ev := range events {
			if (kind == KindGoal || ev.Role == "human") && ev.Seq > resetSeq {
				resetSeq = ev.Seq
			}
		}
	}
	count := 0
	for i := current; i >= 0 && count < scheduleClaimGoalFailureCap; i-- {
		turn := turns[i]
		if !isStoredScheduleClaimMismatch(&turn) {
			break
		}
		events, err := e.store.ListTurnEvents(turn.ID)
		if err != nil {
			return 0, err
		}
		var terminalSeq int64
		for _, ev := range events {
			if ev.Kind == swarm.NotifyError.String() && isManagerAgent(ev.AgentID) && ev.Err == turn.Error && ev.Seq > terminalSeq {
				terminalSeq = ev.Seq
			}
		}
		if terminalSeq == 0 {
			return 0, fmt.Errorf("missing schedule claim terminal receipt for turn %s", turn.ID)
		}
		if terminalSeq <= resetSeq {
			break
		}
		count++
		if !turn.GoalContinue && !turn.ScheduleContinue {
			break
		}
	}
	return count, nil
}

// A protocol rejection is not an external blocker. Pause automatic pursuit at
// its own finite limit, or when its durable budget cannot safely be recovered.
func (e *Engine) allowScheduleClaimGoalRetry(th *store.Thread, last *store.Turn) bool {
	if th.GoalCapped {
		return false
	}
	count, err := e.scheduleClaimGoalFailures(th, last)
	reason, code := "schedule_claim_unverified", "no_matching_schedule"
	if err != nil {
		reason, code = "schedule_claim_retry_state_unavailable", "retry_state_unavailable"
		e.log.Warn("could not recover goal schedule claim retry budget", "thread", th.ID, "turn", last.ID, "err", err)
	} else if count < scheduleClaimGoalFailureCap {
		return true
	}
	body, _ := json.Marshal(map[string]any{"reason": reason, "code": code, "attempt": count, "cap": scheduleClaimGoalFailureCap})
	ev := store.Event{ThreadID: th.ID, TurnID: last.ID, Kind: KindGoalCapped, AgentID: swarm.DefaultManagerID, Text: string(body)}
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	changed, err := e.store.CapGoalAfterTurn(store.GoalTurnScope{ThreadID: th.ID, TurnID: last.ID, Goal: th.Goal, GoalStartedAt: th.GoalStartedAt}, th.GoalAutoTurns, &ev)
	if err != nil {
		e.log.Warn("could not pause goal schedule claim retries", "thread", th.ID, "turn", last.ID, "err", err)
	} else if changed {
		e.broadcast(ev)
	}
	return false
}

func (e *Engine) allowFailedScheduleClaimGoalRetry(turn *store.Turn) bool {
	th, err := e.store.GetThread(turn.ThreadID)
	if err != nil || !pursuingGoal(th) {
		return false
	}
	// The typed runtime failure remains authoritative even if a read of the
	// terminal turn fails. Unknown recovery state pauses instead of blocking.
	last := *turn
	last.Status, last.Error = store.TurnError, (&scheduleAnswerError{Code: "no_matching_schedule"}).Error()
	return e.allowScheduleClaimGoalRetry(th, &last)
}
