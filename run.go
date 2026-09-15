// run.go — the manager run loops. Run is the one-shot convenience surface
// (single task string, SIGINT handling); RunWith is the surface a long-lived
// host uses: it takes a conversation, extra manager tools, and returns the
// resulting transcript so the next turn can continue where this one stopped.
package swarm

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// RunConfig describes one manager run.
//
// Instruction is the manager's system prompt and Messages is the conversation
// to run it on — typically the whole prior transcript plus the new user
// message. That split is what makes multi-turn possible: Run (one-shot) puts
// the task in both, a chat host puts a stable prompt in Instruction and grows
// Messages turn by turn.
type RunConfig struct {
	// Instruction is the manager's system prompt. Required in practice; an
	// empty instruction means the model gets no system message.
	Instruction string

	// Messages is the conversation to run. When empty, Task is used to
	// synthesize a single user message.
	Messages []adk.Message

	// Task is a convenience alternative to Messages for one-shot runs.
	Task string

	// Model overrides the manager's chat model. When nil the registry's
	// ModelBuilder builds it as DefaultManagerID.
	Model model.BaseChatModel

	// ManagerTools are registered on the manager in addition to the five
	// lifecycle tools (spawn/send/wait/close/resume).
	ManagerTools []tool.BaseTool

	// ManagerMiddlewares are appended after the swarm's own manager
	// middleware (history recorder + steering injector).
	ManagerMiddlewares []adk.ChatModelAgentMiddleware

	// MaxIterations caps the manager's ReAct loop (<=0 keeps the default).
	MaxIterations int
}

// RunResult is what one manager run produced.
type RunResult struct {
	// Final is the manager's last answer (empty when the run failed before
	// producing one).
	Final string

	// Transcript is the manager's conversation as the model saw it, including
	// the leading system message, every tool call and tool result, and the
	// final assistant message. Feed it back as RunConfig.Messages (minus the
	// system message, which Instruction regenerates) to continue the session.
	Transcript []adk.Message
}

// Run is the one-shot surface: it wires the manager agent (five lifecycle
// tools + fork_context/steering middleware), runs every agent in streaming
// mode, converts Ctrl+C into context cancellation that cascades to every
// spawned agent, and drives ui with lifecycle + stream events. It blocks until
// the run ends and returns the manager's final answer.
//
// ui may be:
//   - nil                              (headless run)
//   - a Callback / func(Notification)  (CLI printing, tests)
//   - any UI implementation            (bubbletea TUI, web UI, logger…)
func (r *Registry) Run(ctx context.Context, task string, ui any) (string, error) {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	res, err := r.RunWith(ctx, RunConfig{Instruction: task, Task: task}, ui)
	return res.Final, err
}

// RunWithCallback is Run for plain function UIs.
func (r *Registry) RunWithCallback(ctx context.Context, task string, cb Callback) (string, error) {
	return r.Run(ctx, task, cb)
}

// RunWith runs the manager over cfg and returns its final answer together with
// the full transcript. Unlike Run it installs no signal handler — the caller
// owns ctx, which is what a server wants (one context per conversation turn,
// canceled on interrupt or client disconnect).
func (r *Registry) RunWith(ctx context.Context, cfg RunConfig, ui any) (RunResult, error) {
	u := asUI(ui)
	var cb Callback
	if u != nil {
		cb = u.OnNotify
	}
	res, err := r.exec(ctx, cfg, cb)
	if u != nil {
		u.OnDone(res.Final, err)
	}
	return res, err
}

