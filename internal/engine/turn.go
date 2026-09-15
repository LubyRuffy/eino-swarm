package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// runtime is one conversation's live state. At most one turn runs at a time:
// a second request either steers the running turn or is rejected, which is
// what keeps the transcript linear and the sub-agent set attributable.
type runtime struct {
	engine   *Engine
	threadID string

	mu        sync.Mutex
	reg       *swarm.Registry
	cancel    context.CancelFunc
	turnID    string
	startedAt time.Time
	running   bool
	idle      chan struct{} // closed when the current turn finishes
}

// release marks the runtime idle. It runs before the turn's terminal event is
// published, because a client that reacts to that event by sending the next
// message must not be told the conversation is still busy. Calling it twice is
// harmless, which is what lets the panic path share it.
func (rt *runtime) release(cancel context.CancelFunc, idle chan struct{}) {
	cancel()
	rt.mu.Lock()
	wasRunning := rt.running
	rt.running = false
	rt.cancel = nil
	rt.reg = nil
	rt.mu.Unlock()
	if wasRunning {
		close(idle)
	}
}

func newRuntime(e *Engine, threadID string) *runtime {
	idle := make(chan struct{})
	close(idle)
	return &runtime{engine: e, threadID: threadID, idle: idle}
}

func (rt *runtime) status() Status {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	st := Status{ThreadID: rt.threadID, Running: rt.running, TurnID: rt.turnID}
	if !rt.startedAt.IsZero() {
		started := rt.startedAt
		st.StartedAt = &started
	}
	if rt.running {
		st.ElapsedMS = time.Since(rt.startedAt).Milliseconds()
		if rt.reg != nil {
			running, _ := rt.reg.Stats()
			st.Workers = running
		}
	}
	return st
}

func (rt *runtime) interrupt() {
	rt.mu.Lock()
	cancel := rt.cancel
	running := rt.running
	rt.mu.Unlock()
	if running && cancel != nil {
		cancel()
	}
}

func (rt *runtime) waitIdle(d time.Duration) {
	rt.mu.Lock()
	idle := rt.idle
	rt.mu.Unlock()
	select {
	case <-idle:
	case <-time.After(d):
	}
}

func (rt *runtime) close() {
	rt.mu.Lock()
	reg := rt.reg
	rt.reg = nil
	rt.mu.Unlock()
	if reg != nil {
		reg.Close()
	}
}

// ---------- public turn API ----------

// StartTurn begins a new turn on a conversation. It returns as soon as the
// turn is recorded and running: the answer arrives through the event stream,
// because a turn routinely outlives any HTTP request.
func (e *Engine) StartTurn(threadID, text string) (*store.Turn, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("engine: an empty message has nothing to answer")
	}
	th, err := e.store.GetThread(threadID)
	if err != nil {
		return nil, err
	}

	rt := e.runtimeFor(threadID)
	rt.mu.Lock()
	if rt.running {
		rt.mu.Unlock()
		return nil, ErrBusy
	}
	rt.mu.Unlock()

	prov, err := e.pool.Resolve(th.ProviderID)
	if err != nil {
		return nil, err
	}
	history, err := e.replayHistory(threadID)
	if err != nil {
		return nil, err
	}
	toolset, err := tools.Build(context.Background(), e.cfg, e.WorkspaceDir(threadID))
	if err != nil {
		return nil, err
	}

	turn := &store.Turn{
		ThreadID:   threadID,
		UserText:   text,
		ProviderID: prov.ID,
		Model:      prov.Model,
	}
	if err := e.store.CreateTurn(turn); err != nil {
		return nil, err
	}
	if err := e.store.AppendMessages(threadID, turn.ID, []store.Message{
		{Role: string(schema.User), Content: text},
	}); err != nil {
		return nil, err
	}

	builder, err := e.pool.ModelBuilder(context.Background(), prov.ID, e.callRecorder(threadID, turn.ID))
	if err != nil {
		_ = e.store.FinishTurn(turn.ID, store.TurnError, "", err.Error())
		return nil, err
	}

	reg := swarm.NewRegistry()
	reg.ModelBuilder = builder
	reg.MaxConcurrent = e.cfg.Swarm.MaxConcurrent
	reg.AgentTimeout = e.cfg.Swarm.AgentTimeout()
	reg.MaxTurns = e.cfg.Swarm.MaxTurns
	reg.SubAgentTools = toolset.Tools

	ctx, cancel := context.WithCancel(context.Background())
	idle := make(chan struct{})

	rt.mu.Lock()
	if rt.running { // lost a race with a concurrent StartTurn
		rt.mu.Unlock()
		cancel()
		reg.Close()
		_ = e.store.FinishTurn(turn.ID, store.TurnCancelled, "", ErrBusy.Error())
		return nil, ErrBusy
	}
	rt.reg, rt.cancel, rt.turnID, rt.startedAt, rt.running, rt.idle = reg, cancel, turn.ID, time.Now(), true, idle
	rt.mu.Unlock()

	_ = e.store.TouchThread(threadID)
	e.autoTitle(th, text)

	messages := append(history, schema.UserMessage(text))
	go rt.run(ctx, cancel, idle, turn, reg, toolset, messages, len(messages))
	return turn, nil
}

