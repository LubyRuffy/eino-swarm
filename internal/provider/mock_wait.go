package provider

import (
	"encoding/json"

	"github.com/cloudwego/eino/schema"
)

type mockWaitAgent struct {
	AgentID    string `json:"agent_id"`
	Generation int    `json:"generation,omitempty"`
	Status     string `json:"status"`
	Result     string `json:"result"`
	Err        string `json:"error"`
}

type mockWaitState struct {
	report  *mockWaitReport
	pending []string
	failed  bool
}

// A partial wait collects only its returned terminal generations. Keep earlier
// results, but an observed newer numbered tool receipt invalidates older results.
// Legacy receipts without generations retain ID-level compatibility, not proof
// of the current generation after restoration. A roster supplies identities only;
// its System-prefix position cannot establish receipt freshness or collection.
func defaultMockWaitState(messages []*schema.Message, ids []string) mockWaitState {
	known := make(map[string]bool, len(ids))
	for _, id := range ids {
		known[id] = true
	}
	generations := make(map[string]int, len(ids))
	entries := make(map[string]mockWaitAgent, len(ids))
	for _, message := range messages {
		if message == nil || message.Role != schema.Tool {
			continue
		}
		var receipt struct {
			AgentID     string `json:"agent_id"`
			Generation  int    `json:"generation"`
			ResumedFrom string `json:"resumed_from"`
		}
		if json.Unmarshal([]byte(message.Content), &receipt) == nil && known[receipt.AgentID] {
			if receipt.Generation > generations[receipt.AgentID] {
				generations[receipt.AgentID] = receipt.Generation
				delete(entries, receipt.AgentID)
			} else if receipt.ResumedFrom != "" && receipt.Generation == 0 {
				delete(entries, receipt.AgentID)
			}
		}
		var report mockWaitReport
		if json.Unmarshal([]byte(message.Content), &report) != nil {
			continue
		}
		for _, entry := range report.Agents {
			// A failed ID lookup has no execution generation. Preserve that
			// failure without treating it as collection or lowering the known floor.
			unknownID := entry.Status == "unknown" && entry.Generation == 0
			if !known[entry.AgentID] || entry.Generation < 0 || (!unknownID && entry.Generation < generations[entry.AgentID]) {
				continue
			}
			if entry.Generation > generations[entry.AgentID] {
				generations[entry.AgentID] = entry.Generation
			}
			entries[entry.AgentID] = entry
		}
	}
	state := mockWaitState{report: &mockWaitReport{}}
	for _, id := range ids {
		entry, ok := entries[id]
		if !ok {
			state.pending = append(state.pending, id)
			continue
		}
		state.report.Agents = append(state.report.Agents, entry)
		switch entry.Status {
		case "done", "finished": // finished is the legacy offline report spelling.
			state.failed = state.failed || entry.Err != ""
		case "failed", "unknown":
			state.failed = true
		default:
			state.pending = append(state.pending, id)
		}
	}
	return state
}