func (r *Registry) exec(ctx context.Context, cfg RunConfig, cb Callback) (RunResult, error) {
	msgs := cfg.Messages
	if len(msgs) == 0 {
		if strings.TrimSpace(cfg.Task) == "" {
			return RunResult{}, fmt.Errorf("swarm: RunConfig needs Messages or Task")
		}
		msgs = []adk.Message{schema.UserMessage(cfg.Task)}
	}

	managerModel := cfg.Model
	if managerModel == nil {
		if r.ModelBuilder == nil {
			return RunResult{}, fmt.Errorf("swarm: ModelBuilder is required")
		}
		managerModel = r.ModelBuilder(DefaultManagerID, DefaultManagerID)
	}

	// The sink is how worker goroutines reach the UI; install it before the
	// manager can spawn anything and restore it on the way out.
	prevSink := r.setSink(cb)
	defer r.setSink(prevSink)

	r.resetHistory()

	opts := []ManagerOption{WithInstruction(cfg.Instruction)}
	if len(cfg.ManagerTools) > 0 {
		opts = append(opts, WithManagerTool(cfg.ManagerTools...))
	}
	if len(cfg.ManagerMiddlewares) > 0 {
		opts = append(opts, WithManagerHandler(cfg.ManagerMiddlewares...))
	}
	if cfg.MaxIterations > 0 {
		opts = append(opts, WithMaxIterations(cfg.MaxIterations))
	}

	mgr, err := adk.NewChatModelAgent(ctx, r.ManagerConfig(
		DefaultManagerID, "swarm manager", managerModel, opts...))
	if err != nil {
		return RunResult{}, err
	}

	// spawn/finish notifications ride the Spawn/SpawnForked hook points, so a
	// worker appears in the UI the moment the manager asks for it.
	baseSpawn, baseFinish := r.setHooks(
		func(role, agentID string) {
			r.emit(Notification{Kind: NotifySpawned, AgentID: agentID, Role: role, Text: role})
		},
		func(role, agentID, result string, err error) {
			r.emit(Notification{Kind: NotifyFinished, AgentID: agentID, Role: role, Text: result, Err: err})
		},
	)
	defer r.setHooks(baseSpawn, baseFinish)

	// The manager runs in STREAMING mode too: no blocking generation that can
	// hit a gateway idle timeout on a long answer. In streaming mode eino
	// delivers each assistant message ONLY as a MessageStream, so the final
	// answer is the last stream that ended without tool calls.
	acc := &streamAcc{reg: r, agentID: DefaultManagerID, role: DefaultManagerID}
	final := ""
	iter := adk.NewRunner(ctx, adk.RunnerConfig{Agent: mgr, EnableStreaming: true}).Run(ctx, msgs)
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev == nil {
			continue
		}
		if ev.Err != nil {
			return RunResult{Final: final, Transcript: r.historySnapshot()}, ev.Err
		}
		o := ev.Output
		if o == nil || o.MessageOutput == nil {
			continue
		}
		if o.MessageOutput.IsStreaming && o.MessageOutput.MessageStream != nil {
			answer, calls := acc.drain(o.MessageOutput.MessageStream)
			if len(calls) == 0 && strings.TrimSpace(answer) != "" {
				final = answer
			}
			continue
		}
		r.emitComplete(DefaultManagerID, DefaultManagerID, ev)
		if msg := o.MessageOutput.Message; msg != nil &&
			msg.Role == schema.Assistant && len(msg.ToolCalls) == 0 &&
			strings.TrimSpace(msg.Content) != "" {
			final = msg.Content
		}
	}
	return RunResult{Final: final, Transcript: r.historySnapshot()}, nil
}

func (r *Registry) resetHistory() {
	r.mu.Lock()
	r.hist = nil
	r.mgrInbox = nil
	r.mu.Unlock()
}

// setHooks swaps the spawn/finish hooks atomically and returns the previous
// pair, so exec can restore them with a single deferred call.
func (r *Registry) setHooks(spawn func(role, agentID string),
	finish func(role, agentID, result string, err error),
) (func(role, agentID string), func(role, agentID, result string, err error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ps, pf := r.spawnHook, r.finishHook
	r.spawnHook, r.finishHook = spawn, finish
	return ps, pf
}
