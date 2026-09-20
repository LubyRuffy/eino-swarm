package engine

import (
	"context"
	"errors"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// KindGoalSession is the wire name older builds recorded when a standing
// objective turn was forced to end so the next session could start. New
// runs do not emit it: a turn ends when the manager stops calling tools.
// Replay still shows the notice. reason=time was the wall-clock cut;
// reason=iterations was a ReAct slice treated as a session boundary.
const KindGoalSession = "goal_session"

// parkedWorkersCue is appended when the next turn reuses live sub-agents.
// Distinct from resumeWorkersCue: those workers were not restarted.
const parkedWorkersCue = "Sub-agents that were still running continue under their existing ids. Wait for those rather than spawning replacements. Finished workers remain available under the same ids."

// goalSessionWrapSteer is the historical wrap-up leftover. Older builds
// queued it in-process before a wall-clock cut. keepHumanSteers still
// drops it so a replayed unread wrap cannot become a human turn.
func goalSessionWrapSteer() string {
	return "This work session is ending. Summarize current progress. Do not start new long-running work. Call complete_goal only if the standing objective is actually satisfied. Call block_goal if this session retried the same obstacle as the previous one and meaningful progress needs the human or an external change. Otherwise stop this turn without asking the human."
}

func isGoalSessionWrapSteer(text string) bool {
	t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "[steer]"))
	return strings.HasPrefix(t, "This work session is ending.")
}

func (rt *runtime) turnOutcome(interrupt context.Context, runErr error) (status, errText string) {
	if interrupt.Err() != nil {
		return store.TurnCancelled, "interrupted"
	}
	if isLimitStop(runErr) {
		return store.TurnCancelled, runErr.Error()
	}
	if runErr != nil {
		return store.TurnError, publicTurnError(runErr)
	}
	return store.TurnDone, ""
}

func (rt *runtime) shouldPark(status string) bool {
	if status != store.TurnDone || rt.isAbandoned() {
		return false
	}
	th, err := rt.engine.store.GetThread(rt.threadID)
	return err == nil && pursuingGoal(th)
}

func (rt *runtime) parkRegistry(reg *swarm.Registry) {
	if reg == nil {
		return
	}
	rt.mu.Lock()
	rt.parked = reg
	rt.mu.Unlock()
}

func (rt *runtime) takeParkedRegistry() *swarm.Registry {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	reg := rt.parked
	rt.parked = nil
	return reg
}

func (rt *runtime) reapParked() bool {
	rt.mu.Lock()
	reg := rt.parked
	rt.parked = nil
	rt.mu.Unlock()
	if reg == nil {
		return false
	}
	reg.Cleanup()
	reg.Close()
	return true
}

// keepHumanSteers drops the historical session wrap-up leftover. That
// text was for the in-flight manager; turning an unread wrap into a new
// human message would reset the auto-continue budget forever.
func keepHumanSteers(leftover []*schema.Message) (texts []string, images []ImageInput) {
	for _, m := range leftover {
		t := strings.TrimSpace(strings.TrimPrefix(userMessageText(m), "[steer] "))
		if t == "" || isGoalSessionWrapSteer(t) {
			continue
		}
		texts = append(texts, t)
		images = append(images, imagesFromMessage(m)...)
	}
	return texts, images
}

func (e *Engine) goalContextHot(th *store.Thread) bool {
	if th == nil {
		return false
	}
	pct := e.cfg.Swarm.GoalCompactPercent()
	limit := e.cfg.Swarm.AutoCompactLimit()
	turnID := e.lastTurnID(th.ID)
	if snap, err := e.Usage(th.ID, turnID); err == nil && snap.ContextTokens > 0 {
		capTok := limit * pct / 100
		if capTok <= 0 {
			capTok = limit
		}
		if snap.ContextWindow > 0 {
			windowCap := int(int64(snap.ContextWindow) * int64(pct) / 100)
			if windowCap > 0 && windowCap < capTok {
				capTok = windowCap
			}
		}
		return snap.ContextTokens >= capTok
	}
	chars := e.contextChars(th)
	budget := e.cfg.Swarm.ContextBudget()
	return int64(chars)*100 >= int64(budget)*int64(pct)
}

func (e *Engine) sessionMemoryAheadOfCompact(th *store.Thread) bool {
	if th == nil {
		return false
	}
	mem := acceptBriefing(th.SessionMemory, "")
	if mem == "" {
		return false
	}
	return mem != strings.TrimSpace(th.CompactSummary)
}

func (e *Engine) compactBeforeGoalContinue(threadID string) {
	th, err := e.store.GetThread(threadID)
	if err != nil {
		return
	}
	turnID := e.lastTurnID(threadID)
	// Drain an in-flight post-turn refresh first. Merging wrap-up onto a
	// stale read, then letting that refresh write, dropped the wrap-up.
	ctx, cancel := context.WithTimeout(context.Background(), e.sessionMemoryRefreshTimeout())
	defer cancel()
	if err := e.syncSessionMemory(ctx, threadID, turnID, false); err != nil {
		e.log.Warn("could not refresh the session briefing before a goal auto-continue",
			"thread", threadID, "err", err)
	}
	e.captureGoalSessionProgress(threadID, turnID)
	if latest, e2 := e.store.GetThread(threadID); e2 == nil {
		th = latest
	}
	if !e.goalContextHot(th) && !e.sessionMemoryAheadOfCompact(th) {
		return
	}
	if _, err := e.CompactThread(threadID); err != nil &&
		!errors.Is(err, ErrNothingToCompact) && !errors.Is(err, ErrBusy) {
		e.log.Warn("could not compact before a goal auto-continue",
			"thread", threadID, "err", err)
	}
}

const goalSessionProgressKeep = 4

func (e *Engine) captureGoalSessionProgress(threadID, turnID string) {
	if turnID == "" {
		return
	}
	events, _ := e.store.ListEvents(threadID, 0, 0)
	e.mergeSessionMemory(threadID, turnID, goalSessionProgress(events, turnID))
}

func goalSessionProgress(events []store.Event, turnID string) string {
	if turnID == "" {
		return ""
	}
	var msgs []string
	for _, ev := range events {
		if ev.TurnID != turnID {
			continue
		}
		if ev.Kind != swarm.NotifyAgentMessage.String() {
			continue
		}
		if id := strings.TrimSpace(ev.AgentID); id != "" && id != swarm.DefaultManagerID {
			continue
		}
		if t := strings.TrimSpace(ev.Text); t != "" {
			msgs = append(msgs, clip(t, sessionMemoryMessageClip))
		}
	}
	if len(msgs) == 0 {
		return ""
	}
	if len(msgs) > goalSessionProgressKeep {
		msgs = msgs[len(msgs)-goalSessionProgressKeep:]
	}
	return strings.Join(msgs, "\n\n")
}
