package server_test

import (
	"net/http"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestLogRetainsWorkerOwnershipWhenTheReceiptFallsOutsideTheViewport(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	for _, event := range []store.Event{
		{Kind: "spawned", AgentID: "worker-7"},
		{Kind: "workers_retained", AgentID: "manager", Text: `{"reason":"scheduled_check_failed","owner_kind":"thread_wake","owner_id":"wake-7","worker_ids":["worker-7"]}`},
		{Kind: "error", AgentID: "manager", Err: "scheduled check failed"},
	} {
		event.ThreadID, event.TurnID = id, "check-7"
		if err := h.app.Store.AppendEvent(&event); err != nil {
			t.Fatal(err)
		}
	}
	got := h.json(http.MethodGet, "/api/threads/"+id+"/log?limit=1", nil, http.StatusOK)
	page := got["events"].([]any)
	if len(page) != 1 || page[0].(map[string]any)["kind"] != "error" {
		t.Fatalf("page=%v", page)
	}
	roster := got["roster"].([]any)
	if len(roster) != 2 || roster[1].(map[string]any)["kind"] != "workers_retained" {
		t.Fatalf("the error viewport lost its worker ownership proof: %v", roster)
	}
	if roster[1].(map[string]any)["turn_id"] != "check-7" {
		t.Fatal("ownership evidence lost its turn identity")
	}
	full := h.json(http.MethodGet, "/api/threads/"+id+"/log?limit=20", nil, http.StatusOK)
	if len(full["roster"].([]any)) != 0 {
		t.Fatal("in-page receipt was repeated in roster")
	}
}
