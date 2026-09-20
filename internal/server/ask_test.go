package server_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestAnswerTurnEndpointIdleIsConflict(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	got := h.json(http.MethodPost, "/api/threads/"+id+"/answers",
		map[string]any{"text": "x"}, http.StatusConflict)
	if got["code"] != "idle" {
		t.Fatalf("code=%v", got["code"])
	}
}

func TestAnswerTurnEndpointContinuesTheSameTurn(t *testing.T) {
	provider.SetMockAskUser(true)
	t.Cleanup(func() { provider.SetMockAskUser(false) })

	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "choose an approach"}, http.StatusAccepted)

	deadline := time.Now().Add(15 * time.Second)
	var waiting bool
	for time.Now().Before(deadline) {
		got := h.json(http.MethodGet, "/api/threads/"+id, nil, http.StatusOK)
		status, _ := got["status"].(map[string]any)
		if status["awaiting_answer"] == true {
			waiting = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("the turn never paused for ask_user")
	}
	listed := h.json(http.MethodGet, "/api/threads", nil, http.StatusOK)
	rows, _ := listed["threads"].([]any)
	var sawAsk bool
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["id"] == id && row["awaiting_answer"] == true && row["running"] == true {
			sawAsk = true
			break
		}
	}
	if !sawAsk {
		t.Fatalf("the listing must mark the blocked row so the sidebar is not a working pulse: %+v", rows)
	}
	h.json(http.MethodPost, "/api/threads/"+id+"/answers",
		map[string]any{"text": "use the existing layout"}, http.StatusAccepted)
	turn := h.waitTurnDone(id)
	if turn.Status != store.TurnDone {
		t.Fatalf("status=%s err=%s", turn.Status, turn.Error)
	}
}

func TestPlanImplementEndpoint(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodPost, "/api/threads/"+id+"/plan/implement", nil, http.StatusBadRequest)

	h.json(http.MethodPatch, "/api/threads/"+id, map[string]any{"plan_mode": true}, http.StatusOK)
	got := h.json(http.MethodGet, "/api/threads/"+id, nil, http.StatusOK)
	th, _ := got["thread"].(map[string]any)
	if th["plan_mode"] != true {
		t.Fatalf("thread=%v", th)
	}

	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "draft the approach"}, http.StatusAccepted)
	_ = h.waitTurnDone(id)

	h.json(http.MethodPost, "/api/threads/"+id+"/plan/implement", nil, http.StatusAccepted)
	got = h.json(http.MethodGet, "/api/threads/"+id, nil, http.StatusOK)
	th, _ = got["thread"].(map[string]any)
	if th["plan_mode"] == true {
		t.Fatal("implement must leave planning")
	}
}

func TestPlanMarkdownPatch(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodPatch, "/api/threads/"+id,
		map[string]any{"plan_markdown": "# Plan\n"}, http.StatusBadRequest)
	h.json(http.MethodPatch, "/api/threads/"+id, map[string]any{"plan_mode": true}, http.StatusOK)
	h.json(http.MethodPatch, "/api/threads/"+id,
		map[string]any{"plan_markdown": "# Plan\n\nDo the work.\n"}, http.StatusOK)
	got := h.json(http.MethodGet, "/api/threads/"+id, nil, http.StatusOK)
	th, _ := got["thread"].(map[string]any)
	md, _ := th["plan_markdown"].(string)
	if !strings.Contains(md, "Do the work.") {
		t.Fatalf("markdown=%q", md)
	}
}
