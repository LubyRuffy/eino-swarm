package swarm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// workerInstruction is what a sub-agent gets as its system prompt. The task
// is always there; WorkerPreamble rides in front when the host set one.
func (r *Registry) workerInstruction(task string) string {
	p := strings.TrimSpace(r.WorkerPreamble)
	if p == "" {
		return task
	}
	return p + "\n\n" + task
}

// startWorker runs h's goroutine. Spawn and resume both land here so a
// continued worker does not mint a second id — the roster row is the identity.
func (r *Registry) startWorker(ctx context.Context, h *Handle, task, instruction string,
	modelOpt ModelBuilder, seed string, extraTools []tool.BaseTool) {
	role, id := h.Role, h.ID

	max := r.MaxConcurrent
	if max <= 0 {
		max = 8
	}
	sem := r.sem(max)
	timeout := r.AgentTimeout
	if timeout <= 0 {
		timeout = DefaultAgentTimeout
	}

	// Workers outlive one manager ReAct loop. A /goal session yield cancels
	// the manager's context; tying the worker to that context would kill
	// in-flight work the next session still needs. Handle.Cancel / Cleanup /
	// Close still stop them.
	runCtx, cancel := context.WithCancel(context.Background())
	watchCtx, watchCancel := context.WithTimeout(runCtx, timeout)
	h.mu.Lock()
	h.cancel = cancel
	done := h.done
	h.mu.Unlock()
	if seed != "" {
		// Seed before the goroutine runs. Pushing after Spawn returned is how
		// fork_context used to lose the race against the first model call.
		h.pushInbox(seed)
	}
	extraTools = r.bindSendCaller(extraTools, id, role)

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
				fh(role, id, res, e)
			}
		}()
		sem <- struct{}{}
		defer func() { <-sem }()
		defer cancel()
		defer watchCancel()

		inj := &Injector{handle: h, histCap: r.historyCap(), reg: r}
		agent, err := adk.NewChatModelAgent(watchCtx, &adk.ChatModelAgentConfig{
			Name:        id,
			Description: "spawned sub-agent " + role,
			Instruction: instruction,
			Model:       modelOpt(role, id),
			ToolsConfig: adk.ToolsConfig{
				ToolsNodeConfig: compose.ToolsNodeConfig{Tools: extraTools},
			},
			MaxIterations: r.turnsLimit(),
			Handlers:      []adk.ChatModelAgentMiddleware{inj},
		})
		if err != nil {
			h.mu.Lock()
			h.err = fmt.Errorf("swarm: spawn %s: %w", id, err)
			h.finished = time.Now()
			h.mu.Unlock()
			return
		}
		runner := adk.NewRunner(watchCtx, adk.RunnerConfig{Agent: agent, EnableStreaming: true})
		iter := runner.Run(watchCtx, []adk.Message{schema.UserMessage(task)})
		acc := &streamAcc{reg: r, agentID: id, role: role, handle: h, rawEvents: true}
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
				h.mu.Lock()
				h.err = ev.Err
				switch {
				case watchCtx.Err() != nil && runCtx.Err() == nil:
					h.err = fmt.Errorf("swarm: agent %s exceeded timeout %v: %w", id, timeout, ev.Err)
				case runCtx.Err() != nil && ctx.Err() == nil:
					h.err = fmt.Errorf("swarm: agent %s cancelled: %w", id, ev.Err)
				}
				h.finished = time.Now()
				h.mu.Unlock()
				return
			}
			// Single reader of the worker stream: activity for wait_agents,
			// notifications, and the answer if this turn had no tool calls.
			o := ev.Output
			if o != nil && o.MessageOutput != nil &&
				o.MessageOutput.IsStreaming && o.MessageOutput.MessageStream != nil {
				answer, calls := acc.drain(o.MessageOutput.MessageStream)
				if len(calls) == 0 && strings.TrimSpace(answer) != "" {
					final = answer
				}
				continue
			}
			if r.OnEvent != nil {
				r.OnEvent(role, id, ev)
			}
			r.emitComplete(role, id, ev)
			if o != nil && o.MessageOutput != nil && !o.MessageOutput.IsStreaming &&
				o.MessageOutput.Message != nil && o.MessageOutput.Message.Role == schema.Assistant &&
				len(o.MessageOutput.Message.ToolCalls) == 0 {
				final = o.MessageOutput.Message.Content
			}
		}
		h.mu.Lock()
		h.result = final
		h.finished = time.Now()
		h.mu.Unlock()
	}()
}