// Steer delivers guidance to a running turn. The swarm applies it at the
// manager's next turn boundary rather than interrupting a tool mid-call.
// When nothing is running it is not an error to ask — the caller gets ErrIdle
// and can start a turn instead.
func (e *Engine) Steer(threadID, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("engine: nothing to steer with")
	}
	if _, err := e.store.GetThread(threadID); err != nil {
		return err
	}
	e.mu.Lock()
	rt := e.runtimes[threadID]
	e.mu.Unlock()
	if rt == nil {
		return ErrIdle
	}
	rt.mu.Lock()
	reg, running, turnID := rt.reg, rt.running, rt.turnID
	rt.mu.Unlock()
	if !running || reg == nil {
		return ErrIdle
	}
	if !reg.SteerManager(text) {
		return ErrIdle
	}
	// The steer is part of the conversation, so it is both persisted as a
	// message and shown in the timeline.
	if err := e.store.AppendMessages(threadID, turnID, []store.Message{
		{Role: string(schema.User), Content: "[steer] " + text},
	}); err != nil {
		return err
	}
	e.record(store.Event{
		ThreadID: threadID, TurnID: turnID,
		Kind: KindSteer, AgentID: swarm.DefaultManagerID, Text: text,
	})
	return nil
}

// Interrupt cancels a running turn. Everything produced so far is kept: an
// interrupted turn is a turn with a short answer, not a lost one.
func (e *Engine) Interrupt(threadID string) error {
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
	rt.interrupt()
	return nil
}

// ---------- the run itself ----------

// KindSteer and KindCleanup are engine-level event kinds, alongside the ones
// the swarm library emits.
const (
	KindSteer     = "steer"
	KindCleanup   = "cleanup"
	KindReasoning = "reasoning"
	KindUser      = "user_message"
)

