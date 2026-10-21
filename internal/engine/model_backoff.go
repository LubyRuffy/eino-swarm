package engine

import (
	"encoding/json"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// KindModelBackoff records a provider recovery wait on its calling turn. It
// does not mean another model request was sent or another retry was granted.
const KindModelBackoff = "model_backoff"

func (e *Engine) recordModelBackoff(threadID, turnID string, r provider.CallRecord) {
	body, _ := json.Marshal(r.Backoff) // Only integers; no unsupported JSON values.
	e.record(store.Event{
		ThreadID: threadID, TurnID: turnID, AgentID: r.AgentID,
		Kind: KindModelBackoff, Text: string(body),
	})
}
