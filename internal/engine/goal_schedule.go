package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func (e *Engine) goalTurnScope(threadID, turnID string) (*store.GoalTurnScope, error) {
	th, err := e.store.GetThread(threadID)
	if err != nil {
		return nil, err
	}
	return &store.GoalTurnScope{ThreadID: threadID, TurnID: turnID, Goal: th.Goal, GoalStartedAt: th.GoalStartedAt}, nil
}

// No objective means no goal/schedule conflict. Existing quiet reports without
// a saved fire remain valid; an objective-bearing fire must prove its identity.
func (e *Engine) wakeGoalScope(threadID, turnID string) (*store.GoalTurnScope, error) {
	scope, err := e.goalTurnScope(threadID, turnID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(turnID) == "" {
		if strings.TrimSpace(scope.Goal) != "" {
			return nil, fmt.Errorf("engine: a goal wake needs its current turn")
		}
		return nil, nil
	}
	return scope, nil
}

func (e *Engine) completeThreadGoal(threadID, turnID, summary string) error {
	e.goalWakeMu.Lock()
	defer e.goalWakeMu.Unlock()
	scope, err := e.goalTurnScope(threadID, turnID)
	if err != nil {
		return err
	}
	text, _ := json.Marshal(struct {
		Summary string `json:"summary,omitempty"`
	}{strings.TrimSpace(summary)})
	eventTurn := turnID
	if eventTurn == "" {
		eventTurn = e.lastTurnID(threadID) // Public, non-tool API metadata only.
	}
	ev := store.Event{ThreadID: threadID, TurnID: eventTurn, Kind: KindGoalComplete, AgentID: swarm.DefaultManagerID, Text: string(text)}
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	if _, dropped := e.droppedTurns[eventTurn]; dropped {
		return store.ErrNotFound
	}
	changed, err := e.store.CompleteGoalForTurn(*scope, &ev)
	if err == nil && changed {
		e.broadcast(ev)
	}
	return err
}

func goalScheduleFailure(err error) string {
	if errors.Is(err, store.ErrThreadWakeDenied) || errors.Is(err, store.ErrWakePolicyPending) {
		return threadWakeFailure(err)
	}
	body := map[string]any{"ok": false, "error": err.Error()}
	if errors.Is(err, store.ErrGoalScheduleConflict) {
		body["code"] = "goal_schedule_conflict"
	}
	out, _ := json.Marshal(body)
	return string(out)
}

func (e *Engine) cancelScheduleForTurn(scope *store.GoalTurnScope, id string) error {
	if scope == nil || strings.TrimSpace(scope.Goal) == "" {
		return e.CancelSchedule(id)
	}
	row, err := e.store.CancelScheduleForTurn(scope, id)
	if err != nil {
		return err
	}
	if row != nil {
		e.recordScheduleCancelled(row)
		e.continueGoalAfterWakeStop(row)
	}
	return nil
}

func (e *Engine) scheduleForGoalReport(turn *store.Turn, scope *store.GoalTurnScope) (*store.Schedule, error) {
	if scope == nil || strings.TrimSpace(scope.Goal) == "" {
		return e.scheduleForTurn(turn), nil
	}
	run, err := e.store.GetRun(turn.ScheduleRunID)
	if err != nil {
		return nil, err
	}
	return e.store.GetSchedule(run.ScheduleID)
}

func (e *Engine) bindScheduledTurn(turn *store.Turn) error {
	return e.store.BindRunIfUnbound(turn.ThreadID, turn.ID, turn.ScheduleRunID)
}