func (rt *runtime) run(ctx context.Context, cancel context.CancelFunc, idle chan struct{},
	turn *store.Turn, reg *swarm.Registry, toolset *tools.Set,
	messages []adk.Message, inputCount int,
) {
	e := rt.engine
	defer func() {
		r := recover()
		rt.release(cancel, idle)
		if r != nil {
			e.log.Error("turn panicked", "turn", turn.ID, "panic", r)
			_ = e.store.FinishTurn(turn.ID, store.TurnError, "", fmt.Sprintf("internal error: %v", r))
			e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
				Kind: swarm.NotifyError.String(), AgentID: swarm.DefaultManagerID,
				Err: fmt.Sprintf("internal error: %v", r)})
		}
	}()

	e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
		Kind: KindUser, AgentID: swarm.DefaultManagerID, Text: turn.UserText})

	acc := newAccumulator(e, rt.threadID, turn.ID)
	res, runErr := reg.RunWith(ctx, swarm.RunConfig{
		Instruction:        managerPrompt(toolset, e.cfg),
		Messages:           messages,
		ManagerTools:       toolset.Tools,
		ManagerMiddlewares: nil,
		MaxIterations:      e.cfg.Swarm.ManagerMaxIterations,
	}, swarm.Callback(acc.onNotify))
	acc.flushAll()

	// A turn owns its sub-agents: any worker still running when the manager
	// stops is a leak, both of goroutines and of the user's tokens.
	if killed := reg.Cleanup(); killed > 0 {
		e.record(store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
			Kind: KindCleanup, AgentID: swarm.DefaultManagerID,
			Text: fmt.Sprintf("stopped %d sub-agent(s) still running at the end of the turn", killed)})
	}
	// A steer that arrived after the manager's last model call was accepted but
	// never read. It becomes the next turn rather than disappearing.
	leftover := reg.TakePendingSteers()
	reg.Close()

	e.persistTranscript(rt.threadID, turn.ID, res.Transcript, inputCount)

	// Read the outcome before releasing, because releasing cancels the
	// context and would make every turn look interrupted.
	status, errText := store.TurnDone, ""
	switch {
	case ctx.Err() != nil:
		status, errText = store.TurnCancelled, "interrupted"
	case runErr != nil:
		status, errText = store.TurnError, runErr.Error()
	}

	// Idle before the terminal event: the UI starts its next turn the moment
	// it sees "done".
	rt.release(cancel, idle)

	if err := e.store.FinishTurn(turn.ID, status, res.Final, errText); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// The conversation was deleted while its last turn was winding
			// down. Nothing to close, and nothing worth warning about.
			e.log.Debug("turn vanished before it could be closed", "turn", turn.ID)
		} else {
			e.log.Warn("could not close turn", "turn", turn.ID, "err", err)
		}
	}
	_ = e.store.TouchThread(rt.threadID)

	final := store.Event{ThreadID: rt.threadID, TurnID: turn.ID,
		AgentID: swarm.DefaultManagerID, Text: res.Final}
	if status == store.TurnDone {
		final.Kind = swarm.NotifyDone.String()
	} else {
		final.Kind = swarm.NotifyError.String()
		final.Err = errText
	}
	e.record(final)
	rt.runLateSteers(status, leftover)
}

// runLateSteers turns steering that the manager never read into a turn of its
// own. Cancelled and failed turns skip it: someone who pressed stop does not
// want the thing they typed a moment earlier to start it all again.
func (rt *runtime) runLateSteers(status string, leftover []string) {
	if status != store.TurnDone || len(leftover) == 0 {
		return
	}
	if _, err := rt.engine.StartTurn(rt.threadID, strings.Join(leftover, "\n")); err != nil {
		rt.engine.log.Warn("could not run a late steer as its own turn",
			"thread", rt.threadID, "err", err)
	}
}

// callRecorder persists one row per model call so `zwai trace <turn>` can
// explain where a turn's time went.
func (e *Engine) callRecorder(threadID, turnID string) provider.Recorder {
	return func(r provider.CallRecord) {
		rec := &store.LLMCall{
			ThreadID: threadID, TurnID: turnID, AgentID: r.AgentID,
			ProviderID: r.ProviderID, Model: r.Model,
			InputMsgs: r.InputMsgs, InputChars: r.InputChars, OutputChars: r.OutputChars,
			DurationMS: r.Duration.Milliseconds(),
		}
		if r.Err != nil {
			rec.Err = r.Err.Error()
		}
		if err := e.store.AppendLLMCall(rec); err != nil {
			e.log.Warn("could not record a model call", "turn", turnID, "err", err)
		}
	}
}

// record persists an event and broadcasts it. Persisted events carry a
// sequence number, which is what makes them replayable.
// record persists an event and then broadcasts it.
//
// Persisting and broadcasting happen under one lock because workers and the
// manager record concurrently: without it, an event assigned sequence 7 can
// reach subscribers before sequence 6, and a client that resumes from the
// highest sequence it has seen would discard the older one as a duplicate and
// lose it until the next reload.
func (e *Engine) record(ev store.Event) {
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	if err := e.store.AppendEvent(&ev); err != nil {
		e.log.Warn("could not persist an event", "thread", ev.ThreadID, "kind", ev.Kind, "err", err)
	}
	e.broadcast(ev)
}

// emit broadcasts an event without persisting it. Used for streamed deltas:
// they are superseded within milliseconds, and the complete text is persisted
// at the end of the stream, so storing every token would grow the database
// without making replay any better. Seq stays 0 to mark them unreplayable.
func (e *Engine) emit(ev store.Event) {
	ev.CreatedAt = time.Now().UTC()
	e.broadcast(ev)
}

