package engine

import (
	"strings"
	"sync"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

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

	// live holds the latest streamed delta per agent and kind until the
	// coalesce timer fires. Without it a 50-token-per-second model would
	// redraw the UI fifty times a second, once per token.
	coalesce  time.Duration
	live      map[string]store.Event
	liveTimer map[string]*time.Timer
}

func newAccumulator(e *Engine, threadID, turnID string, coalesce time.Duration) *accumulator {
	return &accumulator{
		engine: e, threadID: threadID, turnID: turnID,
		reasoning: map[string]string{},
		answer:    map[string]string{},
		roles:     map[string]string{},
		coalesce:  coalesce,
		live:      map[string]store.Event{},
		liveTimer: map[string]*time.Timer{},
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
	if n.Kind == swarm.NotifyFinished && a.engine.isAbandoned(a.threadID) {
		// Quit cancelled in-flight workers; recording finished would make
		// the next start treat them as done instead of restoring them.
		return
	}
	if n.Role != "" {
		a.mu.Lock()
		a.roles[n.AgentID] = n.Role
		a.mu.Unlock()
	}
	switch n.Kind {
	case swarm.NotifyReasoningDelta:
		a.setReasoning(n.AgentID, n.Text)
		a.pushLive(n)

	case swarm.NotifyDelta:
		// answer text has started, so the thinking for this turn is complete
		a.flushKind(n.AgentID, swarm.NotifyReasoningDelta.String())
		a.flushReasoning(n.AgentID)
		a.setAnswer(n.AgentID, n.Text)
		a.pushLive(n)

	case swarm.NotifyToolCall:
		// an interim turn: persist its thinking and its commentary, then the call
		a.flushAgentLive(n.AgentID)
		a.flushReasoning(n.AgentID)
		a.flushAnswer(n.AgentID)
		a.engine.record(a.event(n, n.Kind.String()))

	case swarm.NotifyToolDelta:
		a.pushLive(n)

	case swarm.NotifyToolResult:
		a.dropKey(liveKey(n.AgentID, swarm.NotifyToolDelta.String(), n.ToolCallID))
		a.engine.record(a.event(n, n.Kind.String()))

	case swarm.NotifyAgentMessage:
		// the swarm already hands over the complete text, so the pending
		// accumulator is dropped rather than persisted twice
		a.dropLive(n.AgentID)
		a.flushReasoning(n.AgentID)
		a.clearAnswer(n.AgentID)
		a.engine.record(a.event(n, n.Kind.String()))

	case swarm.NotifyTurn:
		// The next model round is starting. Drop unpersisted deltas from a
		// preempted generate so a later tool_call cannot flush them as a
		// finished answer.
		a.dropLive(n.AgentID)
		a.clearAnswer(n.AgentID)
		a.mu.Lock()
		delete(a.reasoning, n.AgentID)
		a.mu.Unlock()
		a.engine.emit(a.event(n, n.Kind.String()))

	case swarm.NotifyDone, swarm.NotifyError:
		// the run loop writes the final event itself, once the turn's status
		// is known; emitting here too would duplicate it
		a.flushAgentLive(n.AgentID)
		a.flushAgent(n.AgentID)

	case swarm.NotifyFinished:
		a.flushAgentLive(n.AgentID)
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
	a.flushAllLive()
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
