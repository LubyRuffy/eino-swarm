package engine

import (
	"errors"
	"fmt"
	"strings"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// GoalContinueText is the user message the runtime injects to start the next
// turn while a standing objective is still open. Generic on purpose: a
// sample task leaking in here would become the product's continuation protocol.
func GoalContinueText() string {
	return "Continue. The standing objective is still open. Do not wait for another human message. Call complete_goal when it is actually satisfied. Call block_goal when the same obstacle has already been retried and meaningful progress needs the human or an external change."
}

// continueGoal starts the next turn when a standing objective is still open.
// Follow-ups and unread steers already claimed the next turn; this only runs
// after a clean finish with nothing queued. Stop, errors (which also block
// the objective), and a completed, blocked, or capped goal do nothing.
func (rt *runtime) continueGoal(status string) {
	if status != store.TurnDone {
		return
	}
	th, err := rt.engine.store.GetThread(rt.threadID)
	if err != nil || !pursuingGoal(th) {
		rt.reapParked()
		return
	}
	capN := rt.engine.cfg.Swarm.GoalAutoTurns()
	if th.GoalCapped || th.GoalAutoTurns >= capN {
		rt.reapParked()
		rt.markGoalCapped(th, capN)
		return
	}
	next := th.GoalAutoTurns + 1
	if err := rt.engine.store.UpdateThread(rt.threadID, map[string]any{
		"goal_auto_turns": next,
	}); err != nil {
		rt.engine.log.Warn("could not count a goal auto-continue",
			"thread", rt.threadID, "err", err)
		return
	}

	rt.engine.compactBeforeGoalContinue(rt.threadID)

	var startErr error
	for attempt := 0; attempt < 8; attempt++ {
		_, startErr = rt.engine.StartTurnInput(rt.threadID, UserInput{
			Text: GoalContinueText(), ContinueGoal: true,
		})
		if startErr == nil {
			return
		}
		if errors.Is(startErr, ErrBusy) {
			break
		}
		if !strings.Contains(startErr.Error(), "database is locked") &&
			!strings.Contains(startErr.Error(), "SQLITE_BUSY") {
			break
		}
		time.Sleep(time.Duration(20*(attempt+1)) * time.Millisecond)
	}
	if revert := rt.engine.store.UpdateThread(rt.threadID, map[string]any{
		"goal_auto_turns": th.GoalAutoTurns,
	}); revert != nil {
		rt.engine.log.Warn("could not revert a failed goal auto-continue count",
			"thread", rt.threadID, "err", revert)
	}
	if !errors.Is(startErr, ErrBusy) {
		rt.engine.log.Warn("could not auto-continue the standing objective",
			"thread", rt.threadID, "err", startErr)
	}
}

// blockOpenGoalOnTurnError stops auto-continue when a pursuing turn dies.
// The manager never got to call block_goal; leaving the banner on Pursuing
// and kicking another session is how a ChatModel crash loops forever.
func (rt *runtime) blockOpenGoalOnTurnError() {
	rt.engine.blockOpenGoalOnTurnError(rt.threadID)
}

func (e *Engine) blockOpenGoalOnTurnError(threadID string) {
	th, err := e.store.GetThread(threadID)
	if err != nil || !pursuingGoal(th) {
		return
	}
	if err := e.BlockThreadGoal(threadID, goalBlockedByFailedTurn); err != nil {
		e.log.Warn("could not block the standing objective after a failed turn",
			"thread", threadID, "err", err)
	}
}

func (rt *runtime) markGoalCapped(th *store.Thread, capN int) {
	if th.GoalCapped {
		return
	}
	if err := rt.engine.store.UpdateThread(rt.threadID, map[string]any{
		"goal_capped": true,
	}); err != nil {
		rt.engine.log.Warn("could not mark the standing objective as capped",
			"thread", rt.threadID, "err", err)
		return
	}
	body := fmt.Sprintf(`{"auto_turns":%d,"cap":%d}`, th.GoalAutoTurns, capN)
	rt.engine.record(store.Event{
		ThreadID: rt.threadID, TurnID: rt.engine.lastTurnID(rt.threadID),
		Kind: KindGoalCapped, AgentID: swarm.DefaultManagerID,
		Text: body,
	})
}
