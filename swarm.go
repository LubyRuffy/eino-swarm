// Package swarm implements Codex-style multi-agent orchestration on Eino ADK:
// asynchronous sub-agent spawning, mid-flight steering, waiting/collecting
// results, and cancellation — none of which adk.NewAgentTool supports (it is
// synchronous and offers no communication channel between agents).
//
// Core idea: instead of registering sub-agents as blocking "executor" tools,
// the host registers five lifecycle tools backed by one shared Registry:
//
//	spawn_agent(role, task) -> {"agent_id": "..."}   returns immediately
//	send_message(agent_id, text)                     queued for the target's next turn
//	wait_agents(agent_ids, timeout_s) -> results     returns when the next one finishes
//	close_agent(agent_id)                            cancels the agent
//	resume_agent(agent_id, task) -> {"agent_id": "...", "resumed_from": "..."}
//	                                                 same worker, that finished worker's conversation
//
// Sub-agents are regular adk.ChatModelAgents running in their own goroutine;
// if they are given the swarm's send_message tool too, agents can message each
// other (mesh), not just receive from the manager.
//
// # Lifecycle / resource release
//
// Sub-agents run on their own cancellable context, not the Spawn caller's.
// A /goal session yield cancels the manager without killing in-flight
// workers; Handle.Cancel, Registry.Cleanup, Registry.Close, and AgentTimeout
// still stop them. Two further guards cover the case where nobody ever calls
// close_agent:
//
//   - AgentTimeout: a watchdog context caps each sub-agent's lifetime
//     (default 10m via DefaultAgentTimeout);
//   - MaxTurns: caps ReAct iterations so a broken/looping model ends with
//     eino's ErrExceedMaxIterations instead of spinning forever.
//
// Finished handles remain queryable (Result/Elapsed) but leave the Registry
// map on the next Stats call, so a long-lived session cannot grow it without
// bound. Registry.Close cancels everything and rejects further spawns.
package swarm

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// DefaultAgentTimeout is the watchdog applied to each sub-agent when
// Registry.AgentTimeout is not set.
const DefaultAgentTimeout = 10 * time.Minute

// Handle is the public handle to one running sub-agent.
type Handle struct {
	ID    string
	Role  string
	done  chan struct{}
	mu    sync.Mutex
	inbox []string
	// leftover steering that never hit a model call; wait_agents surfaces it
	undelivered []string
	history     []adk.Message // cloned each model call; resume_agent's seed
	resumedFrom string
	gen         int // bumps on resume so Result/Done cannot see the previous life
	cancel      context.CancelFunc
	result      string
	err         error
	spawned     time.Time
	finished    time.Time
	activity    string // latest streamed reasoning/answer tail (for progress)
}

// Activity returns the agent's most recent streamed text tail (thinking or
// answer), or its last error — a live progress hint for wait_agents.
func (h *Handle) Activity() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.activity
}

func (h *Handle) setActivity(s string) {
	h.mu.Lock()
	h.activity = s
	h.mu.Unlock()
}

// Result returns the agent's final message and error once it has finished.
// finished is false while the agent is still running.
func (h *Handle) Result() (result string, err error, finished bool) {
	h.mu.Lock()
	ch := h.done
	gen := h.gen
	h.mu.Unlock()
	select {
	case <-ch:
		h.mu.Lock()
		defer h.mu.Unlock()
		// Resume swapped the channel; this close belongs to the previous life.
		if h.gen != gen {
			return "", nil, false
		}
		return h.result, h.err, true
	default:
		return "", nil, false
	}
}

// Done exposes the completion channel for external select loops.
func (h *Handle) Done() <-chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.done
}

// Elapsed returns how long the agent ran (valid only after completion).
func (h *Handle) Elapsed() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.finished.Sub(h.spawned)
}

