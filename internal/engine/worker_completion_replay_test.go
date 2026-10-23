package engine

import (
	"encoding/json"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestWorkerCollectionReplayBindsTheActualReturnedGeneration(t *testing.T) {
	spawn := []store.Event{
		{Kind: "spawned", AgentID: "verifier-1", Role: "verifier", Text: "inspect"},
		{Kind: "tool_call", AgentID: "manager", ToolCallID: "spawn", Text: `spawn_agent({"role":"verifier","task":"inspect"})`},
		{Kind: "tool_result", AgentID: "manager", ToolCallID: "spawn", Text: `{"agent_id":"verifier-1","generation":1}`},
		{Kind: "finished", AgentID: "verifier-1", Text: "verified"},
	}
	assert := func(events []store.Event, generation int, collected, closed bool) {
		t.Helper()
		running, done := orphanedWorkers(events)
		if len(running) != 0 || len(done) != 1 {
			t.Fatalf("roster=%+v %+v", running, done)
		}
		if got := done[0]; got.Generation != generation || got.Collected != collected || got.CloseRequested != closed {
			t.Fatalf("collection state=%+v want generation=%d collected=%v closed=%v", got, generation, collected, closed)
		}
	}
	assert(spawn, 1, false, false)
	wait := []store.Event{
		{Kind: "tool_call", AgentID: "manager", ToolCallID: "wait", Text: `wait_agents({"agent_ids":["verifier-1"]})`},
		{Kind: "tool_result", AgentID: "manager", ToolCallID: "wait", Text: `{"agents":[{"agent_id":"verifier-1","generation":1,"status":"done","result":"verified"}]}`},
	}
	assert(append(append([]store.Event{}, spawn...), wait...), 1, true, false)
	for _, receipt := range []string{
		`{"agents":[{"agent_id":"verifier-1","generation":1,"status":"running"}],"timed_out":true}`,
		`{"agents":[{"agent_id":"verifier-1","generation":1,"status":"unknown"}]}`,
		`{"agents":[{"agent_id":"verifier-1","generation":2,"status":"done"}]}`,
		`{"agents":[{"agent_id":"other","generation":1,"status":"done"}]}`,
		`{"agents":`,
	} {
		events := append(append([]store.Event{}, spawn...), wait[0])
		bad := wait[1]
		bad.Text = receipt
		assert(append(events, bad), 1, false, false)
	}
	resumed := append(append([]store.Event{}, spawn...),
		store.Event{Kind: "tool_call", AgentID: "manager", ToolCallID: "resume", Text: `resume_agent({"agent_id":"verifier-1","task":"check again"})`},
		store.Event{Kind: "spawned", AgentID: "verifier-1", Role: "verifier", Text: "check again"},
		store.Event{Kind: "tool_result", AgentID: "manager", ToolCallID: "resume", Text: `{"agent_id":"verifier-1","generation":2}`},
		store.Event{Kind: "finished", AgentID: "verifier-1", Text: "verified again"},
	)
	assert(append(append([]store.Event{}, resumed...), wait...), 2, false, false)
	closed := append(append([]store.Event{}, resumed...),
		store.Event{Kind: "tool_call", AgentID: "manager", ToolCallID: "close", Text: `close_agent({"agent_id":"verifier-1"})`},
		store.Event{Kind: "tool_result", AgentID: "manager", ToolCallID: "close", Text: `{"agent_id":"verifier-1","generation":2,"cancelled":false,"already_finished":true}`},
	)
	assert(closed, 2, true, false)
	wrongCaller := append([]store.Event{}, spawn...)
	for _, ev := range wait {
		ev.AgentID = "worker"
		wrongCaller = append(wrongCaller, ev)
	}
	assert(wrongCaller, 1, false, false)
	assert(append(append([]store.Event{}, spawn...), wait[1]), 1, false, false)
	wrongResult := wait[1]
	wrongResult.AgentID = "worker"
	assert(append(append(append([]store.Event{}, spawn...), wait[0]), wrongResult), 1, false, false)
}

func TestWorkerCollectionReplayHonorsCheckpointAndExplicitCancellation(t *testing.T) {
	for _, boundary := range []string{"pending", "collected", "closed", "late-wait", "failed-wait", "wrong-generation", "rejected-close", "close-cancel", "legacy"} {
		t.Run(boundary, func(t *testing.T) {
			generation := 2
			if boundary == "legacy" {
				generation = 0
			}
			checkpoint := restartCheckpoint{Version: 1, Workers: []swarm.WorkerCheckpoint{{ID: "worker-1", Role: "worker", Generation: generation, Finished: boundary != "close-cancel", Collected: boundary == "collected", CloseRequested: boundary == "closed", Result: "verified"}}}
			body, _ := json.Marshal(checkpoint)
			call := store.Event{Kind: "tool_call", AgentID: "manager", ToolCallID: "collect", Text: `wait_agents({"agent_ids":["worker-1"]})`}
			events := []store.Event{call, {Kind: KindRestartCheckpoint, Text: string(body)}}
			wantCollected := boundary == "collected" || boundary == "late-wait" || boundary == "failed-wait"
			wantClosed := boundary == "closed" || boundary == "close-cancel"
			switch boundary {
			case "late-wait", "failed-wait", "wrong-generation":
				result := `{"agents":[{"agent_id":"worker-1","generation":2,"status":"done"}]}`
				if boundary == "failed-wait" {
					result = `{"agents":[{"agent_id":"worker-1","generation":2,"status":"failed","error":"failed"}]}`
				}
				if boundary == "wrong-generation" {
					result = `{"agents":[{"agent_id":"worker-1","generation":1,"status":"done"}]}`
				}
				events = append(events, store.Event{Kind: "tool_result", AgentID: "manager", ToolCallID: "collect", Text: result})
			case "rejected-close", "close-cancel":
				events = append(events, store.Event{Kind: "tool_call", AgentID: "manager", ToolCallID: "close", Text: `close_agent({"agent_id":"worker-1"})`})
				result := `{"agent_id":"worker-1","generation":2,"cancelled":true}`
				if boundary == "rejected-close" {
					result = `{"error":"close refused"}`
				}
				events = append(events, store.Event{Kind: "tool_result", AgentID: "manager", ToolCallID: "close", Text: result})
			}
			running, done := orphanedWorkers(events)
			if len(running) != 0 || len(done) != 1 {
				t.Fatalf("closed or finished generation was restarted: %+v %+v", running, done)
			}
			got := done[0]
			if got.Generation != generation || got.Collected != wantCollected || got.CloseRequested != wantClosed {
				t.Fatalf("checkpoint collection changed: %+v", got)
			}
			if boundary == "close-cancel" && (got.Err == nil || got.Err.Error() != "context canceled") {
				t.Fatalf("explicit cancellation lost: %+v", got)
			}
		})
	}
}

func TestWorkerCollectionReplayRejectsMalformedOrUnpairedReceipts(t *testing.T) {
	slot := &workerSlot{spawned: true, generation: 2}
	for _, callText := range []string{"exec({})", "wait_agents({", `resume_agent({"agent_id":"other"})`, `wait_agents({"agent_ids":["other"]})`, `close_agent({"agent_id":"other"})`} {
		calls := workerCollectionCalls{}
		calls.note(store.Event{Kind: "tool_call", AgentID: "manager", ToolCallID: "call", Text: callText})
		calls.apply(store.Event{Kind: "tool_result", AgentID: "manager", ToolCallID: "call", Text: `{"agent_id":"worker-1","generation":2,"cancelled":true,"agents":[{"agent_id":"worker-1","generation":2,"status":"done"}]}`}, map[string]*workerSlot{"worker-1": slot})
		if slot.collected || slot.closeRequested || slot.generation != 2 {
			t.Fatalf("unrelated receipt changed current generation: %+v", slot)
		}
	}
	for _, result := range []string{`{"agent_id":"worker-1","generation":1}`, `{"agent_id":"unknown","generation":2}`, `{"agent_id":"worker-1","generation":0}`, `{"error":"refused"}`, `{`} {
		calls := workerCollectionCalls{}
		calls.note(store.Event{Kind: "tool_call", AgentID: "manager", ToolCallID: "call", Text: `spawn_agent({"role":"worker","task":"inspect"})`})
		calls.apply(store.Event{Kind: "tool_result", AgentID: "manager", ToolCallID: "call", Text: result}, map[string]*workerSlot{"worker-1": slot})
		if slot.generation != 2 {
			t.Fatalf("late spawn receipt rewound generation: %+v", slot)
		}
	}
}

func TestRestoredWorkerCollectionDoesNotInventANewGeneration(t *testing.T) {
	body, _ := json.Marshal(restartCheckpoint{Version: 1, Workers: []swarm.WorkerCheckpoint{{ID: "worker-1", Role: "worker", Generation: 4}}})
	events := []store.Event{
		{Kind: "tool_call", AgentID: "manager", ToolCallID: "old-spawn", Text: `spawn_agent({"role":"worker","task":"inspect"})`},
		{Kind: "tool_result", AgentID: "manager", ToolCallID: "old-spawn", Text: `{"agent_id":"worker-1","generation":4}`},
		{Kind: KindRestartCheckpoint, Text: string(body)},
		{Kind: "spawned", AgentID: "worker-1", Role: "worker", Text: "inspect"},
		{Kind: "finished", AgentID: "worker-1", Text: "verified"},
		{Kind: "tool_call", AgentID: "manager", ToolCallID: "wait", Text: `wait_agents({"agent_ids":["worker-1"]})`},
		{Kind: "tool_result", AgentID: "manager", ToolCallID: "wait", Text: `{"agents":[{"agent_id":"worker-1","generation":4,"status":"done","result":"verified"}]}`},
	}
	running, done := orphanedWorkers(events)
	if len(running) != 0 || len(done) != 1 || done[0].Generation != 4 || !done[0].Collected {
		t.Fatalf("Restore lifecycle invented a generation or lost its actual collection: %+v %+v", running, done)
	}
}

func TestWorkerCollectionCallsAreBoundToTheirTurnAndCheckpointEpoch(t *testing.T) {
	calls := workerCollectionCalls{}
	call := store.Event{TurnID: "old", Kind: "tool_call", AgentID: "manager", ToolCallID: "same-call", Text: `wait_agents({"agent_ids":["worker-1"]})`}
	calls.note(call)
	slot := &workerSlot{spawned: true, generation: 3}
	result := store.Event{TurnID: "new", Kind: "tool_result", AgentID: "manager", ToolCallID: "same-call", Text: `{"agents":[{"agent_id":"worker-1","generation":3,"status":"done"}]}`}
	calls.apply(result, map[string]*workerSlot{"worker-1": slot})
	if slot.collected {
		t.Fatal("another turn's colliding ID consumed this generation")
	}
	call.Text = `exec({})`
	calls.note(call)
	result.TurnID = "old"
	calls.apply(result, map[string]*workerSlot{"worker-1": slot})
	if slot.collected {
		t.Fatal("a different tool with the same call ID inherited a wait")
	}
	body, _ := json.Marshal(restartCheckpoint{Version: 1, Workers: []swarm.WorkerCheckpoint{{ID: "worker-1", Role: "worker", Generation: 3}}})
	events := []store.Event{
		{TurnID: "old", Kind: "tool_call", AgentID: "manager", ToolCallID: "spawn", Text: `spawn_agent({"role":"worker","task":"inspect"})`},
		{Kind: KindRestartCheckpoint, Text: string(body)},
		{TurnID: "old", Kind: "spawned", AgentID: "worker-1", Role: "worker", Text: "inspect"},
		{TurnID: "old", Kind: "finished", AgentID: "worker-1", Text: "verified"},
		{TurnID: "old", Kind: "tool_call", AgentID: "manager", ToolCallID: "wait", Text: `wait_agents({"agent_ids":["worker-1"]})`},
		{TurnID: "old", Kind: "tool_result", AgentID: "manager", ToolCallID: "wait", Text: `{"agents":[{"agent_id":"worker-1","generation":3,"status":"done"}]}`},
	}
	_, done := orphanedWorkers(events)
	if len(done) != 1 || done[0].Generation != 3 || !done[0].Collected {
		t.Fatalf("a pre-checkpoint pending spawn re-advanced the restored generation: %+v", done)
	}
}

func TestWorkerCollectionReplayUsesTheToolTargetNormalization(t *testing.T) {
	calls := workerCollectionCalls{}
	slot := &workerSlot{spawned: true, generation: 3}
	call := store.Event{Kind: "tool_call", AgentID: "manager", ToolCallID: "call", Text: `close_agent({"agent_id":" worker-1 "})`}
	calls.note(call)
	calls.apply(store.Event{Kind: "tool_result", AgentID: "manager", ToolCallID: "call", Text: `{"agent_id":"worker-1","generation":3,"already_finished":true}`}, map[string]*workerSlot{"worker-1": slot})
	if !slot.collected {
		t.Fatal("successful normalized close was lost during replay")
	}
	call.Text = `spawn_agent({"role":" worker ","task":"inspect"})`
	calls.note(call)
	if !calls.startsGeneration(store.Event{Kind: "spawned", AgentID: "worker-1", Role: "worker"}) {
		t.Fatal("actual normalized role lost new generation identity")
	}
	calls.apply(store.Event{Kind: "tool_result", AgentID: "manager", ToolCallID: "call", Err: "tool failed"}, nil)
	if calls.startsGeneration(store.Event{Kind: "spawned", AgentID: "worker-1", Role: "worker"}) {
		t.Fatal("a failed creation call granted a restored generation")
	}
}
