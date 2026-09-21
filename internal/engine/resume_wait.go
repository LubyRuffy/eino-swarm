package engine

import (
	"encoding/json"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

const waitAgentsToolName = "wait_agents"

// resumeWaitEntry is one line of the synthetic wait_agents result written
// when a leftover turn is resumed. Same keys as the live tool so the
// manager can wait again instead of inventing a new swarm.
type resumeWaitEntry struct {
	AgentID string `json:"agent_id"`
	Role    string `json:"role,omitempty"`
	Status  string `json:"status"`
	Result  string `json:"result,omitempty"`
	Err     string `json:"error,omitempty"`
}

type resumeWaitReport struct {
	Agents   []resumeWaitEntry `json:"agents"`
	TimedOut bool              `json:"timed_out"`
}

func waitAgentIDsFromArgs(args string) []string {
	var a struct {
		AgentIDs []string `json:"agent_ids"`
	}
	if json.Unmarshal([]byte(args), &a) != nil {
		return nil
	}
	out := make([]string, 0, len(a.AgentIDs))
	for _, id := range a.AgentIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

func resumeWaitSnapshotJSON(ids []string, running []swarm.RestoredWorker, finished []swarm.FinishedWorker) string {
	byRun := map[string]swarm.RestoredWorker{}
	for _, w := range running {
		byRun[w.ID] = w
	}
	byDone := map[string]swarm.FinishedWorker{}
	for _, w := range finished {
		byDone[w.ID] = w
	}
	rep := resumeWaitReport{TimedOut: true, Agents: make([]resumeWaitEntry, 0, len(ids))}
	for _, id := range ids {
		e := resumeWaitEntry{AgentID: id, Status: "unknown", Err: "unknown agent"}
		if w, ok := byRun[id]; ok {
			e.Role, e.Status, e.Err = w.Role, "running", ""
		} else if w, ok := byDone[id]; ok {
			e.Role, e.Status, e.Result = w.Role, "done", w.Result
			e.Err = ""
			if w.Err != nil {
				e.Status = "failed"
				e.Err = w.Err.Error()
			}
		}
		rep.Agents = append(rep.Agents, e)
	}
	body, _ := json.Marshal(rep)
	return string(body)
}

// sealTrailingIncompleteToolCalls is crash-resume's version of
// dropTrailingIncompleteToolCalls: a dangling wait_agents is completed
// with a timed-out snapshot of those ids so the manager keeps waiting
// on the live workers. Any other unfinished tool call is still dropped.
func sealTrailingIncompleteToolCalls(msgs []adk.Message, running []swarm.RestoredWorker, finished []swarm.FinishedWorker) []adk.Message {
	lastAsst, needed := trailingIncompleteToolCalls(msgs)
	if lastAsst < 0 || len(needed) == 0 {
		return msgs
	}
	idsByCall := map[string][]string{}
	for _, tc := range msgs[lastAsst].ToolCalls {
		if _, ok := needed[tc.ID]; !ok {
			continue
		}
		if tc.Function.Name != waitAgentsToolName {
			return dropTrailingIncompleteToolCalls(msgs)
		}
		ids := waitAgentIDsFromArgs(tc.Function.Arguments)
		if len(ids) == 0 {
			for _, w := range running {
				ids = append(ids, w.ID)
			}
		}
		idsByCall[tc.ID] = ids
	}
	out := append([]adk.Message{}, msgs[:lastAsst+1]...)
	for _, tc := range msgs[lastAsst].ToolCalls {
		if _, ok := needed[tc.ID]; !ok {
			continue
		}
		out = append(out, &schema.Message{
			Role:       schema.Tool,
			Content:    resumeWaitSnapshotJSON(idsByCall[tc.ID], running, finished),
			ToolCallID: tc.ID,
		})
	}
	for _, m := range msgs[lastAsst+1:] {
		if isSteerUser(m) {
			out = append(out, m)
		}
	}
	return out
}

func trailingIncompleteToolCalls(msgs []adk.Message) (lastAsst int, needed map[string]struct{}) {
	lastAsst = -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i] == nil || isSteerUser(msgs[i]) {
			continue
		}
		if msgs[i].Role == schema.Assistant && len(msgs[i].ToolCalls) > 0 {
			lastAsst = i
			break
		}
		if msgs[i].Role == schema.User || (msgs[i].Role == schema.Assistant && len(msgs[i].ToolCalls) == 0) {
			return -1, nil
		}
	}
	if lastAsst < 0 {
		return -1, nil
	}
	needed = map[string]struct{}{}
	for _, tc := range msgs[lastAsst].ToolCalls {
		if tc.ID != "" {
			needed[tc.ID] = struct{}{}
		}
	}
	if len(needed) == 0 {
		return lastAsst, needed
	}
	for _, m := range msgs[lastAsst+1:] {
		if m != nil && m.Role == schema.Tool {
			delete(needed, m.ToolCallID)
		}
	}
	return lastAsst, needed
}