// Cancel force-cancels this agent (same effect as close_agent on it).
func (h *Handle) Cancel() {
	h.mu.Lock()
	cancel := h.cancel
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (h *Handle) pushInbox(text string) {
	h.mu.Lock()
	h.inbox = append(h.inbox, text)
	h.mu.Unlock()
}

func (h *Handle) drainInbox() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.inbox) == 0 {
		return nil
	}
	out := h.inbox
	h.inbox = nil
	return out
}

// Registry tracks spawned sub-agents. It is safe for concurrent use: eino's
// ToolsNode runs each tool call in its own goroutine when the host model
// issues multiple calls in one message.
type Registry struct {
	mu     sync.Mutex
	agents map[string]*Handle
	seq    int
	closed bool

	semOnce sync.Once
	semCh   chan struct{}

	// hist snapshots the manager conversation for spawn_agent(fork_context)
	// and for RunResult.Transcript. ManagerMiddleware keeps it fresh.
	hist []adk.Message

	// mgrInbox holds steering messages for the manager agent, drained by
	// ManagerMiddleware at the manager's next turn boundary.
	mgrInbox []*schema.Message

	// notify is the semantic notification sink installed for the duration of
	// one Run/RunWith. Worker goroutines emit through it.
	notify Callback

	// MaxConcurrent caps simultaneously running sub-agents (<=0 means 8).
	MaxConcurrent int

	// AgentTimeout caps each sub-agent's lifetime with a watchdog context.
	// <=0 means DefaultAgentTimeout. The cancel/timeout cause is recorded in
	// the handle's error either way.
	AgentTimeout time.Duration

	// MaxTurns caps each sub-agent's ReAct iterations (<=0 means eino
	// default 20). A broken model that keeps calling tools ends with
	// ErrExceedMaxIterations instead of spinning forever.
	MaxTurns int

	// ManagerMaxIterations caps the manager's ReAct loop when RunConfig
	// does not set MaxIterations (<=0 keeps eino's own default).
	ManagerMaxIterations int

	// ModelBuilder builds each sub-agent's chat model. Return one shared
	// instance to reuse a single client/endpoint across the swarm.
	ModelBuilder ModelBuilder

	// SubAgentTools are registered on every spawned sub-agent in addition to
	// the swarm's send_message tool (e.g. web_search/web_fetch for workers).
	SubAgentTools []tool.BaseTool

	// WorkerPreamble is prepended to every sub-agent's Instruction. The task
	// stays the user message. Empty keeps the previous behaviour: Instruction
	// is the task. Hosts use this for facts workers cannot see in the manager
	// prompt — OS, shell, date — so they do not invent the wrong userland.
	WorkerPreamble string

	// OnEvent, if set, receives every inner agent event (streamed lifecycle,
	// tool calls, assistant output) with the spawning role attached.
	OnEvent func(role, agentID string, ev *adk.AgentEvent)

	// ArchiveLimit caps finished conversations kept for resume_agent
	// (<=0 means defaultArchiveSize). Independent of Stats(), which still
	// forgets live handles.
	ArchiveLimit int

	// HistoryLimit caps messages snapshotted per worker for resume_agent
	// (<=0 means defaultHistoryLimit).
	HistoryLimit int

	// ToolOutputBinder, if set, wraps the context of every invokable tool
	// call so a host can stream output as NotifyToolDelta. emit receives
	// the accumulated text so far; the binder chooses the payload shape.
	ToolOutputBinder func(ctx context.Context, emit func(string), toolName, callID string) context.Context

	past    map[string]*agentPast
	pastIDs []string

	// roleMu serializes spawn_agent per role so two same-role calls cannot
	// race into twins. Different roles still start in parallel — a global
	// lock would deadlock eino's ToolsNode (one spawn's hook waiting for
	// the sibling call that is blocked on the same mutex).
	roleMu map[string]*sync.Mutex

	// internal hooks: invoked by Spawn on registration and by the agent
	// goroutine on completion; Run wires these into Notifications.
	spawnHook  func(role, agentID, instruction string)
	finishHook func(role, agentID, result string, err error)
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{agents: map[string]*Handle{}, past: map[string]*agentPast{}}
}

