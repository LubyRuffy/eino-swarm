package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// KindGoalSession is recorded when a standing-objective turn is forced to
// end so the next session can start. The turn is done, not cancelled.
const KindGoalSession = "goal_session"

const (
	sessionReasonTime       = "time"
	sessionReasonIterations = "iterations"
)

// sessionAfterFunc is the timer used to end a goal session. Tests replace it
// so a 10-minute cap does not have to be waited out.
var sessionAfterFunc = time.AfterFunc

// goalSessionWrapLead is how long before the time cap we steer a wrap-up.
// Shorter than the session itself; skipped when the session is that short.
const goalSessionWrapLead = 30 * time.Second

// parkedWorkersCue is appended when the next session reuses live sub-agents.
// Distinct from resumeWorkersCue: those workers were not restarted.
const parkedWorkersCue = "Sub-agents that were still running continue under their existing ids. Wait for those rather than spawning replacements. Finished workers remain available under the same ids."

// sessionYieldError is a /goal turn ending because the session time cap
// landed. Distinct from interrupt (cancelled) and from eino's ReAct slice
// (which extends the same turn while a goal is open). Historical events
// may still record reason=iterations from builds that cut the session
// there; replay keeps that notice.
type sessionYieldError struct {
	Reason  string
	Rounds  int
	Elapsed time.Duration
}

func (e sessionYieldError) Error() string {
	return fmt.Sprintf("goal session ended (%s)", e.Reason)
}

func asSessionYield(err error) *sessionYieldError {
	var y sessionYieldError
	if errors.As(err, &y) {
		return &y
	}
	return nil
}

func goalSessionWrapSteer() string {
	return "This work session is ending. Summarize current progress. Do not start new long-running work. Call complete_goal only if the standing objective is actually satisfied. Call block_goal if this session retried the same obstacle as the previous one and meaningful progress needs the human or an external change. Otherwise stop this turn without asking the human."
}

func isGoalSessionWrapSteer(text string) bool {
	t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), "[steer]"))
	return strings.HasPrefix(t, "This work session is ending.")
}

func wrapLeadFor(session time.Duration) time.Duration {
	if session <= goalSessionWrapLead {
		return 0
	}
	return goalSessionWrapLead
}

func (rt *runtime) armGoalSession(sessionCancel context.CancelFunc) {
	th, err := rt.engine.store.GetThread(rt.threadID)
	if err != nil || !pursuingGoal(th) {
		return
	}
	rt.mu.Lock()
	rt.sessionStarted = time.Now()
	rt.mu.Unlock()
	d := rt.engine.cfg.Swarm.GoalSessionDuration()
	if lead := wrapLeadFor(d); lead > 0 {
		t := sessionAfterFunc(d-lead, func() {
			_ = rt.injectGoalSessionWrap()
		})
		rt.mu.Lock()
		rt.sessionTimers = append(rt.sessionTimers, t)
		rt.mu.Unlock()
	}
	t := sessionAfterFunc(d, sessionCancel)
	rt.mu.Lock()
	rt.sessionTimers = append(rt.sessionTimers, t)
	rt.mu.Unlock()
}

// injectGoalSessionWrap queues the wrap-up cue for the in-flight manager.
// It must not call Steer: that records a timeline bubble and stores a user
// message, which made a session cut look like human guidance.
func (rt *runtime) injectGoalSessionWrap() bool {
	if !rt.status().Running {
		return false
	}
	rt.mu.Lock()
	reg := rt.reg
	rt.mu.Unlock()
	if reg == nil {
		return false
	}
	return reg.SteerManager(goalSessionWrapSteer())
}

func (rt *runtime) stopGoalSession() {
	rt.mu.Lock()
	timers := rt.sessionTimers
	rt.sessionTimers = nil
	rt.mu.Unlock()
	for _, t := range timers {
		if t != nil {
			t.Stop()
		}
	}
}

func (rt *runtime) turnOutcome(interrupt, session context.Context, runErr error) (status, errText string, yield *sessionYieldError) {
	if interrupt.Err() != nil {
		return store.TurnCancelled, "interrupted", nil
	}
	if y := asSessionYield(runErr); y != nil {
		if y.Elapsed == 0 {
			y.Elapsed = rt.sessionElapsed()
		}
		if y.Rounds == 0 {
			y.Rounds = rt.sessionRounds()
		}
		return store.TurnDone, "", y
	}
	if isLimitStop(runErr) {
		return store.TurnCancelled, runErr.Error(), nil
	}
	// A crashed model call is not a session yield, even if the session
	// timer also cancelled the context. Checking session.Err first used
	// to mark that crash as done and auto-continue into the same failure.
	if runErr != nil && !isContextDone(runErr) {
		return store.TurnError, publicTurnError(runErr), nil
	}
	if session.Err() != nil {
		return store.TurnDone, "", &sessionYieldError{
			Reason:  sessionReasonTime,
			Rounds:  rt.sessionRounds(),
			Elapsed: rt.sessionElapsed(),
		}
	}
	if runErr != nil {
		return store.TurnError, publicTurnError(runErr), nil
	}
	return store.TurnDone, "", nil
}

func isContextDone(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (rt *runtime) sessionElapsed() time.Duration {
	rt.mu.Lock()
	started := rt.sessionStarted
	rt.mu.Unlock()
	if started.IsZero() {
		return 0
	}
	return time.Since(started)
}

func (rt *runtime) sessionRounds() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.sessionUsed
}

func (rt *runtime) setSessionUsed(n int) {
	rt.mu.Lock()
	rt.sessionUsed = n
	rt.mu.Unlock()
}

func (rt *runtime) recordGoalSession(turn *store.Turn, yield *sessionYieldError) {
	if yield == nil || turn == nil {
		return
	}
	body, _ := json.Marshal(struct {
		Reason    string `json:"reason"`
		ElapsedMS int64  `json:"elapsed_ms"`
		Rounds    int    `json:"rounds"`
	}{Reason: yield.Reason, ElapsedMS: yield.Elapsed.Milliseconds(), Rounds: yield.Rounds})
	rt.engine.record(store.Event{
		ThreadID: rt.threadID, TurnID: turn.ID,
		Kind: KindGoalSession, AgentID: swarm.DefaultManagerID,
		Text: string(body),
	})
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

// keepHumanSteers drops the session wrap-up cue. That text is for the
// in-flight manager; if the time cap lands before it is read, turning it
// into a new human message would reset the auto-continue budget forever.
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
	e.captureGoalSessionProgress(threadID, turnID)

	ctx, cancel := context.WithTimeout(context.Background(), e.sessionMemoryRefreshTimeout())
	defer cancel()
	if err := e.syncSessionMemory(ctx, threadID, turnID, false); err != nil {
		e.log.Warn("could not refresh the session briefing before a goal auto-continue",
			"thread", threadID, "err", err)
	}
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
