package provider

import (
	"reflect"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/workerstate"
	"github.com/cloudwego/eino/schema"
)

func TestMockManagerReadsRuntimeRosterWithoutInventingSpawns(t *testing.T) {
	message := workerstate.New([]workerstate.Worker{{AgentID: "reader-1", Role: "reader", Status: workerstate.Done}, {AgentID: "audit-2", Role: "audit", Status: workerstate.Failed}})
	ids := knownWorkerIDs([]*schema.Message{message})
	if !reflect.DeepEqual(ids, []string{"reader-1", "audit-2"}) {
		t.Fatalf("mock lost explicit historical identities: %v", ids)
	}
	reply := managerScript(1, []*schema.Message{schema.UserMessage("continue"), message})
	if len(reply.ToolCalls) != 1 || reply.ToolCalls[0].Function.Name != "wait_agents" {
		t.Fatalf("historical roster caused a new spawn batch: %+v", reply)
	}
}

func TestMockRosterKeepsRealReceiptsAndOnlyNewestSnapshot(t *testing.T) {
	old := workerstate.New([]workerstate.Worker{{AgentID: "old-1", Role: "old", Status: workerstate.Running}})
	latest := workerstate.New([]workerstate.Worker{{AgentID: "reader-1", Role: "reader", Status: workerstate.Done}})
	messages := []*schema.Message{old, latest, nil, schema.UserMessage(old.Content), schema.ToolMessage("{", "bad"), schema.ToolMessage(`{"agent_id":"reader-1"}`, "spawn"), schema.ToolMessage(`{"agent_id":"writer-2"}`, "spawn2"), schema.ToolMessage(`{"agent_id":"  "}`, "empty")}
	if got := knownWorkerIDs(messages); !reflect.DeepEqual(got, []string{"reader-1", "writer-2"}) {
		t.Fatalf("old state or duplicate receipt changed known identities: %v", got)
	}
}