func (r *Registry) get(id string) (*Handle, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, false
	}
	h, ok := r.agents[id]
	return h, ok
}

// SetHistory seeds the conversation snapshot that spawn_agent(fork_context)
// hands to newly spawned agents — the Codex fork_turns equivalent. Installed
// automatically via ManagerMiddleware; can also be seeded manually.
func (r *Registry) SetHistory(msgs []adk.Message) {
	cp := make([]adk.Message, len(msgs))
	copy(cp, msgs)
	r.mu.Lock()
	defer r.mu.Unlock()
	// eino's ExceedMaxIterations is a ChatModel preprocessor failure.
	// BeforeModel still runs and would replace hist with a snapshot that
	// dropped the wrap-appended tool results of the round that just
	// finished. Keep those results so the next RunWith does not re-issue
	// the same calls.
	have := make(map[string]bool, len(cp)+len(r.hist))
	for _, m := range cp {
		if m != nil && m.Role == schema.Tool && strings.TrimSpace(m.ToolCallID) != "" {
			have[m.ToolCallID] = true
		}
	}
	for _, m := range r.hist {
		if m == nil || m.Role != schema.Tool {
			continue
		}
		id := strings.TrimSpace(m.ToolCallID)
		if id == "" || have[id] {
			continue
		}
		cp = append(cp, m)
		have[id] = true
	}
	r.hist = cp
}

func (r *Registry) historySnapshot() []adk.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]adk.Message, len(r.hist))
	copy(out, r.hist)
	return out
}

// SteerManager queues a steering message for the manager agent. Like
// send_message to a worker it lands at the manager's next turn boundary,
// never interrupting an in-flight model call or tool execution. It reports
// false when the registry is closed (nothing is running to steer).
func (r *Registry) SteerManager(text string) bool {
	return r.SteerManagerMessage(schema.UserMessage("[steer] " + text))
}

// SteerManagerMessage queues an already-built user message, which is how a
// pasted image rides along with the steering text. False when nothing is
// running to steer.
func (r *Registry) SteerManagerMessage(msg *schema.Message) bool {
	if msg == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	r.mgrInbox = append(r.mgrInbox, msg)
	return true
}

// TakePendingSteers removes and returns steering messages that were queued but
// never delivered, which happens when a steer lands after the manager's last
// model call. The caller decides what to do with them; dropping them silently
// would lose something the user typed. Captions only — use
// TakePendingSteerMessages when the queued item may carry images.
func (r *Registry) TakePendingSteers() []string {
	msgs := r.drainManagerInbox()
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, strings.TrimSpace(strings.TrimPrefix(steerCaption(m), "[steer]")))
	}
	return out
}

// TakePendingSteerMessages is TakePendingSteers with the full messages, so a
// pasted image is not stripped off when a late steer becomes its own turn.
func (r *Registry) TakePendingSteerMessages() []*schema.Message {
	return r.drainManagerInbox()
}

func (r *Registry) drainManagerInbox() []*schema.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.mgrInbox) == 0 {
		return nil
	}
	out := r.mgrInbox
	r.mgrInbox = nil
	return out
}

func steerCaption(m *schema.Message) string {
	if m == nil {
		return ""
	}
	if t := strings.TrimSpace(m.Content); t != "" {
		return t
	}
	for _, p := range m.UserInputMultiContent {
		if p.Type == schema.ChatMessagePartTypeText {
			if t := strings.TrimSpace(p.Text); t != "" {
				return t
			}
		}
	}
	return ""
}

// historyRecorder keeps the registry's snapshot of the manager conversation
// fresh each turn (so spawned agents can inherit it and RunWith can return it
// as a transcript) and drains queued manager steering messages.
type historyRecorder struct {
	adk.BaseChatModelAgentMiddleware
	reg *Registry
}

