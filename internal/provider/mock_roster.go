package provider

import (
	"encoding/json"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/workerstate"
	"github.com/cloudwego/eino/schema"
)

// knownWorkerIDs reads explicit runtime state and real tool receipts. A roster
// is identity/state, not evidence that the manager just spawned those workers.
func knownWorkerIDs(messages []*schema.Message) []string {
	var ids []string
	seen := map[string]bool{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if snapshot, ok := workerstate.Parse(messages[i]); ok {
			for _, worker := range snapshot.Workers {
				add(worker.AgentID)
			}
			break
		}
	}
	for _, message := range messages {
		if message == nil || message.Role != schema.Tool {
			continue
		}
		var result struct {
			AgentID string `json:"agent_id"`
		}
		if json.Unmarshal([]byte(message.Content), &result) == nil {
			add(result.AgentID)
		}
	}
	return ids
}
