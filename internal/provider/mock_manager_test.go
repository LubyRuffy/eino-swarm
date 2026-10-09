package provider

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestDefaultOfflineManagerStillAsksWaitsAndCompletesTheGoal(t *testing.T) {
	t.Setenv("ZWAI_MOCK_TASK_COORDINATION", "")
	SetMockAskUser(true)
	t.Cleanup(func() { SetMockAskUser(false) })
	question := managerScript(1, []*schema.Message{schema.UserMessage("work")})
	if len(question.ToolCalls) != 1 || question.ToolCalls[0].Function.Name != askUserToolName {
		t.Fatalf("question=%+v", question)
	}
	SetMockAskUser(false)
	msgs := []*schema.Message{schema.UserMessage("work"), schema.ToolMessage(`{"agent_id":"worker"}`, "spawn")}
	msgs = append(msgs, schema.ToolMessage(`{"agents":[{"agent_id":"worker","status":"running"}],"timed_out":true}`, "wait"))
	progress := managerScript(2, msgs)
	if len(progress.ToolCalls) != 1 || progress.ToolCalls[0].Function.Name != "wait_agents" || !strings.Contains(progress.Content, "1 still working") {
		t.Fatalf("progress=%+v", progress)
	}
	msgs = append(msgs, schema.ToolMessage(`{"agents":[{"agent_id":"worker","status":"finished","result":"verified"}]}`, "wait-done"), schema.SystemMessage("An open objective can call "+completeGoalToolName))
	completed := managerScript(3, msgs)
	if len(completed.ToolCalls) != 1 || completed.ToolCalls[0].Function.Name != completeGoalToolName {
		t.Fatalf("completion=%+v", completed)
	}
}
