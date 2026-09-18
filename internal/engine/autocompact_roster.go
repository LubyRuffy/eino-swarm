package engine

import (
	"encoding/json"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/cloudwego/eino/schema"
)

// pinWorkerPairs rebuilds spawn_agent tool pairs from leftover spawned /
// finished events. The roster lives on those rows, not in compactable
// replay: after a fold — or a later turn that dropped previous tool
// results — the model still has to see the ids or it will mint twins.
// Synthetic call ids are fine — mock spawnedIDs reads agent_id.
func pinWorkerPairs(running []swarm.RestoredWorker, finished []swarm.FinishedWorker) []*schema.Message {
	var out []*schema.Message
	seen := map[string]struct{}{}
	add := func(id, role string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		if strings.TrimSpace(role) == "" {
			role = "worker"
		}
		args, _ := json.Marshal(map[string]string{"role": role})
		body, _ := json.Marshal(map[string]string{"agent_id": id})
		callID := "roster-" + id
		out = append(out,
			schema.AssistantMessage("", []schema.ToolCall{{
				ID:   callID,
				Type: "function",
				Function: schema.FunctionCall{
					Name:      spawnAgentToolName,
					Arguments: string(args),
				},
			}}),
			&schema.Message{Role: schema.Tool, Content: string(body), ToolCallID: callID},
		)
	}
	for _, w := range running {
		add(w.ID, w.Role)
	}
	for _, w := range finished {
		add(w.ID, w.Role)
	}
	return out
}

func (m *autoCompact) rosterPin() []*schema.Message {
	if m == nil || m.engine == nil || m.threadID == "" {
		return nil
	}
	running, finished := m.engine.workersFromThread(m.threadID)
	return pinWorkerPairs(running, finished)
}

func toolResultAgentID(content string) string {
	var r struct {
		AgentID string `json:"agent_id"`
	}
	if json.Unmarshal([]byte(content), &r) != nil {
		return ""
	}
	return strings.TrimSpace(r.AgentID)
}

func dropPairsAlreadyIn(pairs, tail []*schema.Message) []*schema.Message {
	if len(pairs) == 0 {
		return nil
	}
	haveCall := map[string]struct{}{}
	haveAgent := map[string]struct{}{}
	for _, m := range tail {
		if m == nil {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID != "" {
				haveCall[tc.ID] = struct{}{}
			}
		}
		if m.ToolCallID != "" {
			haveCall[m.ToolCallID] = struct{}{}
		}
		if m.Role == schema.Tool {
			if id := toolResultAgentID(m.Content); id != "" {
				haveAgent[id] = struct{}{}
			}
		}
	}
	skipCall := map[string]struct{}{}
	for _, m := range pairs {
		if m == nil || m.Role != schema.Tool {
			continue
		}
		if _, ok := haveCall[m.ToolCallID]; ok {
			skipCall[m.ToolCallID] = struct{}{}
		}
		if id := toolResultAgentID(m.Content); id != "" {
			if _, ok := haveAgent[id]; ok {
				skipCall[m.ToolCallID] = struct{}{}
			}
		}
	}
	out := pairs[:0]
	for _, m := range pairs {
		if m == nil {
			continue
		}
		if m.Role == schema.Tool && m.ToolCallID != "" {
			if _, ok := skipCall[m.ToolCallID]; ok {
				continue
			}
		}
		skipAsst := false
		for _, tc := range m.ToolCalls {
			if _, ok := skipCall[tc.ID]; ok {
				skipAsst = true
				break
			}
			if _, ok := haveCall[tc.ID]; ok {
				skipAsst = true
				break
			}
		}
		if skipAsst {
			continue
		}
		out = append(out, m)
	}
	return out
}