func (m *historyRecorder) BeforeModelRewriteState(ctx context.Context,
	state *adk.ChatModelAgentState, mc *adk.TypedModelContext[*schema.Message],
) (context.Context, *adk.ChatModelAgentState, error) {
	for _, msg := range m.reg.drainManagerInbox() {
		state.Messages = append(state.Messages, msg)
	}
	m.reg.SetHistory(state.Messages)
	return ctx, state, nil
}

// AfterModelRewriteState re-snapshots the conversation once the model result
// has been appended, so the recorded history includes the assistant message
// the manager just produced — that is what makes the snapshot usable as the
// run's full transcript.
func (m *historyRecorder) AfterModelRewriteState(ctx context.Context,
	state *adk.ChatModelAgentState, mc *adk.TypedModelContext[*schema.Message],
) (context.Context, *adk.ChatModelAgentState, error) {
	m.reg.SetHistory(state.Messages)
	return ctx, state, nil
}

// WrapInvokableToolCall snapshots each tool result as it lands. eino's
// ExceedMaxIterations is a ChatModel preprocessor failure: AfterModel has
// the assistant call, but the next BeforeModel never runs, so without this
// the returned transcript would drop the results and a /goal slice would
// re-issue the same calls.
func (m *historyRecorder) WrapInvokableToolCall(ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint, tc *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		ctx = m.reg.bindToolOutput(ctx, DefaultManagerID, "manager", tc)
		out, err := endpoint(ctx, argumentsInJSON, opts...)
		m.reg.appendHistoryToolResult(tc, out, err)
		return out, err
	}, nil
}

func (r *Registry) bindToolOutput(ctx context.Context, agentID, role string, tc *adk.ToolContext) context.Context {
	if r == nil || r.ToolOutputBinder == nil || tc == nil {
		return ctx
	}
	return r.ToolOutputBinder(ctx, func(text string) {
		r.emit(Notification{
			Kind:       NotifyToolDelta,
			AgentID:    agentID,
			Role:       role,
			Text:       text,
			ToolCallID: tc.CallID,
		})
	}, tc.Name, tc.CallID)
}

func (m *historyRecorder) WrapStreamableToolCall(ctx context.Context,
	endpoint adk.StreamableToolCallEndpoint, tc *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		sr, err := endpoint(ctx, argumentsInJSON, opts...)
		if err != nil {
			return nil, err
		}
		var b strings.Builder
		for {
			chunk, recvErr := sr.Recv()
			if recvErr != nil {
				break
			}
			b.WriteString(chunk)
		}
		m.reg.appendHistoryToolResult(tc, b.String(), nil)
		return schema.StreamReaderFromArray([]string{b.String()}), nil
	}, nil
}

func (r *Registry) appendHistoryToolResult(tc *adk.ToolContext, content string, runErr error) {
	if tc == nil || strings.TrimSpace(tc.CallID) == "" {
		return
	}
	text := content
	if runErr != nil && strings.TrimSpace(text) == "" {
		text = runErr.Error()
	}
	msg := schema.ToolMessage(text, tc.CallID, schema.WithToolName(tc.Name))
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.hist {
		if m != nil && m.Role == schema.Tool && m.ToolCallID == tc.CallID {
			return
		}
	}
	r.hist = append(r.hist, msg)
}

// ManagerMiddleware returns the middleware to install on the manager agent
// (adk.ChatModelAgentConfig.Handlers) so spawn_agent(fork_context=true) can
// inherit the manager's conversation so far — Codex's fork_turns — and so
// SteerManager messages are delivered at turn boundaries.
func (r *Registry) ManagerMiddleware() adk.ChatModelAgentMiddleware {
	return &historyRecorder{reg: r}
}