// ---------- accumulator ----------

// accumulator converts the swarm's notification stream into the event stream.
//
// Streamed text arrives as a growing accumulated string per agent; the UI
// wants that live, but a reload needs one complete record instead of ten
// thousand partial ones. So deltas are broadcast only, and the accumulator
// persists the finished text at each turn boundary.
type accumulator struct {
	engine   *Engine
	threadID string
	turnID   string

	mu        sync.Mutex
	reasoning map[string]string
	answer    map[string]string
	roles     map[string]string
}

func newAccumulator(e *Engine, threadID, turnID string) *accumulator {
	return &accumulator{
		engine: e, threadID: threadID, turnID: turnID,
		reasoning: map[string]string{},
		answer:    map[string]string{},
		roles:     map[string]string{},
	}
}

func (a *accumulator) event(n swarm.Notification, kind string) store.Event {
	ev := store.Event{
		ThreadID: a.threadID, TurnID: a.turnID,
		Kind: kind, AgentID: n.AgentID, Role: n.Role,
		Text: n.Text, ToolCallID: n.ToolCallID,
	}
	if n.Err != nil {
		ev.Err = n.Err.Error()
	}
	return ev
}

func (a *accumulator) onNotify(n swarm.Notification) {
	if n.Role != "" {
		a.mu.Lock()
		a.roles[n.AgentID] = n.Role
		a.mu.Unlock()
	}
	switch n.Kind {
	case swarm.NotifyReasoningDelta:
		a.setReasoning(n.AgentID, n.Text)
		a.engine.emit(a.event(n, n.Kind.String()))

	case swarm.NotifyDelta:
		// answer text has started, so the thinking for this turn is complete
		a.flushReasoning(n.AgentID)
		a.setAnswer(n.AgentID, n.Text)
		a.engine.emit(a.event(n, n.Kind.String()))

	case swarm.NotifyToolCall:
		// an interim turn: persist its thinking and its commentary, then the call
		a.flushReasoning(n.AgentID)
		a.flushAnswer(n.AgentID)
		a.engine.record(a.event(n, n.Kind.String()))

	case swarm.NotifyAgentMessage:
		// the swarm already hands over the complete text, so the pending
		// accumulator is dropped rather than persisted twice
		a.flushReasoning(n.AgentID)
		a.clearAnswer(n.AgentID)
		a.engine.record(a.event(n, n.Kind.String()))

	case swarm.NotifyTurn:
		a.engine.emit(a.event(n, n.Kind.String()))

	case swarm.NotifyDone, swarm.NotifyError:
		// the run loop writes the final event itself, once the turn's status
		// is known; emitting here too would duplicate it
		a.flushAgent(n.AgentID)

	case swarm.NotifyFinished:
		a.flushAgent(n.AgentID)
		a.engine.record(a.event(n, n.Kind.String()))

	default:
		a.engine.record(a.event(n, n.Kind.String()))
	}
}

func (a *accumulator) setReasoning(agentID, text string) {
	a.mu.Lock()
	a.reasoning[agentID] = text
	a.mu.Unlock()
}

func (a *accumulator) setAnswer(agentID, text string) {
	a.mu.Lock()
	a.answer[agentID] = text
	a.mu.Unlock()
}

func (a *accumulator) clearAnswer(agentID string) {
	a.mu.Lock()
	delete(a.answer, agentID)
	a.mu.Unlock()
}

func (a *accumulator) take(m map[string]string, agentID string) (string, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	text := m[agentID]
	delete(m, agentID)
	return text, a.roles[agentID]
}

func (a *accumulator) flushReasoning(agentID string) {
	text, role := a.take(a.reasoning, agentID)
	if strings.TrimSpace(text) == "" {
		return
	}
	a.engine.record(store.Event{
		ThreadID: a.threadID, TurnID: a.turnID,
		Kind: KindReasoning, AgentID: agentID, Role: role, Text: text,
	})
}

