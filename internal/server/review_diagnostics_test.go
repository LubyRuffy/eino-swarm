package server_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A failed background review must remain explainable by the original turn ID,
// without changing its successful terminal state or publishing partial notes.
func TestReviewCapacityFailureIsExplainedByTheOriginalTurnTrace(t *testing.T) {
	h := newHarness(t)
	const original = "Keep detailed reports with supporting source references."
	h.json(http.MethodPut, "/api/settings", map[string]any{
		"memory": map[string]any{
			"enabled": true, "auto_review": true,
			"char_limit": len(original), "entry_max": len(original),
			"review_max_iterations": 1,
		},
	}, http.StatusOK)
	projectID := h.newProject(map[string]any{"name": "Review diagnostics"})["id"].(string)
	h.json(http.MethodPut, "/api/projects/"+projectID+"/memory", map[string]any{"text": original}, http.StatusOK)
	threadID := h.json(http.MethodPost, "/api/threads", map[string]any{"project_id": projectID}, http.StatusCreated)["thread"].(map[string]any)["id"].(string)
	h.json(http.MethodPost, "/api/threads/"+threadID+"/turns", map[string]any{"text": "Use concise reports."}, http.StatusAccepted)
	turn := h.waitTurnDone(threadID)
	h.waitForReviewEvent(turn.ID)

	trace := h.json(http.MethodGet, "/api/trace/"+turn.ID, nil, http.StatusOK)
	found := false
	for _, raw := range trace["events"].([]any) {
		event := raw.(map[string]any)
		if event["kind"] != engine.KindMemoryReview {
			continue
		}
		found = true
		if event["turn_id"] != turn.ID || event["agent_id"] != engine.ReviewAgentID {
			t.Fatalf("review diagnostics escaped their original turn: %v", event)
		}
		var outcome struct {
			Changed      bool   `json:"changed"`
			StopReason   string `json:"stop_reason"`
			AttemptCount int    `json:"attempt_count"`
			Attempts     []struct {
				Status string `json:"status"`
			} `json:"attempts"`
			Refused []struct {
				Code   string `json:"code"`
				Usage  string `json:"usage"`
				OverBy int    `json:"over_by"`
			} `json:"refused"`
		}
		text := event["text"].(string)
		if err := json.Unmarshal([]byte(text), &outcome); err != nil {
			t.Fatal(err)
		}
		if outcome.Changed || outcome.StopReason != "iteration_limit" || outcome.AttemptCount != 1 ||
			len(outcome.Attempts) != 1 || outcome.Attempts[0].Status != "refused" ||
			len(outcome.Refused) != 1 || outcome.Refused[0].Code != "capacity" ||
			outcome.Refused[0].Usage == "" || outcome.Refused[0].OverBy <= 0 {
			t.Fatalf("Trace did not retain the actual failure: %s", text)
		}
		if strings.Contains(text, original) || strings.Contains(text, "current_entries") || strings.Contains(text, "\"quote\"") {
			t.Fatalf("repair-only memory content leaked into Trace: %s", text)
		}
	}
	latest, err := h.app.Store.GetTurn(turn.ID)
	if err != nil || !found || latest.Status != store.TurnDone {
		t.Fatalf("the background review changed the primary result: turn=%+v found=%v err=%v", latest, found, err)
	}
	memory := h.json(http.MethodGet, "/api/projects/"+projectID+"/memory", nil, http.StatusOK)["memory"].(map[string]any)["memory"].(map[string]any)
	if memory["text"] != original {
		t.Fatalf("a refused review changed live notes: %v", memory)
	}
}