// Spawn starts a sub-agent in the background and returns its handle
// immediately. The agent's context derives from ctx: if the Spawn caller is
// extraTools are appended to the sub-agent's toolset on top of
// Registry.SubAgentTools and the mesh send_message tool. The worker does not
// die with ctx: a /goal session yield cancels the manager without killing
// in-flight sub-agents. Handle.Cancel, Cleanup, Close, and AgentTimeout still
// stop them.
func (r *Registry) Spawn(ctx context.Context, role, task string,
	modelOpt ModelBuilder, extraTools ...tool.BaseTool) (*Handle, error) {
	return r.spawnAgent(ctx, role, task, modelOpt, "", extraTools...)
}

// spawnAgent is Spawn with an inbox seed applied before the worker goroutine
// starts. fork_context uses it so the first model call cannot beat the
// context it is supposed to inherit.
func (r *Registry) spawnAgent(ctx context.Context, role, task string,
	modelOpt ModelBuilder, seed string, extraTools ...tool.BaseTool) (*Handle, error) {

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, fmt.Errorf("swarm: registry closed")
	}
	r.seq++
	id := fmt.Sprintf("%s-%d", role, r.seq)
	h := &Handle{ID: id, Role: role, done: make(chan struct{}), spawned: time.Now()}
	r.agents[id] = h
	hook := r.spawnHook
	r.mu.Unlock()
	if hook != nil {
		hook(role, id, r.workerInstruction(task))
	}
	r.startWorker(ctx, h, task, r.workerInstruction(task), modelOpt, seed, extraTools)
	return h, nil
}

// finishAgent records leftover steering and archives the conversation so a
// later resume_agent can find it even after Stats has forgotten the handle.
func (r *Registry) finishAgent(h *Handle) {
	leftover := h.drainInbox()
	h.mu.Lock()
	h.undelivered = leftover
	if h.finished.IsZero() {
		h.finished = time.Now()
	}
	h.ensureResultLocked()
	h.mu.Unlock()
	r.remember(h)
}

// liveTail returns a running agent's latest activity text ("" if unknown).
func (r *Registry) liveTail(id string) string {
	h, ok := r.get(id)
	if !ok {
		return ""
	}
	return h.Activity()
}

func truncStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func lastLine(s string) string {
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func (r *Registry) turnsLimit() int {
	if r.MaxTurns > 0 {
		return r.MaxTurns
	}
	return 20
}

// SpawnForked is Spawn plus conversation inheritance (the Codex fork_turns
// equivalent): msgs are replayed into the sub-agent's session before its task
// message, so the worker starts with the manager's context so far.
func (r *Registry) SpawnForked(ctx context.Context, role, task string,
	modelOpt ModelBuilder, msgs []adk.Message, extraTools ...tool.BaseTool) (*Handle, error) {
	seed := formatContext("context inherited from manager", msgs)
	return r.spawnAgent(ctx, role, task, modelOpt, seed, extraTools...)
}

// Stats returns (running, finished) counts; finished handles are pruned from
// the registry as a side effect so it cannot grow without bound.
func (r *Registry) Stats() (running, finished int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, h := range r.agents {
		select {
		case <-h.Done():
			delete(r.agents, id)
			finished++
		default:
			running++
		}
	}
	return running, finished
}

// AgentProgress is a read-only snapshot of one sub-agent, for hosts that want
// to report progress while a turn is still running.
type AgentProgress struct {
	AgentID  string
	Role     string
	Running  bool
	Activity string        // latest streamed tail, while running
	Err      string        // why it failed, once it has
	Elapsed  time.Duration // how long it ran, or has been running so far
}

// Progress snapshots every tracked sub-agent without mutating the registry,
// ordered by spawn time so a UI's rows do not jump around.
//
// Use this and not Stats for anything that polls: Stats deliberately forgets
// finished agents to bound a long-lived registry, so a poll landing between a
// worker finishing and wait_agents collecting it would turn a completed worker
// into an unknown one and lose the result the manager just paid for.
func (r *Registry) Progress() []AgentProgress {
	r.mu.Lock()
	handles := make([]*Handle, 0, len(r.agents))
	for _, h := range r.agents {
		handles = append(handles, h)
	}
	r.mu.Unlock()

	sort.Slice(handles, func(i, j int) bool {
		a, b := handles[i].spawnedAt(), handles[j].spawnedAt()
		if a.Equal(b) {
			return handles[i].ID < handles[j].ID
		}
		return a.Before(b)
	})
	out := make([]AgentProgress, 0, len(handles))
	for _, h := range handles {
		out = append(out, h.progress())
	}
	return out
}

func (h *Handle) spawnedAt() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.spawned
}