func (a *accumulator) flushAnswer(agentID string) {
	text, role := a.take(a.answer, agentID)
	if strings.TrimSpace(text) == "" {
		return
	}
	a.engine.record(store.Event{
		ThreadID: a.threadID, TurnID: a.turnID,
		Kind: swarm.NotifyAgentMessage.String(), AgentID: agentID, Role: role, Text: text,
	})
}

func (a *accumulator) flushAgent(agentID string) {
	a.flushReasoning(agentID)
	a.flushAnswer(agentID)
}

// flushAll persists whatever was still streaming when the turn ended. Without
// it, interrupting a turn mid-answer loses the partial answer on reload — the
// user would see text on screen that vanishes when they come back.
func (a *accumulator) flushAll() {
	a.mu.Lock()
	ids := make([]string, 0, len(a.reasoning)+len(a.answer))
	for id := range a.reasoning {
		ids = append(ids, id)
	}
	for id := range a.answer {
		ids = append(ids, id)
	}
	a.mu.Unlock()
	for _, id := range ids {
		a.flushAgent(id)
	}
}

// ---------- transcript ----------

// persistTranscript stores the messages this turn added. The transcript the
// swarm returns starts with the system instruction and then replays the input
// messages, so only the tail past them is new.
func (e *Engine) persistTranscript(threadID, turnID string, transcript []adk.Message, inputCount int) {
	// +1 for the leading system instruction
	start := inputCount + 1
	if len(transcript) <= start {
		return
	}
	var rows []store.Message
	for _, m := range transcript[start:] {
		if m == nil {
			continue
		}
		row := store.Message{
			Role:       string(m.Role),
			Content:    m.Content,
			Reasoning:  m.ReasoningContent,
			ToolCallID: m.ToolCallID,
		}
		if len(m.ToolCalls) > 0 {
			if raw, err := json.Marshal(m.ToolCalls); err == nil {
				row.ToolCalls = string(raw)
			}
		}
		if row.Role == string(schema.User) && strings.HasPrefix(row.Content, "[steer] ") {
			continue // already stored by Steer
		}
		rows = append(rows, row)
	}
	if err := e.store.AppendMessages(threadID, turnID, rows); err != nil {
		e.log.Warn("could not persist transcript", "turn", turnID, "err", err)
	}
}

// replayHistory rebuilds the conversation to hand to the next turn.
//
// It deliberately drops previous turns' tool calls and tool results and keeps
// only what was said. Tool output is the bulky part of an agent transcript —
// whole files, whole web pages — and replaying all of it costs the user tokens
// on every later turn to re-read material the assistant already summarized in
// its answer. Dropping a tool call and its results together also keeps the
// message sequence valid for providers that require the pairing.
func (e *Engine) replayHistory(threadID string) ([]adk.Message, error) {
	rows, err := e.store.ListMessages(threadID)
	if err != nil {
		return nil, err
	}
	out := make([]adk.Message, 0, len(rows))
	for _, r := range rows {
		switch schema.RoleType(r.Role) {
		case schema.User:
			if strings.TrimSpace(r.Content) != "" {
				out = append(out, schema.UserMessage(r.Content))
			}
		case schema.Assistant:
			if strings.TrimSpace(r.Content) != "" {
				out = append(out, schema.AssistantMessage(r.Content, nil))
			}
		default:
			// system messages come from the instruction; tool traffic is
			// intentionally not replayed
		}
	}
	return out, nil
}

// autoTitle names a conversation from its first message, the way Codex does,
// so the sidebar is readable without asking the user to name anything.
func (e *Engine) autoTitle(th *store.Thread, text string) {
	if strings.TrimSpace(th.Title) != "" {
		return
	}
	title := titleFrom(text)
	if title == "" {
		return
	}
	if err := e.store.UpdateThread(th.ID, map[string]any{"title": title}); err != nil {
		e.log.Warn("could not set the conversation title", "thread", th.ID, "err", err)
	} else {
		th.Title = title
	}
}

// titleMaxRunes keeps sidebar titles to one line at the narrowest supported
// sidebar width.
const titleMaxRunes = 48

func titleFrom(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	if flat == "" {
		return ""
	}
	r := []rune(flat)
	if len(r) <= titleMaxRunes {
		return flat
	}
	return strings.TrimSpace(string(r[:titleMaxRunes])) + "…"
}
