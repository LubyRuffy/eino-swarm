package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// KindGoal is recorded when the human sets or clears a standing objective.
const KindGoal = "goal"

// KindGoalComplete is recorded when the manager calls complete_goal (or the
// runtime marks the objective done). Auto-continue stops.
const KindGoalComplete = "goal_complete"

// KindGoalContinued is recorded instead of user_message when the runtime
// starts the next turn to keep pursuing an open objective.
const KindGoalContinued = "goal_continued"

// KindGoalCapped is recorded when consecutive auto-turns hit
// swarm.goal_max_auto_turns. The objective stays open; a human message
// or an explicit resume resets the budget.
const KindGoalCapped = "goal_capped"

// KindGoalBlocked is recorded when the manager calls block_goal: the
// same obstacle has already been retried and progress needs the human
// or an external change. Auto-continue stops until they resume.
const KindGoalBlocked = "goal_blocked"

// KindGoalEdited is recorded when the human changes the objective text
// without reopening pursuit. Status (blocked/capped/complete) stays put.
const KindGoalEdited = "goal_edited"

// KindGoalResumed is recorded when the human starts pursuit again after
// a block, a cap, or an idle open goal.
const KindGoalResumed = "goal_resumed"

// ToolCompleteGoal is the manager-only tool that marks a standing objective
// done. The name travels in transcripts and in Trace, so renaming it is a
// protocol change.
const ToolCompleteGoal = "complete_goal"

// ToolBlockGoal is the manager-only tool that marks a standing objective
// stuck. Same stability rule as complete_goal.
const ToolBlockGoal = "block_goal"

const goalMaxRunes = 2000

const goalContinuedNotice = "Continuing the standing objective."
const goalResumedNotice = "Resuming the standing objective."

// SetThreadGoal stores a standing objective for later turns. An empty value
// clears it. A new value (including replacing a completed one) opens pursuit
// again and resets the auto-continue budget. Clearing interrupts a running
// turn: the human is aborting the pursuit, not waiting for the current answer.
func (e *Engine) SetThreadGoal(id, goal string) error {
	th, err := e.store.GetThread(id)
	if err != nil {
		return err
	}
	goal = clip(strings.TrimSpace(goal), goalMaxRunes)
	clearing := goal == "" && strings.TrimSpace(th.Goal) != ""
	fields := map[string]any{
		"goal":              goal,
		"goal_complete":     false,
		"goal_auto_turns":   0,
		"goal_capped":       false,
		"goal_blocked":      false,
		"goal_block_reason": "",
	}
	if goal == "" {
		fields["goal_started_at"] = nil
	} else {
		fields["goal_started_at"] = time.Now().UTC()
	}
	if err := e.store.UpdateThread(id, fields); err != nil {
		return err
	}
	e.record(store.Event{
		ThreadID: id, TurnID: e.lastTurnID(id),
		Kind: KindGoal, AgentID: swarm.DefaultManagerID,
		Text: goal,
	})
	if clearing && e.Status(id).Running {
		_ = e.Interrupt(id)
	}
	return nil
}

// EditThreadGoal changes the objective text in place. A completed goal is
// reopened (same as setting a new one). Empty clears. While a turn is
// running the new text is steered in so this turn sees it, not only the next.
func (e *Engine) EditThreadGoal(id, goal string) error {
	th, err := e.store.GetThread(id)
	if err != nil {
		return err
	}
	goal = clip(strings.TrimSpace(goal), goalMaxRunes)
	if goal == "" || strings.TrimSpace(th.Goal) == "" || th.GoalComplete {
		return e.SetThreadGoal(id, goal)
	}
	if goal == th.Goal {
		return nil
	}
	if err := e.store.UpdateThread(id, map[string]any{"goal": goal}); err != nil {
		return err
	}
	e.record(store.Event{
		ThreadID: id, TurnID: e.lastTurnID(id),
		Kind: KindGoalEdited, AgentID: swarm.DefaultManagerID,
		Text: goal,
	})
	if e.Status(id).Running {
		e.steerGoalEdit(id, goal)
	}
	return nil
}

func (e *Engine) steerGoalEdit(id, goal string) {
	if err := e.Steer(id, goalUpdatedSteer(goal)); err != nil && !errors.Is(err, ErrIdle) {
		e.log.Warn("could not steer an updated standing objective", "thread", id, "err", err)
	}
}

