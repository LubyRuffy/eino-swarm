package engine

import (
	"fmt"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func threadGoalSection(th *store.Thread) string {
	if th == nil {
		return ""
	}
	if !pausedStandingGoal(th) {
		return goalSection(th.Goal, th.GoalComplete, th.GoalBlocked, th.GoalBlockReason)
	}
	body := "## Goal\n\nThe standing objective below is paused. It does not authorize further investigation or automatic continuation. Only an explicit human goal resume starts pursuit again. An ordinary human message does not resume it.\n\n"
	if th.GoalBlocked && strings.TrimSpace(th.GoalBlockReason) != "" {
		body += strings.TrimSpace(th.GoalBlockReason) + "\n\n"
	}
	return body + strings.TrimSpace(th.Goal) + "\n"
}

func (e *Engine) pausedGoalAnswerSection(th *store.Thread, turn *store.Turn) (string, error) {
	if !pausedStandingGoal(th) || turn == nil || turn.GoalContinue || turn.ScheduleContinue {
		return "", nil
	}
	source, err := e.store.TurnInputSource(turn.ThreadID, turn.ID)
	if err != nil {
		return "", err
	}
	if source != store.MessageSourceHuman {
		return "", nil
	}
	return fmt.Sprintf("## This human turn\n\nThe deliverable for this turn is to answer the latest human request quoted below. The paused standing objective remains context, not authorization to advance it. Tools may be used to answer this request. Do not start workers to advance the paused objective. Do not substitute further objective work for this answer.\n\n<turn_request source=\"human\" turn_id=%q>\n%q\n</turn_request>\n", turn.ID, turn.UserText), nil
}
