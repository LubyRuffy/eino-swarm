package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestCompactAPIPresentsOnlyTheBriefingBody(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	turn := &store.Turn{ThreadID: id, Status: store.TurnDone, UserText: "continue"}
	if err := h.app.Store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	const prose = "Continue the current investigation."
	const summary = "<zwai-worker-lifecycle-evidence>\n{\"source\":\"persisted_worker_lifecycle\",\"new_workers\":0}\n</zwai-worker-lifecycle-evidence>\n\nConversation briefing (model-written claims, not execution evidence):\n" + prose
	if err := h.app.Store.UpdateThread(id, map[string]any{"session_memory": summary}); err != nil {
		t.Fatal(err)
	}
	h.json(http.MethodPost, "/api/threads/"+id+"/compact", nil, http.StatusOK)
	got := h.json(http.MethodGet, "/api/threads/"+id, nil, http.StatusOK)
	thread := got["thread"].(map[string]any)
	for _, private := range []string{"compact_summary", "session_memory"} {
		if _, exposed := thread[private]; exposed {
			t.Fatalf("the thread view exposed %s", private)
		}
	}
	trace := h.json(http.MethodGet, "/api/trace/"+turn.ID, nil, http.StatusOK)
	found := false
	for _, raw := range trace["events"].([]any) {
		event := raw.(map[string]any)
		if event["kind"] != engine.KindCompacted {
			continue
		}
		found = true
		var payload struct {
			Summary string `json:"summary"`
		}
		if err := json.Unmarshal([]byte(event["text"].(string)), &payload); err != nil || payload.Summary != prose {
			t.Fatalf("the compact event exposed engine metadata in the user briefing: %+v, %v", payload, err)
		}
	}
	stored, _ := h.app.Store.GetThread(id)
	if !found || stored.CompactSummary != summary {
		t.Fatal("presentation removed evidence from storage or lost the compact event")
	}
}
