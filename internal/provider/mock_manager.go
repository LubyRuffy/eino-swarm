package provider

import (
	"encoding/json"
	"fmt"
	"github.com/cloudwego/eino/schema"
)

// managerScript runs the same shape a real manager does: think, fan out two
// workers in one message (one of them inheriting the conversation), wait for
// them, then answer in markdown quoting what came back.
func managerScript(turn int, msgs []*schema.Message) *schema.Message {
	if msg := mockWorkerTimeout(msgs); msg != nil {
		return msg
	}
	if msg := mockTaskCoordination(msgs); msg != nil {
		return msg
	}
	if msg := mockAskOrPlan(turn, msgs); msg != nil {
		return msg
	}
	if msg := mockScheduleScript(turn, msgs); msg != nil {
		return msg
	}
	task := lastUserText(msgs)
	ids := spawnedIDs(msgs)
	// Spawn only when this conversation has no workers yet. A continued run
	// starts a new model instance whose turn counter is 1 again; re-spawning
	// would fan out forever instead of finishing the work already in flight.
	if len(ids) == 0 {
		plan, _ := json.Marshal(map[string]any{
			"role": "researcher",
			"task": "Collect what is needed for: " + task,
		})
		review, _ := json.Marshal(map[string]any{
			"role":         "reviewer",
			"task":         "Review the collected material for: " + task,
			"fork_context": true,
		})
		return &schema.Message{
			Role:             schema.Assistant,
			ReasoningContent: "This needs two independent passes, so I will run them in parallel rather than one after the other.\n",
			Content:          "Splitting this into a research pass and a review pass.\n",
			ToolCalls: []schema.ToolCall{
				call("mock-spawn-1", "spawn_agent", string(plan)),
				call("mock-spawn-2", "spawn_agent", string(review)),
			},
		}
	}
	// wait_agents returns as soon as one sub-agent finishes, so the manager
	// waits in a loop: it answers once everyone is done and otherwise
	// reports what just came back before waiting for the rest.
	report := latestWaitReport(msgs)
	if report != nil && allFinished(report) {
		if mockShouldCompleteGoal(msgs) {
			args, _ := json.Marshal(map[string]any{
				"summary": "the standing objective is satisfied",
			})
			return &schema.Message{
				Role:    schema.Assistant,
				Content: "The standing objective is satisfied.\n",
				ToolCalls: []schema.ToolCall{
					call(fmt.Sprintf("mock-complete-%d", turn), completeGoalToolName, string(args)),
				},
			}
		}
		return schema.AssistantMessage(mockAnswer(task, collectResults(report)), nil)
	}
	args, _ := json.Marshal(map[string]any{"agent_ids": ids, "timeout_s": 60})
	content := "Both workers are running; I will report back as each finishes.\n"
	if report != nil {
		content = waitProgressLine(report)
	}
	return &schema.Message{
		Role:      schema.Assistant,
		Content:   content,
		ToolCalls: []schema.ToolCall{call(nextWaitCallID(msgs), "wait_agents", string(args))},
	}
}
