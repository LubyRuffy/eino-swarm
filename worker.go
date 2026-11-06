package swarm

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// workerInstruction is what a sub-agent gets as its system prompt. The task
// is always there; WorkerPreamble rides in front when the host set one.
func (r *Registry) workerInstruction(task string) string {
	return workerInstructionWithConfig(r.WorkerConfiguration(), task)
}

// Host-owned durable identities sometimes need to reach worker tools. Keep
// that value transport explicit; inheriting the full manager context would
// also carry its model/role/accounting state and shorter execution lifetime.
func (r *Registry) workerContext(parent context.Context) context.Context {
	detached := context.Background()
	config := r.workerConfigurationForContext(parent)
	if config.WorkerContextValues != nil {
		detached = context.WithoutCancel(config.WorkerContextValues(parent, detached))
	}
	return withWorkerConfig(detached, config)
}

// startWorker runs h's goroutine. Spawn and resume both land here so a
// continued worker does not mint a second id — the roster row is the identity.
func (r *Registry) startWorker(ctx context.Context, h *Handle, task, instruction string,
	modelOpt ModelBuilder, seed string, extraTools []tool.BaseTool) {
	role, id := h.Role, h.ID
	config := r.workerConfigurationForContext(ctx)
	ctx = withWorkerConfig(ctx, config)

	timeout := r.agentTimeout()

	// Workers outlive one manager ReAct loop. A /goal or scheduled-wake
	// handoff cancels the manager's context; tying the worker to that context
	// would kill in-flight work the next turn still needs. Handle.Cancel /
	// Cleanup / Close still stop them.
	runCtx, cancel := context.WithCancel(r.workerContext(ctx))
	watchCtx, watchCancel := context.WithTimeout(runCtx, timeout)
	h.mu.Lock()
	h.workerConfig = &config
	h.configTag = config.Tag
	enabled := config.EnableDelegation
	h.delegationEnabled = &enabled
	h.cancel = cancel
	h.handoff.task, h.handoff.instruction = task, instruction
	initial := restoredWorkerMessages(task, workerReplayHistory(h.handoff.restoreHistory, instruction))
	h.handoff.restoreHistory = nil
	if h.handoff.cancelled || h.closeRequested {
		cancel()
	}
	done := h.done
	h.mu.Unlock()
	if seed != "" {
		// Seed before the goroutine runs. Pushing after Spawn returned is how
		// fork_context used to lose the race against the first model call.
		h.pushInbox(seed)
	}
	extraTools = r.workerTools(config, r.bindSendCaller(extraTools, id, role))

	identity := AgentIdentity{AgentID: id, Role: role, ParentID: h.ParentID, Generation: h.generation()}
	receiptScope := rand.Text()
	go func() {
		defer close(done)
		defer r.finishAgent(h)
		defer func() {
			r.mu.Lock()
			fh := r.finishHook
			r.mu.Unlock()
			if fh != nil {
				h.mu.Lock()
				res, e := h.result, h.err
				h.mu.Unlock()
				if !errors.Is(e, ErrHandoff) {
					fh(role, id, res, e, identity)
				}
			}
		}()
		r.acquireSlot()
		defer r.releaseSlot()
		defer cancel()
		defer watchCancel()

		acc := &streamAcc{reg: r, agentID: id, role: role, handle: h, rawEvents: true,
			workerGeneration: identity.Generation, parentID: identity.ParentID, toolReceiptScope: receiptScope}
		inj := &Injector{handle: h, histCap: r.historyCap(), reg: r, generation: identity.Generation,
			config: config, configured: true, toolReceiptScope: receiptScope}
		handlers := []adk.ChatModelAgentMiddleware{inj}
		if config.WorkerMiddlewares != nil {
			handlers = append(handlers, config.WorkerMiddlewares(identity)...)
		}
		agent, err := adk.NewChatModelAgent(watchCtx, &adk.ChatModelAgentConfig{
			Name:        id,
			Description: "spawned sub-agent " + role,
			Instruction: instruction,
			Model:       &generationModel{BaseChatModel: modelOpt(role, id), acc: acc},
			ToolsConfig: adk.ToolsConfig{
				ToolsNodeConfig: compose.ToolsNodeConfig{Tools: extraTools},
			},
			MaxIterations: r.turnsLimit(),
			Handlers:      handlers,
		})
		if err != nil {
			h.completeWorker("", fmt.Errorf("swarm: spawn %s: %w", id, err), timeout, watchCtx.Err(), runCtx.Err())
			return
		}
		runner := adk.NewRunner(watchCtx, adk.RunnerConfig{Agent: agent, EnableStreaming: true})
		iter := runner.Run(watchCtx, initial)
		final := ""
		for {
			ev, ok := iter.Next()
			if !ok {
				break
			}
			if ev == nil {
				continue
			}
			if ev.Err != nil {
				acc.interruptPending(ev.Err)
				h.completeWorker("", ev.Err, timeout, watchCtx.Err(), runCtx.Err())
				return
			}
			// Single reader of the worker stream: activity for wait_agents,
			// notifications, and the answer if this turn had no tool calls.
			o := ev.Output
			if o != nil && o.MessageOutput != nil &&
				o.MessageOutput.IsStreaming && o.MessageOutput.MessageStream != nil {
				answer, calls, drainErr := acc.drain(o.MessageOutput.MessageStream)
				if drainErr != nil {
					h.completeWorker("", drainErr, timeout, watchCtx.Err(), runCtx.Err())
					return
				}
				if len(calls) == 0 && strings.TrimSpace(answer) != "" {
					final = answer
				}
				continue
			}
			if r.OnEvent != nil {
				r.OnEvent(role, id, ev)
			}
			r.emitComplete(role, id, ev, workerNotificationOwner{identity: identity, receiptScope: receiptScope})
			if o != nil && o.MessageOutput != nil && !o.MessageOutput.IsStreaming &&
				o.MessageOutput.Message != nil && o.MessageOutput.Message.Role == schema.Assistant &&
				len(o.MessageOutput.Message.ToolCalls) == 0 {
				final = o.MessageOutput.Message.Content
			}
		}
		if watchCtx.Err() != nil {
			acc.interruptPending(watchCtx.Err())
		}
		h.completeWorker(final, nil, timeout, watchCtx.Err(), runCtx.Err())
	}()
}
