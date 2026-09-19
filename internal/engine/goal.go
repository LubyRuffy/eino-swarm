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
// swarm.goal_max_auto_turns, or when the human interrupts a pursuing
// turn. The objective stays open; a human message or an explicit resume
// resets the budget. Interrupt payload is JSON {reason:"interrupted"};
// a budget cap is {auto_turns,cap}.
const KindGoalCapped = "goal_capped"

// KindGoalBlocked is recorded when the manager calls block_goal, or when
// a pursuing turn fails for a reason that is not a recoverable model error.
// Truncated tool JSON / 429 / a dropped stream auto-continue instead.
// Auto-continue stops until the human resumes.
const KindGoalBlocked = "goal_blocked"

// KindGoalEdited is recorded when the human changes the objective text
// without reopening pursuit. Status (blocked/capped/complete) stays put.
const KindGoalEdited = "goal_edited"

// KindGoalIdle is recorded when an engine-started continuation finished
// with no counted tool activity. Auto-continue stops until a human
// message or resume; the objective stays open.
const KindGoalIdle = "goal_idle"

// KindGoalResumed is recorded when the human starts pursuit again after
// a block, a cap (including a stop), an idle open goal, or a completed
// objective that was closed in error. The manager's reopen_goal uses the
// same kind so the banner drops Done without a new wire name.
const KindGoalResumed = "goal_resumed"

// ToolCompleteGoal is the manager-only tool that marks a standing objective
// done. The name travels in transcripts and in Trace, so renaming it is a
// protocol change.
const ToolCompleteGoal = "complete_goal"

// ToolBlockGoal is the manager-only tool that marks a standing objective
// stuck. Same stability rule as complete_goal.
const ToolBlockGoal = "block_goal"

// ToolReopenGoal is the manager-only tool that undoes a complete_goal from
// this turn. Same stability rule as complete_goal.
const ToolReopenGoal = "reopen_goal"

const goalMaxRunes = 2000

const goalContinuedNotice = "Continuing the standing objective."
const goalResumedNotice = "Resuming the standing objective."
const goalIdleNotice = "Stopped auto-continuing: the last continuation made no progress."

// goalBlockedByFailedTurn is the fallback banner reason when a pursuing
// turn dies before the manager can call block_goal and the turn row has
// no public error. Prefer the turn's Error: folding a session used to
// hide the dump that this sentinel was supposed to point at.
const goalBlockedByFailedTurn = "the last turn failed"

// SetThreadGoal stores a standing objective for later turns. An empty value
// clears it. A new value (including replacing a completed one) opens pursuit
// again and resets the auto-continue budget. Clearing interrupts a running
// turn: the human is aborting the pursuit, not waiting for the current answer.
// Setting while a turn is running steers the new text in so this turn sees
// it, not only the next.
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
		"goal_idle":         false,
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
	if clearing {
		_ = e.Interrupt(id)
	} else if e.Status(id).Running {
		// Same as an in-place edit: this turn already has a prompt. Without
		// a steer, `/goal` during a run looks like it did nothing until the
		// next session.
		e.steerGoalEdit(id, goal)
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
		"goal_idle":         false,
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
		"goal_idle":         false,
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

// ResumeThreadGoal clears a block, cap, idle hold, or a completed mark and
// starts the next turn. A missing goal is an error; a running turn is ErrBusy.
// Completing used to be terminal; a mistaken complete_goal left no Play
// control, so resume reopens that case too.
func (e *Engine) ResumeThreadGoal(id string) (*store.Turn, error) {
	th, err := e.store.GetThread(id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(th.Goal) == "" {
		return nil, fmt.Errorf("engine: there is no standing objective to resume")
	}
	if e.Status(id).Running {
		return nil, ErrBusy
	}
	prev := map[string]any{
		"goal_complete":     th.GoalComplete,
		"goal_auto_turns":   th.GoalAutoTurns,
		"goal_capped":       th.GoalCapped,
		"goal_blocked":      th.GoalBlocked,
		"goal_block_reason": th.GoalBlockReason,
		"goal_idle":         th.GoalIdle,
	}
	if err := e.store.UpdateThread(id, map[string]any{
		"goal_complete":     false,
		"goal_auto_turns":   0,
		"goal_capped":       false,
		"goal_blocked":      false,
		"goal_block_reason": "",
		"goal_idle":         false,
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

// ReopenThreadGoal clears a complete_goal from this conversation so pursuit
// continues when the current turn ends. The manager calls this after a
// mistaken complete; the human Play path is ResumeThreadGoal. The
// auto-continue budget resets the same way Start does — otherwise a
// complete at the cap would reopen and immediately recap. Not complete
// is an error so a spurious call cannot hide that nothing closed.
func (e *Engine) ReopenThreadGoal(id, reason string) error {
	th, err := e.store.GetThread(id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(th.Goal) == "" {
		return fmt.Errorf("engine: there is no standing objective to reopen")
	}
	if !th.GoalComplete {
		return fmt.Errorf("engine: the standing objective is not complete")
	}
	if err := e.store.UpdateThread(id, map[string]any{
		"goal_complete":     false,
		"goal_auto_turns":   0,
		"goal_capped":       false,
		"goal_blocked":      false,
		"goal_block_reason": "",
		"goal_idle":         false,
	}); err != nil {
		return err
	}
	e.record(store.Event{
		ThreadID: id, TurnID: e.lastTurnID(id),
		Kind: KindGoalResumed, AgentID: swarm.DefaultManagerID,
		Text: goalResumedNotice,
	})
	return nil
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

// closedStandingGoal is complete or blocked: the text is still there, but
// this turn must not keep the ReAct loop or ask the human to extend it.
func closedStandingGoal(th *store.Thread) bool {
	return th != nil && strings.TrimSpace(th.Goal) != "" && !pursuingGoal(th)
}

func resetGoalBudget(e *Engine, th *store.Thread) {
	if th == nil || (th.GoalAutoTurns == 0 && !th.GoalCapped && !th.GoalBlocked && !th.GoalIdle) {
		return
	}
	if err := e.store.UpdateThread(th.ID, map[string]any{
		"goal_auto_turns":   0,
		"goal_capped":       false,
		"goal_blocked":      false,
		"goal_block_reason": "",
		"goal_idle":         false,
	}); err != nil {
		e.log.Warn("could not reset the goal auto-continue budget",
			"thread", th.ID, "err", err)
	}
}

func goalUpdatedSteer(goal string) string {
	return "The standing objective was updated. Pursue the text below; it replaces the previous objective.\n\n" + goal
}
