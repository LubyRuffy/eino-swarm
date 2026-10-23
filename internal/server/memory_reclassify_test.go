package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestRequestedMemoryTidyRetainsMovesInTheHTTPTrace(t *testing.T) {
	h := newHarness(t)
	id := h.newProject(map[string]any{"name": "Recorded memory"})["id"].(string)
	th, err := h.app.Engine.CreateThread("memory source", "", id)
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, UserText: "Recorded source"}
	if err := h.app.Store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := h.app.Store.FinishTurn(turn.ID, store.TurnDone, "Recorded source", ""); err != nil {
		t.Fatal(err)
	}
	const note = "1. Inspect the saved inputs.\n2. Verify the resulting records."
	h.json(http.MethodPut, "/api/projects/"+id+"/memory", map[string]any{"text": note}, http.StatusOK)
	got := h.json(http.MethodPost, "/api/projects/"+id+"/memory/tidy-skills", nil, http.StatusOK)
	report := got["report"].(map[string]any)
	if report["promoted"] != float64(1) || report["demoted"] != float64(0) || got["folded"] != true {
		t.Fatalf("report=%v", got)
	}
	trace := h.json(http.MethodGet, "/api/trace/"+turn.ID, nil, http.StatusOK)
	found := false
	for _, raw := range trace["events"].([]any) {
		ev := raw.(map[string]any)
		if ev["kind"] == engine.KindMemoryReview {
			var out struct {
				Changes []struct {
					Action string `json:"action"`
				} `json:"changes"`
			}
			if err := json.Unmarshal([]byte(ev["text"].(string)), &out); err != nil {
				t.Fatal(err)
			}
			for _, c := range out.Changes {
				if c.Action == "promote" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("HTTP Trace lost requested move")
	}
}
