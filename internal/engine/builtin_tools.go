package engine

import (
	"encoding/json"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// KindBuiltinTool is a cloud execution receipt, distinct from a local tool call.
const KindBuiltinTool = "builtin_tool"

func (e *Engine) recordBuiltinTools(call *store.LLMCall, events []provider.BuiltinToolEvent) {
	for _, event := range events {
		receipt := struct {
			provider.BuiltinToolEvent
			ProviderID string `json:"provider_id"`
			Model      string `json:"model"`
			LLMCallID  uint   `json:"llm_call_id"`
		}{event, call.ProviderID, call.Model, call.ID}
		body, err := json.Marshal(receipt)
		if err != nil {
			e.log.Warn("could not encode a cloud tool receipt", "turn", call.TurnID, "err", err)
			continue
		}
		e.record(store.Event{
			ThreadID: call.ThreadID, TurnID: call.TurnID, AgentID: call.AgentID,
			Kind: KindBuiltinTool, Text: string(body),
		})
	}
}
