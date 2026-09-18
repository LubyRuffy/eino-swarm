package server_test

import (
	"net/http"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestThreadLogReturnsTheLatestPage(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	for i := 0; i < 5; i++ {
		if err := h.app.Store.AppendEvent(&store.Event{
			ThreadID: id, Kind: "user_message", Text: "row",
		}); err != nil {
			t.Fatal(err)
		}
	}

	got := h.json(http.MethodGet, "/api/threads/"+id+"/log?limit=2", nil, http.StatusOK)
	events, _ := got["events"].([]any)
	if len(events) != 2 {
		t.Fatalf("events=%v", got["events"])
	}
	if got["has_more"] != true {
		t.Fatalf("has_more=%v", got["has_more"])
	}
	first := events[0].(map[string]any)
	second := events[1].(map[string]any)
	if first["seq"] != float64(4) || second["seq"] != float64(5) {
		t.Fatalf("want seq 4 then 5, got %+v %+v", first, second)
	}

	older := h.json(http.MethodGet, "/api/threads/"+id+"/log?before=4&limit=2", nil, http.StatusOK)
	olderEvents, _ := older["events"].([]any)
	if len(olderEvents) != 2 || older["has_more"] != true {
		t.Fatalf("older=%v", older)
	}
	if olderEvents[0].(map[string]any)["seq"] != float64(2) {
		t.Fatalf("older starts at seq 2: %v", olderEvents[0])
	}
	if _, ok := older["roster"]; ok {
		t.Fatalf("older pages must not carry a roster: %v", older["roster"])
	}
}

func TestThreadLogIsEmptyOnANewConversation(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	got := h.json(http.MethodGet, "/api/threads/"+id+"/log", nil, http.StatusOK)
	events, ok := got["events"].([]any)
	if !ok || len(events) != 0 {
		t.Fatalf("empty log must be an array: %v", got["events"])
	}
	if got["has_more"] != false {
		t.Fatalf("has_more=%v", got["has_more"])
	}
	roster, ok := got["roster"].([]any)
	if !ok || len(roster) != 0 {
		t.Fatalf("live-edge roster must be an array: %v", got["roster"])
	}
}

func TestThreadLogClampsAHugeLimit(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	for i := 0; i < 3; i++ {
		if err := h.app.Store.AppendEvent(&store.Event{
			ThreadID: id, Kind: "user_message", Text: "row",
		}); err != nil {
			t.Fatal(err)
		}
	}
	got := h.json(http.MethodGet, "/api/threads/"+id+"/log?limit=99999", nil, http.StatusOK)
	events, _ := got["events"].([]any)
	if len(events) != 3 || got["has_more"] != false {
		t.Fatalf("a clamped limit still returns the rows that exist: %v", got)
	}
}

func TestThreadLogRosterIsTheWorkersOutsideThePage(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	rows := []store.Event{
		{ThreadID: id, Kind: "user_message", AgentID: "manager", Text: "go"},
		{ThreadID: id, Kind: "spawned", AgentID: "worker-1", Role: "worker"},
		{ThreadID: id, Kind: "finished", AgentID: "worker-1", Text: "done"},
		{ThreadID: id, Kind: "cleanup", AgentID: "manager", Text: "stopped 0"},
	}
	for _, ev := range rows {
		if err := h.app.Store.AppendEvent(&ev); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 4; i++ {
		if err := h.app.Store.AppendEvent(&store.Event{
			ThreadID: id, Kind: "tool_call", AgentID: "manager", Text: "exec",
		}); err != nil {
			t.Fatal(err)
		}
	}

	got := h.json(http.MethodGet, "/api/threads/"+id+"/log?limit=2", nil, http.StatusOK)
	events, _ := got["events"].([]any)
	if len(events) != 2 {
		t.Fatalf("events=%v", got["events"])
	}
	if events[0].(map[string]any)["kind"] != "tool_call" {
		t.Fatalf("tail should be the recent tools: %v", events[0])
	}
	roster, _ := got["roster"].([]any)
	if len(roster) != 3 {
		t.Fatalf("roster=%v", got["roster"])
	}
	if roster[0].(map[string]any)["kind"] != "spawned" {
		t.Fatalf("roster starts at spawned: %v", roster[0])
	}
	if roster[1].(map[string]any)["kind"] != "finished" {
		t.Fatalf("roster keeps finished: %v", roster[1])
	}
	if roster[2].(map[string]any)["kind"] != "cleanup" {
		t.Fatalf("roster keeps cleanup: %v", roster[2])
	}

	inPage := h.json(http.MethodGet, "/api/threads/"+id+"/log?limit=20", nil, http.StatusOK)
	dupes, _ := inPage["roster"].([]any)
	if len(dupes) != 0 {
		t.Fatalf("rows already on the page are not a sidecar: %v", inPage["roster"])
	}
}

func TestThreadAgentLogIsThatWorkerOnly(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	rows := []store.Event{
		{ThreadID: id, Kind: "user_message", AgentID: "manager", Text: "go"},
		{ThreadID: id, Kind: "spawned", AgentID: "worker-1", Role: "worker"},
		{ThreadID: id, Kind: "tool_call", AgentID: "worker-1", Text: "exec"},
		{ThreadID: id, Kind: "tool_call", AgentID: "worker-2", Text: "exec"},
		{ThreadID: id, Kind: "finished", AgentID: "worker-1", Text: "done"},
	}
	for _, ev := range rows {
		if err := h.app.Store.AppendEvent(&ev); err != nil {
			t.Fatal(err)
		}
	}

	got := h.json(http.MethodGet, "/api/threads/"+id+"/agents/worker-1/log", nil, http.StatusOK)
	events, _ := got["events"].([]any)
	if len(events) != 3 {
		t.Fatalf("events=%v", got["events"])
	}
	if events[0].(map[string]any)["kind"] != "spawned" {
		t.Fatalf("starts at spawned: %v", events[0])
	}
	if events[1].(map[string]any)["kind"] != "tool_call" {
		t.Fatalf("keeps the worker's tools: %v", events[1])
	}

	empty := h.json(http.MethodGet, "/api/threads/"+id+"/agents/worker-missing/log", nil, http.StatusOK)
	none, _ := empty["events"].([]any)
	if len(none) != 0 {
		t.Fatalf("missing worker must be an empty list: %v", empty["events"])
	}

	h.json(http.MethodGet, "/api/threads/th_missing/agents/worker-1/log", nil, http.StatusNotFound)
}
