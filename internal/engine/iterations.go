package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// limitStopError is the turn ending because the human declined to extend
// past the manager's tool-round cap. Distinct from eino's NodeRunError so
// the transcript can say what happened without looking like a crash.
type limitStopError struct {
	Used int
}

func (e limitStopError) Error() string {
	return fmt.Sprintf("stopped after %d tool rounds", e.Used)
}

func isLimitStop(err error) bool {
	var stop limitStopError
	return errors.As(err, &stop)
}

// isMaxIterations reports whether err is eino's ReAct cap, including the
// NodeRunError wrapper the graph puts around the preprocessor failure.
func isMaxIterations(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, adk.ErrExceedMaxIterations) {
		return true
	}
	return strings.Contains(err.Error(), adk.ErrExceedMaxIterations.Error())
}

func (rt *runtime) runManager(ctx context.Context, turn *store.Turn, reg *swarm.Registry,
	acc *accumulator, toolset *tools.Set, pc *projectContext, messages []adk.Message,
) (swarm.RunResult, error) {
	e := rt.engine
	var th *store.Thread
	if got, err := e.store.GetThread(rt.threadID); err == nil {
		th = got
	}
	instruction := ManagerPrompt(toolset, e.cfg, managerExtra(e.cfg, th, pc, e.scheduleLines(rt.threadID)))
	// Fresh slice: pc.managerTools may return the workspace toolset's
	// backing array, and appending complete_goal / block_goal in place
	// would hand them to every sub-agent.
	managerTools := append([]tool.BaseTool(nil), pc.managerTools(toolset)...)
	if th != nil && th.PlanMode {
		managerTools = dropPlanMutatingManagerTools(managerTools)
		tid := rt.threadID
		managerTools = append(managerTools, ProposePlanTool(func(markdown string) (string, error) {
			return e.proposePlanJSON(tid, markdown)
		}))
	}
	if pursuingGoal(th) && (th == nil || !th.PlanMode) {
		tid := rt.threadID
		managerTools = append(managerTools,
			CompleteGoalTool(func(summary string) (string, error) {
				return e.completeGoalJSON(tid, summary)
			}),
			BlockGoalTool(func(reason string) (string, error) {
				return e.blockGoalJSON(tid, reason)
			}),
			ReopenGoalTool(func(reason string) (string, error) {
				return e.reopenGoalJSON(tid, reason)
			}),
		)
	}
	managerTools = append(managerTools, AskUserTool(rt.waitAsk))
	managerTools = append(managerTools, rt.managerScheduleTools(turn)...)
	budget := e.cfg.Swarm.ManagerIterations()
	if pursuingGoal(th) {
		budget = e.cfg.Swarm.GoalSessionIterations()
	}
	used := 0
	modelRetries := 0
	var res swarm.RunResult
	var runErr error
	restore, planted := rt.takeWorkerRestore()
	for {
		res, runErr = reg.RunWith(ctx, swarm.RunConfig{
			Instruction:        instruction,
			Messages:           messages,
			ManagerTools:       managerTools,
			MaxIterations:      budget,
			RestoreWorkers:     restore,
			FinishedWorkers:    planted,
			ManagerMiddlewares: e.autoCompactHandlers(rt.threadID, turn.ID, th),
		}, swarm.Callback(acc.onNotify))
		restore, planted = nil, nil
		used += budget
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		// A human Interrupt Injection cancels the epoch, not this ctx. The
		// generate can die with context.Canceled while the turn is still live;
		// stitch unread steering and keep going. Stop still hits ctx above.
		preempted := reg.TakePreempt()
		if preempted && (runErr == nil || isPreemptRunError(runErr)) {
			if runErr != nil || reg.HasPendingSteers() {
				messages = stitchManagerToolResults(
					nextManagerMessages(res, messages, reg.TakePendingSteerMessages()),
					rt.engine.managerToolResults(turn.ID),
				)
				messages = dropTrailingIncompleteToolCalls(messages)
				continue
			}
		}
		if !isMaxIterations(runErr) {
			if runErr != nil && isRetryableModelError(runErr) && modelRetries < modelErrorRetries {
				modelRetries++
				rt.recordModelRetry(turn, modelRetries)
				messages = rt.retryManagerRun(turn, res, messages, reg)
				continue
			}
			return res, runErr
		}
		if got, err := e.store.GetThread(rt.threadID); err == nil {
			th = got
		}
		if pursuingGoal(th) {
			// eino needs a finite ReAct slice. That is not a /goal
			// session boundary: keep this turn, keep the registry, do
			// not spend goal_max_auto_turns.
			messages = stitchManagerToolResults(
				nextManagerMessages(res, messages, reg.TakePendingSteerMessages()),
				rt.engine.managerToolResults(turn.ID),
			)
			budget = e.cfg.Swarm.GoalSessionIterations()
			continue
		}
		if closedStandingGoal(th) {
			// complete_goal / block_goal landed mid-slice. Asking to
			// extend would pop the non-goal confirm card on a turn
			// that already stopped pursuing.
			return res, nil
		}
		extend := e.cfg.Swarm.ManagerIterations()
		if !rt.waitToExtend(ctx, turn, used, extend) {
			if ctx.Err() != nil {
				return res, runErr
			}
			return res, limitStopError{Used: used}
		}
		messages = nextManagerMessages(res, messages, reg.TakePendingSteerMessages())
		budget = extend
	}
}

