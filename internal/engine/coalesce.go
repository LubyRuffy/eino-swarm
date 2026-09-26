package engine

import (
	"strings"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
)

// liveKey is one streamed stream: one agent, one kind, and for tool
// deltas the call id so two parallel execs cannot overwrite each other.
func liveKey(agentID, kind, toolCallID string) string {
	return agentID + "\x00" + kind + "\x00" + toolCallID
}

// pushLive holds a streamed delta until the coalesce window closes, then
// sends the latest one. Tokens that arrive inside the window replace the
// pending event, so a 50-token-per-second model costs the UI one redraw, not
// fifty.
func (a *accumulator) pushLive(n swarm.Notification) {
	ev := a.event(n, n.Kind.String())
	if a.coalesce <= 0 {
		a.engine.emit(ev)
		return
	}
	key := liveKey(n.AgentID, n.Kind.String(), n.ToolCallID)
	a.mu.Lock()
	_, armed := a.liveTimer[key]
	a.live[key] = ev
	if !armed {
		a.liveTimer[key] = time.AfterFunc(a.coalesce, func() { a.flushKey(key) })
	}
	a.mu.Unlock()
}

// flushAgentLive sends every pending streamed event for this agent. A tool
// call or a finished worker must not leave a token sitting in the coalescer
// that would arrive after the row that says it is done.
func (a *accumulator) flushAgentLive(agentID string) {
	a.gate.Lock()
	defer a.gate.Unlock()
	a.flushAgentLiveLocked(agentID)
}

func (a *accumulator) flushAgentLiveLocked(agentID string) {
	for _, key := range a.liveKeys(agentID) {
		a.flushKeyLocked(key)
	}
}

func (a *accumulator) liveKeys(agentID string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	keys := make([]string, 0, len(a.live))
	for key := range a.live {
		if agentID == "" || hasAgentPrefix(key, agentID) {
			keys = append(keys, key)
		}
	}
	return keys
}

func hasAgentPrefix(key, agentID string) bool {
	return strings.HasPrefix(key, agentID+"\x00")
}

// dropLive cancels pending streamed events without sending them. Used when
// the complete text is about to be recorded: emitting the last partial first
// would paint the same answer twice.
func (a *accumulator) dropLive(agentID string) {
	a.gate.Lock()
	defer a.gate.Unlock()
	a.dropLiveLocked(agentID)
}

func (a *accumulator) dropLiveLocked(agentID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for key, timer := range a.liveTimer {
		if agentID != "" && !hasAgentPrefix(key, agentID) {
			continue
		}
		if timer != nil {
			timer.Stop()
		}
		delete(a.liveTimer, key)
		delete(a.live, key)
	}
}

// flushAllLive sends every held delta. Called at the end of a turn so a
// cancelled stream still shows the last tokens and no timer fires after done.
func (a *accumulator) flushAllLive() {
	a.gate.Lock()
	defer a.gate.Unlock()
	a.flushAllLiveLocked()
}

func (a *accumulator) flushAllLiveLocked() {
	for _, key := range a.liveKeys("") {
		a.flushKeyLocked(key)
	}
}

func (a *accumulator) flushKey(key string) {
	a.gate.Lock()
	defer a.gate.Unlock()
	a.flushKeyLocked(key)
}

// flushKeyLocked claims one held delta and broadcasts it. Caller holds gate,
// so a boundary that already decided "this paragraph, then the tool call"
// cannot be overtaken by the timer between the claim and the send.
func (a *accumulator) flushKeyLocked(key string) {
	a.mu.Lock()
	ev, ok := a.live[key]
	delete(a.live, key)
	if timer := a.liveTimer[key]; timer != nil {
		timer.Stop()
	}
	delete(a.liveTimer, key)
	a.mu.Unlock()
	if !ok {
		return
	}
	if a.beforeLiveEmit != nil {
		a.beforeLiveEmit()
	}
	a.engine.emit(ev)
}

func (a *accumulator) dropKey(key string) {
	a.gate.Lock()
	defer a.gate.Unlock()
	a.dropKeyLocked(key)
}

func (a *accumulator) dropKeyLocked(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if timer := a.liveTimer[key]; timer != nil {
		timer.Stop()
	}
	delete(a.liveTimer, key)
	delete(a.live, key)
}
