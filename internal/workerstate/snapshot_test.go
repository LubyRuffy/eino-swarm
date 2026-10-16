package workerstate

import (
	"reflect"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestSnapshotKeepsRuntimeStateSeparateFromHumanAndTools(t *testing.T) {
	workers := []Worker{{AgentID: "reader-1", Role: "reader", Status: Done}, {AgentID: "writer-2", Role: "writer", Status: Running}, {AgentID: "audit-3", Role: "audit", Status: Failed}}
	message := New(workers)
	snapshot, ok := Parse(message)
	if !ok || !reflect.DeepEqual(snapshot.Workers, workers) || snapshot.Note == "" {
		t.Fatalf("snapshot=%+v valid=%v", snapshot, ok)
	}
	if message.Role != schema.System || len(message.ToolCalls) != 0 || message.ToolCallID != "" {
		t.Fatal("runtime state became human input or a tool execution")
	}
	if New(nil) != nil {
		t.Fatal("an empty roster needs no message")
	}
	for _, invalid := range []*schema.Message{nil, schema.UserMessage(message.Content), schema.SystemMessage("{"), schema.SystemMessage(`{"source":"different"}`), schema.ToolMessage(message.Content, "tool")} {
		if _, ok := Parse(invalid); ok {
			t.Fatalf("accepted unrelated message: %+v", invalid)
		}
	}
}

func TestWithoutOnlyRemovesRuntimeSnapshots(t *testing.T) {
	human := schema.UserMessage("continue")
	call := schema.AssistantMessage("", []schema.ToolCall{{ID: "call", Function: schema.FunctionCall{Name: "spawn_agent", Arguments: `{"role":"reader","task":"work"}`}}})
	result := schema.ToolMessage(`{"agent_id":"reader-1"}`, "call")
	snapshot := New([]Worker{{AgentID: "reader-1", Role: "reader", Status: Done}})
	original := []*schema.Message{nil, human, snapshot, call, result, snapshot}
	expected := []*schema.Message{nil, human, call, result}
	got := Without(original)
	if !reflect.DeepEqual(got, expected) || original[2] != snapshot {
		t.Fatalf("real history was changed or input mutated: %+v", got)
	}
}
