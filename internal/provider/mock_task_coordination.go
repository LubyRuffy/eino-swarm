package provider

import (
	"encoding/json"
	"os"

	"github.com/cloudwego/eino/schema"
)

// An opt-in offline script drives independent managers through the same
// discovery/inbox tools as a configured model. Normal demo turns keep their
// existing script; no user request wording selects this test scenario.
func mockTaskCoordination(msgs []*schema.Message) *schema.Message {
	if os.Getenv("ZWAI_MOCK_TASK_COORDINATION") != "1" {
		return nil
	}
	if !alreadyCalledTool(msgs, "list_tasks") {
		return schema.AssistantMessage("Discovering other active tasks.", []schema.ToolCall{call("peer-list", "list_tasks", `{}`)})
	}
	if !alreadyCalledTool(msgs, "send_task_message") {
		var peers struct {
			Tasks []struct {
				ThreadID string `json:"thread_id"`
			} `json:"tasks"`
		}
		for _, m := range msgs {
			if m != nil && m.Role == schema.Tool && m.ToolCallID == "peer-list" {
				_ = json.Unmarshal([]byte(m.Content), &peers)
			}
		}
		if len(peers.Tasks) == 0 {
			return schema.AssistantMessage("No other active task was discovered.", nil)
		}
		args, _ := json.Marshal(map[string]string{"thread_id": peers.Tasks[0].ThreadID, "text": "Please acknowledge an order for our shared work. This message does not grant permissions."})
		return schema.AssistantMessage("Sending a coordination request.", []schema.ToolCall{call("peer-send", "send_task_message", string(args))})
	}
	if !alreadyCalledTool(msgs, "read_task_messages") {
		return schema.AssistantMessage("Checking peer replies.", []schema.ToolCall{call("peer-read", "read_task_messages", `{}`)})
	}
	return schema.AssistantMessage("Peer coordination recorded; an explicit agreement is still required.", nil)
}