// waitToExtend pauses the turn at the tool-round cap until the human
// extends it, declines, or interrupts. The channel is installed before the
// event is recorded so a client that reacts to the event cannot land on
// ContinueTurn before anyone is listening.
func (rt *runtime) waitToExtend(ctx context.Context, turn *store.Turn, used, extend int) bool {
	ch := make(chan bool, 1)
	rt.mu.Lock()
	rt.continueCh = ch
	rt.mu.Unlock()
	defer func() {
		rt.mu.Lock()
		rt.continueCh = nil
		rt.mu.Unlock()
	}()

	// Two ints: fmt keeps this branch-free. encoding/json cannot fail here
	// and a failure would stall the turn with no event for the UI to answer.
	body := fmt.Sprintf(`{"limit":%d,"extend_by":%d}`, used, extend)
	rt.engine.record(store.Event{
		ThreadID: rt.threadID, TurnID: turn.ID,
		Kind: KindMaxIterations, AgentID: swarm.DefaultManagerID,
		Text: body,
	})

	select {
	case proceed := <-ch:
		if proceed {
			rt.engine.record(store.Event{
				ThreadID: rt.threadID, TurnID: turn.ID,
				Kind: KindMaxIterationsContinued, AgentID: swarm.DefaultManagerID,
				Text: fmt.Sprintf("continuing for another %d tool rounds", extend),
			})
		}
		return proceed
	case <-ctx.Done():
		return false
	}
}

// ContinueTurn answers a pause at the manager's tool-round cap. proceed
// extends the current turn by another configured slice; false ends it.
func (e *Engine) ContinueTurn(threadID string, proceed bool) error {
	if _, err := e.store.GetThread(threadID); err != nil {
		return err
	}
	e.mu.Lock()
	rt := e.runtimes[threadID]
	e.mu.Unlock()
	if rt == nil {
		return ErrIdle
	}
	if !rt.status().Running {
		return ErrIdle
	}
	if !rt.signalContinue(proceed) {
		return ErrIdle
	}
	return nil
}

func (rt *runtime) signalContinue(proceed bool) bool {
	rt.mu.Lock()
	ch := rt.continueCh
	rt.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- proceed:
		return true
	default:
		return false
	}
}

// nextManagerMessages feeds the previous transcript back into the next
// ReAct slice. Instruction regenerates the system prompt; unread steers
// land at the end so they are the last thing the manager reads.
func nextManagerMessages(res swarm.RunResult, fallback []adk.Message, extra []*schema.Message) []adk.Message {
	next := messagesWithoutSystem(res.Transcript)
	if len(next) == 0 {
		next = fallback
	}
	if len(extra) == 0 {
		return next
	}
	out := make([]adk.Message, len(next), len(next)+len(extra))
	copy(out, next)
	for _, m := range extra {
		out = append(out, m)
	}
	return out
}

// stitchManagerToolResults puts manager tool_result rows back next to the
// assistant call that produced them. eino's iteration cap is a ChatModel
// preprocessor failure: AfterModel can snapshot the call, then SetHistory
// on the capped next step wipes the wrap-appended results. Without this a
// /goal slice re-reads a stale wait_agents report forever.
func stitchManagerToolResults(msgs []adk.Message, results []store.Event) []adk.Message {
	if len(results) == 0 {
		return msgs
	}
	byID := make(map[string]store.Event, len(results))
	order := make([]string, 0, len(results))
	for _, ev := range results {
		id := strings.TrimSpace(ev.ToolCallID)
		if id == "" {
			continue
		}
		if _, ok := byID[id]; !ok {
			order = append(order, id)
		}
		byID[id] = ev
	}
	if len(byID) == 0 {
		return msgs
	}
	placed := make(map[string]bool, len(byID))
	out := make([]adk.Message, 0, len(msgs)+len(byID))
	for _, m := range msgs {
		if m != nil && m.Role == schema.Tool {
			id := strings.TrimSpace(m.ToolCallID)
			if ev, ok := byID[id]; ok {
				if placed[id] {
					continue
				}
				out = append(out, schema.ToolMessage(ev.Text, id))
				placed[id] = true
				continue
			}
		}
		out = append(out, m)
		if m == nil || m.Role != schema.Assistant || len(m.ToolCalls) == 0 {
			continue
		}
		for _, tc := range m.ToolCalls {
			id := strings.TrimSpace(tc.ID)
			if id == "" || placed[id] {
				continue
			}
			ev, ok := byID[id]
			if !ok {
				continue
			}
			out = append(out, schema.ToolMessage(ev.Text, id))
			placed[id] = true
		}
	}
	for _, id := range order {
		if placed[id] {
			continue
		}
		out = append(out, schema.ToolMessage(byID[id].Text, id))
		placed[id] = true
	}
	return out
}

func (e *Engine) managerToolResults(turnID string) []store.Event {
	if e == nil || turnID == "" {
		return nil
	}
	events, err := e.store.ListTurnEvents(turnID)
	if err != nil {
		return nil
	}
	var out []store.Event
	for _, ev := range events {
		if ev.Kind != swarm.NotifyToolResult.String() {
			continue
		}
		if ev.AgentID != "" && ev.AgentID != swarm.DefaultManagerID {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// messagesWithoutSystem drops leading system messages so a continued run can
// feed the previous transcript back as RunConfig.Messages. Instruction
// regenerates the system prompt; keeping the old one would send two.
func messagesWithoutSystem(msgs []adk.Message) []adk.Message {
	out := make([]adk.Message, 0, len(msgs))
	for _, m := range msgs {
		if m == nil || m.Role == schema.System {
			continue
		}
		out = append(out, m)
	}
	return out
}
