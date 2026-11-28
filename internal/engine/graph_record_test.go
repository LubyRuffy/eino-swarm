package engine

import (
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestGraphRecordAllowanceIsOnePassOnTheTurnLog(t *testing.T) {
	if graphRecordUsed(nil) || graphRecordUsed([]store.Event{{Kind: KindModelRetry, AgentID: "manager", Text: "{"}}) {
		t.Fatal("an empty or unreadable log spent the recording pass")
	}
	other := store.Event{Kind: KindModelRetry, AgentID: "manager", Text: `{"reason":"workers_uncollected"}`}
	worker := store.Event{Kind: KindModelRetry, AgentID: "worker-1", Text: `{"reason":"graph_unrecorded"}`}
	if graphRecordUsed([]store.Event{other, worker}) {
		t.Fatal("another retry, or a worker's copy, spent the manager's pass")
	}
	spent := store.Event{Kind: KindModelRetry, AgentID: "manager", Text: `{"attempt":1,"cap":1,"reason":"graph_unrecorded"}`}
	if !graphRecordUsed([]store.Event{other, spent}) {
		t.Fatal("the recorded pass was still available")
	}
}

func TestGraphRecordIsDueOnlyForAnOpenWritableTurnWithResults(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("graph record fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, GraphTools: true}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if e.graphRecordDue(t.Context(), turn, false) || e.graphRecordContinued(turn) {
		t.Fatal("a turn with no tool result was sent back to record")
	}
	readOnly := *turn
	readOnly.GraphReadOnly = true
	if e.graphRecordDue(t.Context(), &readOnly, false) || e.graphRecordContinued(&readOnly) {
		t.Fatal("a read-only turn was asked to write")
	}
	off := *turn
	off.GraphTools = false
	if e.graphRecordDue(t.Context(), &off, false) {
		t.Fatal("a turn without the graph was asked to write")
	}
	if e.graphRecordDue(t.Context(), nil, false) || e.graphRecordContinued(nil) {
		t.Fatal("a missing turn was asked to write")
	}
	ev := &store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "tool_result", AgentID: "manager", Text: "noted"}
	if err := e.store.AppendEvent(ev); err != nil {
		t.Fatal(err)
	}
	if !e.graphRecordDue(t.Context(), turn, false) {
		t.Fatal("a tool result was left off the graph with the pass still available")
	}
	if e.graphRecordDue(t.Context(), turn, true) {
		t.Fatal("a spent pass was granted again")
	}
	if !(&runtime{engine: e}).recordGraphRecordRetry(turn) {
		t.Fatal("could not persist the recording pass")
	}
	if !e.graphRecordContinued(turn) || e.graphRecordDue(t.Context(), turn, e.graphRecordContinued(turn)) {
		t.Fatal("the persisted pass did not stick")
	}
}
