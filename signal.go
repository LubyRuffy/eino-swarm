package swarm

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// Callback is the lightweight UI form: a plain function receiving events.
// It is adapted to UI automatically when passed to Run.
type Callback func(Notification)

// UI is the plugin interface for frontends (bubbletea TUI, web UI, logger…).
// Everything the swarm knows about a frontend is this interface — the core
// never changes for a new frontend; implement UI and hand it to Run.
//
// Implementation contract:
//   - OnNotify is called sequentially from the swarm's event pump; heavy work
//     or rendering should be forwarded to the UI's own goroutine.
//   - Streaming semantics: each NotifyDelta/NotifyReasoningDelta carries the
//     FULL accumulated text so far ("a", "ab", "abc"…), so a UI renders by
//     overwriting the target pane — no buffering needed.
type UI interface {
	// OnNotify receives one event. Called from the swarm's event pump
	// goroutine(s); implementations must not block for long.
	OnNotify(n Notification)
	// OnDone is called once when the run finishes (final = manager answer,
	// err = failure).
	OnDone(final string, err error)
}

// CallbackUI adapts a plain function to the UI interface.
type cbUI struct{ fn Callback }

func (c cbUI) OnNotify(n Notification) { c.fn(n) }
func (c cbUI) OnDone(final string, err error) {
	if c.fn == nil {
		return
	}
	if err != nil {
		c.fn(Notification{Kind: NotifyError, AgentID: DefaultManagerID, Text: final, Err: err})
		return
	}
	c.fn(Notification{Kind: NotifyDone, AgentID: DefaultManagerID, Text: final})
}

// Notification kinds delivered to the UI.
type NotifyKind int

const (
	NotifyAgentMessage   NotifyKind = iota // an assistant message completed
	NotifySpawned                          // a worker was spawned (Text=role, AgentID set)
	NotifyFinished                         // a worker finished (Text=result; Err set on failure)
	NotifyToolCall                         // a tool call was issued (Text="name(args)")
	NotifyToolResult                       // a tool returned (Text=truncated result)
	NotifyTurn                             // an agent started a new model turn (Text="turn N")
	NotifyDelta                            // streamed answer text, accumulated so far
	NotifyReasoningDelta                   // streamed reasoning, accumulated so far
	NotifyDone                             // manager final answer; run complete
	NotifyError                            // fatal error; run failed
)

func (k NotifyKind) String() string {
	switch k {
	case NotifyAgentMessage:
		return "agent_message"
	case NotifySpawned:
		return "spawned"
	case NotifyFinished:
		return "finished"
	case NotifyToolCall:
		return "tool_call"
	case NotifyToolResult:
		return "tool_result"
	case NotifyTurn:
		return "turn"
	case NotifyDelta:
		return "delta"
	case NotifyReasoningDelta:
		return "reasoning_delta"
	case NotifyDone:
		return "done"
	case NotifyError:
		return "error"
	}
	return "unknown"
}

// DefaultManagerID is the agent ID of the top-level manager agent created by
// Run. It appears as Notification.AgentID for manager-scope events.
const DefaultManagerID = "manager"

// Notification is one UI event.
//
// Delta semantics: NotifyDelta/NotifyReasoningDelta carry the FULL
// accumulated text so far ("a", then "ab", then "abc") — a UI overwrites the
// bubble per event, no buffering needed. Reasoning and answer use separate
// accumulators (separate event kinds), so a "thinking" view and an answer
// view render independently.
type Notification struct {
	Kind    NotifyKind
	AgentID string // emitting agent (DefaultManagerID for the top level)
	Role    string
	Text    string // accumulated text / tool summary
	Err     error  // set for NotifyError
}

// msgPump accumulates one agent's streamed message. Reasoning (thinking) and
// answer text use separate accumulators.
type msgPump struct {
	mu        sync.Mutex
	sb        strings.Builder
	reasoning strings.Builder
	turn      int
}

