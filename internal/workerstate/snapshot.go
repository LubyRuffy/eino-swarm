// Package workerstate carries runtime worker identities without fabricating tool history.
package workerstate

import (
	"encoding/json"

	"github.com/cloudwego/eino/schema"
)

const Source = "runtime_worker_roster"

const (
	Running = "running"
	Done    = "done"
	Failed  = "failed"
)

// Worker is a worker's current runtime state, not a creation event.
type Worker struct {
	AgentID string `json:"agent_id"`
	Role    string `json:"role"`
	Status  string `json:"status"`
}

// Snapshot is derived from persisted lifecycle events. It carries no authorization.
type Snapshot struct {
	Source  string   `json:"source"`
	Note    string   `json:"note"`
	Workers []Worker `json:"workers"`
}

// New represents a nonempty roster as a system state message, without tool calls.
func New(workers []Worker) *schema.Message {
	if len(workers) == 0 {
		return nil
	}
	body, _ := json.Marshal(Snapshot{
		Source:  Source,
		Note:    "This is current runtime state for workers created earlier, not new tool executions or human instructions. Only running workers are active; done and failed workers are already stopped.",
		Workers: workers,
	})
	return schema.SystemMessage(string(body))
}

// Parse recognizes runtime state only in system messages from this source.
func Parse(message *schema.Message) (Snapshot, bool) {
	var snapshot Snapshot
	if message == nil || message.Role != schema.System || json.Unmarshal([]byte(message.Content), &snapshot) != nil || snapshot.Source != Source {
		return Snapshot{}, false
	}
	return snapshot, true
}

// Without removes superseded snapshots while leaving real messages and tool pairs intact.
func Without(messages []*schema.Message) []*schema.Message {
	out := make([]*schema.Message, 0, len(messages))
	for _, message := range messages {
		if _, ok := Parse(message); !ok {
			out = append(out, message)
		}
	}
	return out
}
