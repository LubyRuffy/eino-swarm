package engine

import (
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestNuwaDescendantCollectionSurvivesColdReplay(t *testing.T) {
	base := []store.Event{
		{Kind: "spawned", AgentID: "parent-1", WorkerGeneration: 1},
		{Kind: "spawned", AgentID: "child-2", ParentID: "parent-1", WorkerGeneration: 1},
		{Kind: "finished", AgentID: "child-2", ParentID: "parent-1", WorkerGeneration: 1},
	}
	for _, caller := range []string{"parent-1", "peer-3"} {
		t.Run(caller, func(t *testing.T) {
			events := append(append([]store.Event{}, base...),
				store.Event{Kind: "tool_call", AgentID: caller, WorkerGeneration: 1, ToolCallID: "wait", Text: `wait_agents({"agent_ids":["child-2"]})`},
				store.Event{Kind: "tool_result", AgentID: caller, WorkerGeneration: 1, ToolCallID: "wait", Text: `{"agents":[{"agent_id":"child-2","generation":1,"status":"done"}]}`})
			_, finished := orphanedWorkers(events)
			if len(finished) != 1 || finished[0].Collected != (caller == "parent-1") {
				t.Fatalf("actual descendant collection lost or accepted another tree: %+v", finished)
			}
		})
	}
}

func TestNuwaSubtreeCloseReceiptSurvivesColdReplay(t *testing.T) {
	events := []store.Event{
		{Kind: "spawned", AgentID: "parent-1", WorkerGeneration: 1},
		{Kind: "spawned", AgentID: "child-2", ParentID: "parent-1", WorkerGeneration: 1},
		{Kind: "tool_call", AgentID: "manager", ToolCallID: "close", Text: `close_agent({"agent_id":"parent-1"})`},
		{Kind: "tool_result", AgentID: "manager", ToolCallID: "close", Text: `{"agent_id":"parent-1","generation":1,"cancelled":true,"closed_agents":[{"agent_id":"parent-1","generation":1,"cancelled":true},{"agent_id":"child-2","generation":1,"cancelled":true}]}`},
	}
	running, finished := orphanedWorkers(events)
	if len(running) != 0 || len(finished) != 2 {
		t.Fatalf("closed descendants became running again after cold replay: running=%+v finished=%+v", running, finished)
	}
	for _, worker := range finished {
		if !worker.CloseRequested {
			t.Fatalf("subtree decision was lost for %+v", worker)
		}
	}
}

func TestNuwaLateFinishCannotStopAResumedGeneration(t *testing.T) {
	running, finished := orphanedWorkers([]store.Event{
		{Kind: "spawned", AgentID: "worker-1", WorkerGeneration: 1},
		{Kind: "finished", AgentID: "worker-1", WorkerGeneration: 1},
		{Kind: "spawned", AgentID: "worker-1", WorkerGeneration: 2},
		{Kind: "finished", AgentID: "worker-1", WorkerGeneration: 1, Err: "old transport failure"},
	})
	if len(running) != 1 || running[0].Generation != 2 || len(finished) != 0 {
		t.Fatalf("late previous generation replaced current state: running=%+v finished=%+v", running, finished)
	}
}

// A damaged or incomplete history cannot grant control over a different tree,
// and a cycle must terminate instead of hanging project/history replay.
func TestNuwaReplayRefusesMissingOrCyclicAncestry(t *testing.T) {
	for _, tc := range []struct {
		name  string
		slots map[string]*workerSlot
	}{
		{"missing worker", map[string]*workerSlot{}},
		{"missing parent", map[string]*workerSlot{"child": {parentID: "missing"}}},
		{"root without parent", map[string]*workerSlot{"child": {}}},
		{"cyclic history", map[string]*workerSlot{"child": {parentID: "parent"}, "parent": {parentID: "child"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if replayDescendant(tc.slots, "child", "unrelated") {
				t.Fatal("incomplete history granted cross-tree lifecycle control")
			}
		})
	}
}
