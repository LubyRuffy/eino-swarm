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
