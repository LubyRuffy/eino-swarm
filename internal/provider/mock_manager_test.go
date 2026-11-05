package provider

import (
	"encoding/json"
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

// An already-collected result must not turn a wait for a slow worker into polling.
func TestMockManagerWaitsOnlyForPendingWorkers(t *testing.T) {
	msgs := []*schema.Message{
		schema.UserMessage("work"),
		schema.ToolMessage(`{"agent_id":"done-worker","generation":1}`, "spawn-first"),
		schema.ToolMessage(`{"agent_id":"pending-worker","generation":1}`, "spawn-second"),
		schema.ToolMessage(`{"agents":[{"agent_id":"done-worker","generation":1,"status":"finished","result":"first result"},{"agent_id":"pending-worker","generation":1,"status":"running"}]}`, "wait-first"),
	}
	got := managerScript(3, msgs)
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].Function.Name != "wait_agents" {
		t.Fatalf("pending worker was not waited: %+v", got)
	}
	var args struct {
		IDs []string `json:"agent_ids"`
	}
	if err := json.Unmarshal([]byte(got.ToolCalls[0].Function.Arguments), &args); err != nil {
		t.Fatal(err)
	}
	if len(args.IDs) != 1 || args.IDs[0] != "pending-worker" {
		t.Fatalf("wait includes an already-collected worker: %v", args.IDs)
	}
}

// A partial report cannot settle a worker absent from that report.
func TestMockManagerWaitsForKnownWorkersMissingFromTheLastReport(t *testing.T) {
	msgs := []*schema.Message{
		schema.UserMessage("work"),
		schema.ToolMessage(`{"agent_id":"done-worker","generation":1}`, "spawn-first"),
		schema.ToolMessage(`{"agent_id":"pending-worker","generation":1}`, "spawn-second"),
		schema.ToolMessage(`{"agents":[{"agent_id":"done-worker","generation":1,"status":"finished","result":"first result"}]}`, "wait-partial"),
	}
	got := managerScript(3, msgs)
	if len(got.ToolCalls) != 1 || got.ToolCalls[0].Function.Name != "wait_agents" {
		t.Fatalf("partial report prematurely finishes known workers: %+v", got)
	}
}