// CompleteThreadGoal marks the standing objective done. Auto-continue stops
// after the current turn. An already-complete goal is a no-op; no goal is
// an error the tool surfaces as JSON rather than crashing the turn.
func (e *Engine) CompleteThreadGoal(id, summary string) error {
	th, err := e.store.GetThread(id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(th.Goal) == "" {
		return fmt.Errorf("engine: there is no standing objective to complete")
	}
	if th.GoalComplete {
		return nil
	}
	if err := e.store.UpdateThread(id, map[string]any{
		"goal_complete":     true,
		"goal_capped":       false,
		"goal_blocked":      false,
		"goal_block_reason": "",
	}); err != nil {
		return err
	}
	text, _ := json.Marshal(struct {
		Summary string `json:"summary,omitempty"`
	}{Summary: strings.TrimSpace(summary)})
	e.record(store.Event{
		ThreadID: id, TurnID: e.lastTurnID(id),
		Kind: KindGoalComplete, AgentID: swarm.DefaultManagerID,
		Text: string(text),
	})
	return nil
}

// BlockThreadGoal marks the standing objective stuck. Auto-continue stops
// until the human resumes. An already-blocked goal is a no-op; no goal or a
// completed one is an error the tool surfaces as JSON.
func (e *Engine) BlockThreadGoal(id, reason string) error {
	th, err := e.store.GetThread(id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(th.Goal) == "" {
		return fmt.Errorf("engine: there is no standing objective to block")
	}
	if th.GoalComplete {
		return fmt.Errorf("engine: the standing objective is already complete")
	}
	if th.GoalBlocked {
		return nil
	}
	reason = clip(strings.TrimSpace(reason), goalMaxRunes)
	if err := e.store.UpdateThread(id, map[string]any{
		"goal_blocked":      true,
		"goal_block_reason": reason,
		"goal_capped":       false,
	}); err != nil {
		return err
	}
	text, _ := json.Marshal(struct {
		Reason string `json:"reason,omitempty"`
	}{Reason: reason})
	e.record(store.Event{
		ThreadID: id, TurnID: e.lastTurnID(id),
		Kind: KindGoalBlocked, AgentID: swarm.DefaultManagerID,
		Text: string(text),
	})
	return nil
}

// ResumeThreadGoal clears a block or cap and starts the next turn. An idle
// open goal (interrupted, or set and not yet started) starts the same way.
// A completed goal or a missing one is an error; a running turn is ErrBusy.
func (e *Engine) ResumeThreadGoal(id string) (*store.Turn, error) {
	th, err := e.store.GetThread(id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(th.Goal) == "" {
		return nil, fmt.Errorf("engine: there is no standing objective to resume")
	}
	if th.GoalComplete {
		return nil, fmt.Errorf("engine: the standing objective is already complete")
	}
	if e.Status(id).Running {
		return nil, ErrBusy
	}
	prev := map[string]any{
		"goal_auto_turns":   th.GoalAutoTurns,
		"goal_capped":       th.GoalCapped,
		"goal_blocked":      th.GoalBlocked,
		"goal_block_reason": th.GoalBlockReason,
	}
	if err := e.store.UpdateThread(id, map[string]any{
		"goal_auto_turns":   0,
		"goal_capped":       false,
		"goal_blocked":      false,
		"goal_block_reason": "",
	}); err != nil {
		return nil, err
	}
	e.record(store.Event{
		ThreadID: id, TurnID: e.lastTurnID(id),
		Kind: KindGoalResumed, AgentID: swarm.DefaultManagerID,
		Text: goalResumedNotice,
	})
	turn, startErr := e.StartTurnInput(id, UserInput{
		Text: GoalContinueText(), ContinueGoal: true,
	})
	if startErr != nil {
		e.revertGoalResume(id, prev)
		return nil, startErr
	}
	return turn, nil
}

func (e *Engine) revertGoalResume(id string, prev map[string]any) {
	if err := e.store.UpdateThread(id, prev); err != nil {
		e.log.Warn("could not revert a failed goal resume", "thread", id, "err", err)
	}
}

func hasOpenGoal(th *store.Thread) bool {
	return th != nil && strings.TrimSpace(th.Goal) != "" && !th.GoalComplete
}

func pursuingGoal(th *store.Thread) bool {
	return hasOpenGoal(th) && !th.GoalBlocked
}

func resetGoalBudget(e *Engine, th *store.Thread) {
	if th == nil || (th.GoalAutoTurns == 0 && !th.GoalCapped && !th.GoalBlocked) {
		return
	}
	if err := e.store.UpdateThread(th.ID, map[string]any{
		"goal_auto_turns":   0,
		"goal_capped":       false,
		"goal_blocked":      false,
		"goal_block_reason": "",
	}); err != nil {
		e.log.Warn("could not reset the goal auto-continue budget",
			"thread", th.ID, "err", err)
	}
}

func goalUpdatedSteer(goal string) string {
	return "The standing objective was updated. Pursue the text below; it replaces the previous objective.\n\n" + goal
}