func (p *msgPump) delta(chunk string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if chunk != "" {
		p.sb.WriteString(chunk)
		return p.sb.String(), true
	}
	return p.sb.String(), false
}

func (p *msgPump) reasoningDelta(chunk string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if chunk != "" {
		p.reasoning.WriteString(chunk)
		return p.reasoning.String(), true
	}
	return p.reasoning.String(), false
}

func (p *msgPump) nextTurn() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.turn++
	p.sb.Reset()
	p.reasoning.Reset()
	return p.turn
}

// streamCfg carries per-run streaming wiring.
type streamCfg struct {
	pumps map[string]*msgPump
	mu    sync.Mutex
}

func (s *streamCfg) pump(agentID string) *msgPump {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pumps[agentID]
	if !ok {
		p = &msgPump{}
		s.pumps[agentID] = p
	}
	return p
}

// summarize truncates tool output for notifications.
func summarize(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// emitEvent converts one agent event into UI notifications. Streaming events
// produce accumulated deltas; complete events produce canonical
// message/tool notifications.
func (r *Registry) emitEvent(cb Callback, role, agentID string, sc *streamCfg, ev *adk.AgentEvent) {
	if cb == nil || ev == nil || ev.Err != nil {
		return
	}
	o := ev.Output
	if o == nil || o.MessageOutput == nil {
		return
	}

	// worker events are drained by the swarm goroutine (single reader), which
	// then calls OnEvent with complete per-chunk data; nothing to do here.
	if o.MessageOutput.IsStreaming {
		return
	}

	// complete-message path
	msg := o.MessageOutput.Message
	if msg == nil {
		return
	}
	p := sc.pump(agentID)
	switch msg.Role {
	case schema.Assistant:
		// Worker synthetic events (from the Spawn drain loop) arrive per
		// chunk: reasoning chunks carry ReasoningContent, answer chunks carry
		// accumulated Content. Route them to the right accumulators WITHOUT
		// emitting turn/agent_message spam — the swarm goroutine emits a real
		// turn boundary via NotifyFinished/NotifyDelta already.
		if msg.ReasoningContent != "" && msg.Content == "" {
			if acc, changed := p.reasoningDelta(msg.ReasoningContent); changed {
				cb(Notification{Kind: NotifyReasoningDelta, AgentID: agentID, Role: role, Text: acc})
			}
			return
		}
		if msg.ToolCalls == nil && msg.Content != "" {
			// accumulated answer chunk — emit as delta, not as final message
			if acc, changed := p.delta(msg.Content); changed {
				cb(Notification{Kind: NotifyDelta, AgentID: agentID, Role: role, Text: acc})
			}
			return
		}
		// real turn boundary (manager path or a worker's merged tool calls)
		turnN := p.nextTurn()
		cb(Notification{Kind: NotifyTurn, AgentID: agentID, Role: role,
			Text: fmt.Sprintf("turn %d", turnN)})
		if c := strings.TrimSpace(msg.Content); c != "" {
			cb(Notification{Kind: NotifyAgentMessage, AgentID: agentID, Role: role, Text: c})
		}
		for _, tc := range mergeStreamedToolCalls(msg.ToolCalls) {
			cb(Notification{Kind: NotifyToolCall, AgentID: agentID, Role: role,
				Text: tc.Function.Name + "(" + tc.Function.Arguments + ")"})
		}
	case schema.Tool:
		cb(Notification{Kind: NotifyToolResult, AgentID: agentID, Role: role,
			Text: summarize(msg.Content, 160)})
	}
}

// mergeStreamedToolCalls merges incrementally-streamed tool call chunks into
// complete calls (keyed by Index when set, else by ID). Returns one entry per
// actual call with fully concatenated name/arguments, in first-appearance order.
func mergeStreamedToolCalls(chunks []schema.ToolCall) []schema.ToolCall {
	byKey := map[string]*schema.ToolCall{}
	order := make([]string, 0, len(chunks))
	for _, c := range chunks {
		key := c.ID
		if c.Index != nil {
			key = fmt.Sprintf("i%d", *c.Index)
		}
		e, ok := byKey[key]
		if !ok {
			cp := c
			byKey[key] = &cp
			e = &cp
			order = append(order, key)
		}
		// Some providers resend the full name in every chunk; take the first
		// non-empty instead of concatenating (args remain concatenated).
		if e.Function.Name == "" && c.Function.Name != "" {
			e.Function.Name = c.Function.Name
		}
		if c.Function.Arguments != "" {
			e.Function.Arguments += c.Function.Arguments
		}
		if c.ID != "" && e.ID == "" {
			e.ID = c.ID
		}
	}
	out := make([]schema.ToolCall, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	return out
}

// Run is the entire integration surface: it wires the manager agent (with the
// four lifecycle tools and fork_context middleware), runs EVERY agent in
// streaming mode, converts Ctrl+C into context cancellation that cascades to
// every spawned agent, and drives ui with lifecycle + stream events. It
// blocks until the run ends and returns the manager's final answer.
//
// ui may be:
//   - nil                        (headless run)
//   - a Callback func(Notification) (CLI printing, tests)
//   - any UI implementation      (bubbletea TUI, web UI, logger…)
//
// Run blocks until the run ends and returns the manager's final answer.
//
// ui may be:
//   - nil                                  (headless run)
//   - a Callback / func(Notification)      (CLI printing, tests)
//   - any UI implementation                (bubbletea TUI, web UI, logger…)
func (r *Registry) Run(ctx context.Context, task string, ui any) (string, error) {
	return r.runUI(ctx, task, ui)
}

// RunWithCallback is Run for plain function UIs.
func (r *Registry) RunWithCallback(ctx context.Context, task string, cb Callback) (string, error) {
	return r.runUI(ctx, task, cb)
}

// asUI normalizes the three accepted UI forms (nil / Callback / UI) into one
// internal sink: an OnNotify func plus an OnDone hook.
func asUI(v any) (cb Callback, done func(final string, err error)) {
	switch u := v.(type) {
	case nil:
		return nil, nil
	case Callback:
		return u, func(final string, err error) { u(doneNotif(final, err)) }
	case func(Notification):
		return Callback(u), func(final string, err error) { u(doneNotif(final, err)) }
	case UI:
		return u.OnNotify, u.OnDone
	default:
		// unknown type: accept nothing silently — callers pass the wrong
		// type only by mistake; a compile-time UI/Callback is expected.
		return nil, nil
	}
}

func doneNotif(final string, err error) Notification {
	if err != nil {
		return Notification{Kind: NotifyError, AgentID: DefaultManagerID, Text: final, Err: err}
	}
	return Notification{Kind: NotifyDone, AgentID: DefaultManagerID, Text: final}
}

func (r *Registry) runUI(ctx context.Context, task string, ui any) (string, error) {
	cb, done := asUI(ui)
	final, err := r.run(ctx, task, cb)
	if done != nil {
		done(final, err)
	}
	return final, err
}

func (r *Registry) run(ctx context.Context, task string, cb Callback) (string, error) {
	if r.ModelBuilder == nil {
		return "", fmt.Errorf("swarm: ModelBuilder is required")
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	sc := &streamCfg{pumps: map[string]*msgPump{}}

	shared := r.ModelBuilder(DefaultManagerID, DefaultManagerID)
	mgr, err := adk.NewChatModelAgent(ctx, r.ManagerConfig(
		DefaultManagerID, "swarm manager", shared,
		WithInstruction(task),
	))
	if err != nil {
		return "", err
	}

	// OnEvent fan-out: worker (Spawn path) events -> notifications.
	prev := r.OnEvent
	r.OnEvent = func(role, agentID string, ev *adk.AgentEvent) {
		if prev != nil {
			prev(role, agentID, ev)
		}
		r.emitEvent(cb, role, agentID, sc, ev)
	}
	defer func() { r.OnEvent = prev }()

	// spawn/finish notifications via Spawn/SpawnForked hook points
	baseSpawn := r.spawnHook
	r.spawnHook = func(role, agentID string) {
		if baseSpawn != nil {
			baseSpawn(role, agentID)
		}
		if cb != nil {
			cb(Notification{Kind: NotifySpawned, AgentID: agentID, Role: role, Text: role})
		}
	}
	baseFinish := r.finishHook
	r.finishHook = func(role, agentID, result string, err error) {
		if baseFinish != nil {
			baseFinish(role, agentID, result, err)
		}
		if cb != nil {
			cb(Notification{Kind: NotifyFinished, AgentID: agentID, Role: role, Text: result, Err: err})
		}
	}
	defer func() { r.spawnHook, r.finishHook = nil, nil }()

	// manager runner in STREAMING mode — no per-agent blocking generation that
	// can hit gateway idle timeouts on long answers.
	//
	// In streaming mode eino delivers each assistant message ONLY as a
	// MessageStream; there is no separate complete-message event. So: drain
	// each stream into the pump (emitting accumulated deltas), and when a
	// manager stream ends without tool calls the accumulated text is the
	// final answer.
	var final string
	sawFinal := false
	iter := adk.NewRunner(ctx, adk.RunnerConfig{Agent: mgr, EnableStreaming: true}).
		Run(ctx, []adk.Message{schema.UserMessage(task)})
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev == nil {
			continue
		}
		if ev.Err != nil {
			if cb != nil {
				cb(Notification{Kind: NotifyError, AgentID: DefaultManagerID, Err: ev.Err})
			}
			return "", ev.Err
		}
		o := ev.Output
		if o == nil || o.MessageOutput == nil {
			continue
		}
		if o.MessageOutput.IsStreaming && o.MessageOutput.MessageStream != nil {
			p := sc.pump(DefaultManagerID)
			var toolCalls []schema.ToolCall
			for {
				chunk, err := o.MessageOutput.MessageStream.Recv()
				if err != nil {
					break // io.EOF or canceled; accumulation complete
				}
				if chunk == nil {
					continue
				}
				if rc := chunk.ReasoningContent; rc != "" {
					if acc, changed := p.reasoningDelta(rc); changed && cb != nil {
						cb(Notification{Kind: NotifyReasoningDelta, AgentID: DefaultManagerID, Text: acc})
					}
				}
				if c := chunk.Content; c != "" {
					if acc, changed := p.delta(c); changed && cb != nil {
						cb(Notification{Kind: NotifyDelta, AgentID: DefaultManagerID, Text: acc})
					}
				}
				if len(chunk.ToolCalls) > 0 {
					toolCalls = append(toolCalls, chunk.ToolCalls...)
				}
			}
			// turn boundary: flush pump. Tool calls => turn continues;
			// otherwise this stream was the final answer.
			p.mu.Lock()
			acc := p.sb.String()
			p.sb.Reset()
			p.mu.Unlock()
			if len(toolCalls) > 0 {
				if cb != nil {
					for _, tc := range mergeStreamedToolCalls(toolCalls) {
						cb(Notification{Kind: NotifyToolCall, AgentID: DefaultManagerID,
							Text: tc.Function.Name + "(" + tc.Function.Arguments + ")"})
					}
				}
				continue
			}
			if strings.TrimSpace(acc) != "" {
				final = acc
				sawFinal = true
				if cb != nil {
					cb(Notification{Kind: NotifyAgentMessage, AgentID: DefaultManagerID, Text: final})
				}
			}
		}
	}
	if cb != nil && sawFinal {
		cb(Notification{Kind: NotifyDone, AgentID: DefaultManagerID, Text: final})
	}
	return final, nil
}
