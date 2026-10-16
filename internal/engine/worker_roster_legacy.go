package engine

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/workerstate"
	"github.com/cloudwego/eino/schema"
)

// Old releases persisted generated roster pairs during compaction. Remove only
// their reserved identity, exact pair shape and event-backed worker identity;
// real calls, refusals and unrecognized evidence stay in order for replay.
func withoutLegacyWorkerPairs(messages, roster []*schema.Message) []*schema.Message {
	known := map[string]string{}
	for _, message := range roster {
		if snapshot, ok := workerstate.Parse(message); ok {
			for _, worker := range snapshot.Workers {
				known[worker.AgentID] = worker.Role
			}
		}
	}
	out := make([]*schema.Message, 0, len(messages))
	for i := 0; i < len(messages); i++ {
		if i+1 < len(messages) && isLegacyWorkerPair(messages[i], messages[i+1], known) {
			i++
			continue
		}
		out = append(out, messages[i])
	}
	return out
}

func isLegacyWorkerPair(assistant, result *schema.Message, known map[string]string) bool {
	if assistant == nil || result == nil || assistant.Role != schema.Assistant || assistant.Content != "" || assistant.ReasoningContent != "" || len(assistant.ToolCalls) != 1 || result.Role != schema.Tool {
		return false
	}
	call := assistant.ToolCalls[0]
	if call.Function.Name != spawnAgentToolName || result.ToolCallID != call.ID {
		return false
	}
	var args, body map[string]string
	if json.Unmarshal([]byte(call.Function.Arguments), &args) != nil || len(args) != 1 || json.Unmarshal([]byte(result.Content), &body) != nil || len(body) != 1 {
		return false
	}
	id := body["agent_id"]
	role, exists := known[id]
	if !exists || args["role"] != role {
		return false
	}
	if call.ID == "roster-"+id {
		return true
	}
	suffix, ok := strings.CutPrefix(call.ID, "restore-spawn-")
	if !ok {
		return false
	}
	index, err := strconv.Atoi(suffix)
	return err == nil && index >= 0 && strconv.Itoa(index) == suffix
}
