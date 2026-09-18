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
	return "Continue. The standing objective is still open. Ending this turn does not shrink the objective. Inspect current state, make concrete progress, or poll a live wait that is still running. Call complete_goal only when evidence proves it is satisfied. Call block_goal only when the same genuine blocker has already repeated for at least three consecutive turns and meaningful progress needs the human or an external change. Do not wait for a free-form chat line. To ask a material question, call ask_user."
}

// continueGoal starts the next turn when a standing objective is still open.
// Follow-ups and unread steers already claimed the next turn; this only runs
// after a clean finish with nothing queued. Stop, errors (which also block
// the objective), a completed, blocked, capped, or idle-held goal, and a
// continuation that made no counted tool progress do nothing.
func (rt *runtime) continueGoal(status string) {
	if status != store.TurnDone {
		return
	}
	th, err := rt.engine.store.GetThread(rt.threadID)
	if err != nil || !pursuingGoal(th) {
		rt.reapParked()
		return
	}
	if th.PlanMode {
		rt.reapParked()
		return
	}
	if th.GoalIdle {
		rt.reapParked()
		return
	}
	capN := rt.engine.cfg.Swarm.GoalAutoTurns()
	if th.GoalCapped || th.GoalAutoTurns >= capN {
		rt.reapParked()
		rt.markGoalCapped(th, capN)
		return
	}
	// Budget still remains: an empty continuation is a loop, not a cap.
	if last := lastThreadTurn(rt.engine, rt.threadID); last != nil && last.GoalContinue &&
		!rt.engine.turnHasCountedGoalActivity(rt.threadID, last.ID) {
		rt.reapParked()
		rt.engine.holdGoalIdle(rt.threadID)
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

// blockOpenGoalOnTurnError stops auto-continue when a pursuing turn dies
// after in-turn retries. The manager never got to call block_goal; leaving
// the banner on Pursuing and kicking another session is how a ChatModel
// crash loops forever. Truncated tool JSON / 429 / a dropped stream retry
// inside the same turn first.
func (rt *runtime) blockOpenGoalOnTurnError() {
	rt.engine.blockOpenGoalOnTurnError(rt.threadID)
}

func (e *Engine) blockOpenGoalOnTurnError(threadID string) {
	th, err := e.store.GetThread(threadID)
	if err != nil || !pursuingGoal(th) {
		return
	}
	errText := ""
	if turns, listErr := e.store.ListTurns(threadID); listErr == nil && len(turns) > 0 {
		errText = turns[len(turns)-1].Error
	}
	if err := e.BlockThreadGoal(threadID, failedTurnBlockReason(errText)); err != nil {
		e.log.Warn("could not block the standing objective after a failed turn",
			"thread", threadID, "err", err)
	}
}

// failedTurnBlockReason is what the banner shows after a crash. Empty
// falls back to the sentinel so older clients still have a string.
func failedTurnBlockReason(errText string) string {
	if s := strings.TrimSpace(errText); s != "" {
		return s
	}
	return goalBlockedByFailedTurn
}

// goalCappedReasonInterrupted is the goal_capped payload when Stop paused
// pursuit. Distinct from {auto_turns,cap} so Trace does not look like the
// auto-continue budget ran out.
const goalCappedReasonInterrupted = `{"reason":"interrupted"}`

func (rt *runtime) pauseOpenGoalOnInterrupt() {
	rt.engine.pauseOpenGoalOnInterrupt(rt.threadID)
}

func (e *Engine) pauseOpenGoalOnInterrupt(threadID string) {
	th, err := e.store.GetThread(threadID)
	if err != nil || !pursuingGoal(th) || th.GoalCapped {
		return
	}
	if err := e.store.UpdateThread(threadID, map[string]any{
		"goal_capped": true,
	}); err != nil {
		e.log.Warn("could not pause the standing objective after interrupt",
			"thread", threadID, "err", err)
		return
	}
	e.record(store.Event{
		ThreadID: threadID, TurnID: e.lastTurnID(threadID),
		Kind: KindGoalCapped, AgentID: swarm.DefaultManagerID,
		Text: goalCappedReasonInterrupted,
	})
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

func lastThreadTurn(e *Engine, threadID string) *store.Turn {
	turns, err := e.store.ListTurns(threadID)
	if err != nil || len(turns) == 0 {
		return nil
	}
	return &turns[len(turns)-1]
}

func (e *Engine) holdGoalIdle(threadID string) {
	th, err := e.store.GetThread(threadID)
	if err != nil || !pursuingGoal(th) || th.GoalIdle {
		return
	}
	if err := e.store.UpdateThread(threadID, map[string]any{"goal_idle": true}); err != nil {
		e.log.Warn("could not hold auto-continue after a no-progress continuation",
			"thread", threadID, "err", err)
		return
	}
	e.record(store.Event{
		ThreadID: threadID, TurnID: e.lastTurnID(threadID),
		Kind: KindGoalIdle, AgentID: swarm.DefaultManagerID,
		Text: goalIdleNotice,
	})
}

// turnHasCountedGoalActivity reports tool work that moved the objective.
// complete_goal / block_goal close pursuit themselves and must not count
// as "keep going" activity.
func (e *Engine) turnHasCountedGoalActivity(threadID, turnID string) bool {
	if turnID == "" {
		return false
	}
	events, err := e.store.ListEvents(threadID, 0, 0)
	if err != nil {
		return false
	}
	return countedGoalActivity(events, turnID)
}

func countedGoalActivity(events []store.Event, turnID string) bool {
	for _, ev := range events {
		if ev.TurnID != turnID || ev.Kind != swarm.NotifyToolCall.String() {
			continue
		}
		switch toolCallName(ev.Text) {
		case ToolCompleteGoal, ToolBlockGoal, "":
			continue
		default:
			return true
		}
	}
	return false
}