// progress reads one handle's live state under its own lock.
func (h *Handle) progress() AgentProgress {
	p := AgentProgress{AgentID: h.ID, Role: h.Role}
	select {
	case <-h.Done():
	default:
		p.Running = true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	p.Activity = h.activity
	if h.err != nil {
		p.Err = h.err.Error()
	}
	// A running agent has no finish time yet, so its elapsed is measured
	// against now — otherwise a live row would report a negative age.
	if p.Running || h.finished.IsZero() {
		p.Elapsed = time.Since(h.spawned)
	} else {
		p.Elapsed = h.finished.Sub(h.spawned)
	}
	return p
}

// Wait blocks until every spawned agent has finished or ctx/timeout elapses.
// timeout <=0 means no timeout beyond ctx. Polling interval is 50ms — cheap
// because handle completion is channel-close based.
func (r *Registry) Wait(ctx context.Context, timeout time.Duration) error {
	var deadline <-chan time.Time
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		deadline = timer.C
	}
	for {
		running, _ := r.Stats()
		if running == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("swarm: wait timeout, %d agents still running", running)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Cleanup cancels every running agent and returns how many were killed. The
// registry stays usable (new spawns are allowed).
func (r *Registry) Cleanup() int {
	r.mu.Lock()
	running := make([]*Handle, 0, len(r.agents))
	for _, h := range r.agents {
		select {
		case <-h.Done():
		default:
			running = append(running, h)
		}
	}
	r.mu.Unlock()

	killed := 0
	for _, h := range running {
		h.Cancel()
		killed++
	}
	return killed
}

// Close marks the registry closed (no further spawns), cancels every running
// agent, and clears the map. Use with defer: `defer reg.Close()`.
func (r *Registry) Close() {
	r.mu.Lock()
	r.closed = true
	handles := make([]*Handle, 0, len(r.agents))
	for _, h := range r.agents {
		handles = append(handles, h)
	}
	r.agents = map[string]*Handle{}
	r.past = map[string]*agentPast{}
	r.pastIDs = nil
	r.mu.Unlock()
	for _, h := range handles {
		h.Cancel()
	}
}

func (r *Registry) sem(n int) chan struct{} {
	r.semOnce.Do(func() { r.semCh = make(chan struct{}, n) })
	return r.semCh
}

// ManagerConfig returns a ready-to-use ChatModelAgentConfig for the manager:
// it wires the five lifecycle tools and the fork_context history middleware in
// one step, so callers cannot forget the Handlers wiring. Options are applied
// left-to-right (last wins), matching Go functional-options convention.
//
//	reg := swarm.NewRegistry()
//	manager, err := adk.NewChatModelAgent(ctx, reg.ManagerConfig(
//		"manager", "swarm manager", managerModel,
//		swarm.WithInstruction(managerPrompt),
//		swarm.WithMaxIterations(32),
//	))
func (r *Registry) ManagerConfig(name, description string, m model.BaseChatModel,
	opts ...ManagerOption) *adk.ChatModelAgentConfig {

	mc := &managerConfig{
		ChatModelAgentConfig: adk.ChatModelAgentConfig{
			Name:          name,
			Description:   description,
			Instruction:   "Coordinate sub-agents.",
			Model:         m,
			MaxIterations: 24,
		},
	}
	mc.ToolsConfig.ToolsNodeConfig = compose.ToolsNodeConfig{Tools: r.Tools()}
	// stream inner sub-agent events up to the manager's event stream, and let
	// the manager itself run streaming — no blocking generations anywhere.
	mc.ToolsConfig.EmitInternalEvents = true
	mc.Handlers = []adk.ChatModelAgentMiddleware{r.ManagerMiddleware()}
	for _, o := range opts {
		if o != nil {
			o(mc)
		}
	}
	return &mc.ChatModelAgentConfig
}

// ManagerOption customizes ManagerConfig. Applied left-to-right, last wins.
type ManagerOption func(*managerConfig)

// managerConfig bundles the exposed config with swarm's own wiring so
// options can touch both without exporting the internals.
type managerConfig struct {
	adk.ChatModelAgentConfig
}

// WithInstruction replaces the manager's system prompt.
func WithInstruction(s string) ManagerOption {
	return func(c *managerConfig) { c.Instruction = s }
}

// WithMaxIterations overrides the manager's ReAct iteration cap.
func WithMaxIterations(n int) ManagerOption {
	return func(c *managerConfig) { c.MaxIterations = n }
}

// WithManagerTool appends extra tools to the manager alongside the five
// lifecycle tools (e.g. read_file so the manager can inspect results itself).
func WithManagerTool(ts ...tool.BaseTool) ManagerOption {
	return func(c *managerConfig) {
		c.ToolsConfig.ToolsNodeConfig.Tools = append(c.ToolsConfig.ToolsNodeConfig.Tools, ts...)
	}
}

// WithManagerHandler appends extra ChatModelAgentMiddleware after the swarm's
// own (history recorder) middleware.
func WithManagerHandler(hs ...adk.ChatModelAgentMiddleware) ManagerOption {
	return func(c *managerConfig) { c.Handlers = append(c.Handlers, hs...) }
}

// Injector delivers steering messages at each turn boundary.
type Injector struct {
	adk.BaseChatModelAgentMiddleware
	handle  *Handle
	histCap int
	reg     *Registry
}

// BeforeModelRewriteState drains queued steering messages into the
// conversation right before each model call — the eino-native equivalent of
// Codex steering: guidance lands at the next turn boundary without
// interrupting an in-flight model call or tool execution.
func (in *Injector) BeforeModelRewriteState(ctx context.Context,
	state *adk.ChatModelAgentState, mc *adk.TypedModelContext[*schema.Message],
) (context.Context, *adk.ChatModelAgentState, error) {
	for _, msg := range in.handle.drainInbox() {
		state.Messages = append(state.Messages, schema.UserMessage("[steer] "+msg))
	}
	in.handle.setHistory(state.Messages, in.histCap)
	return ctx, state, nil
}

// AfterModelRewriteState clones the worker's conversation after the model
// reply is appended, so resume_agent can continue this worker with what it
// actually said — not just the task it was given.
func (in *Injector) AfterModelRewriteState(ctx context.Context,
	state *adk.ChatModelAgentState, mc *adk.TypedModelContext[*schema.Message],
) (context.Context, *adk.ChatModelAgentState, error) {
	in.handle.setHistory(state.Messages, in.histCap)
	return ctx, state, nil
}

func (in *Injector) WrapInvokableToolCall(ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint, tc *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		if in.reg != nil && in.handle != nil {
			ctx = in.reg.bindToolOutput(ctx, in.handle.ID, in.handle.Role, tc)
		}
		return endpoint(ctx, argumentsInJSON, opts...)
	}, nil
}

// ModelBuilder builds the chat model for a spawned sub-agent. Returning the
// same instance across calls shares one client/endpoint across the swarm.
type ModelBuilder func(role, agentID string) model.BaseChatModel
