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
	extra := pc.promptSections()
	var th *store.Thread
	if got, err := e.store.GetThread(rt.threadID); err == nil {
		th = got
		extra = conversationExtra(th, pc)
	}
	instruction := managerPrompt(toolset, e.cfg, extra)
	// Fresh slice: pc.managerTools may return the workspace toolset's
	// backing array, and appending complete_goal / block_goal in place
	// would hand them to every sub-agent.
	managerTools := append([]tool.BaseTool(nil), pc.managerTools(toolset)...)
	if pursuingGoal(th) {
		tid := rt.threadID
		managerTools = append(managerTools,
			CompleteGoalTool(func(summary string) (string, error) {
				return e.completeGoalJSON(tid, summary)
			}),
			BlockGoalTool(func(reason string) (string, error) {
				return e.blockGoalJSON(tid, reason)
			}),
		)
	}
	budget := e.cfg.Swarm.ManagerIterations()
	used := 0
	var res swarm.RunResult
	var runErr error
	restore, planted := rt.takeWorkerRestore()
	for {
		res, runErr = reg.RunWith(ctx, swarm.RunConfig{
			Instruction:     instruction,
			Messages:        messages,
			ManagerTools:    managerTools,
			MaxIterations:   budget,
			RestoreWorkers:  restore,
			FinishedWorkers: planted,
		}, swarm.Callback(acc.onNotify))
		restore, planted = nil, nil
		used += budget
		if !isMaxIterations(runErr) {
			return res, runErr
		}
		extend := e.cfg.Swarm.ManagerIterations()
		if !rt.waitToExtend(ctx, turn, used, extend) {
			if ctx.Err() != nil {
				return res, runErr
			}
			return res, limitStopError{Used: used}
		}
		next := messagesWithoutSystem(res.Transcript)
		if len(next) == 0 {
			next = messages
		}
		for _, m := range reg.TakePendingSteerMessages() {
			next = append(next, m)
		}
		messages = next
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
