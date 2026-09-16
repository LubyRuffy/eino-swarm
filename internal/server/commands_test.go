package server_test

import (
	"net/http"
	"testing"
)

func TestGoalRoundTripsOnTheConversation(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()

	set := h.json(http.MethodPatch, "/api/threads/"+id,
		map[string]any{"goal": "  keep pursuing this  "}, http.StatusOK)
	thread := set["thread"].(map[string]any)
	if thread["goal"] != "keep pursuing this" {
		t.Fatalf("goal not kept: %v", thread)
	}
	if thread["goal_complete"] != false {
		t.Fatalf("a freshly set goal must be open: %v", thread)
	}
	if thread["goal_blocked"] != false {
		t.Fatalf("a freshly set goal must not be blocked: %v", thread)
	}
	if thread["goal_started_at"] == nil || thread["goal_started_at"] == "" {
		t.Fatal("setting a goal must stamp when it started")
	}

	got := h.json(http.MethodGet, "/api/threads/"+id, nil, http.StatusOK)
	if got["thread"].(map[string]any)["goal"] != "keep pursuing this" {
		t.Fatalf("GET lost the goal: %v", got)
	}
	if _, ok := got["thread"].(map[string]any)["context_budget"]; !ok {
		t.Fatal("GET must report how full the replay looks")
	}

	cleared := h.json(http.MethodPatch, "/api/threads/"+id,
		map[string]any{"goal": ""}, http.StatusOK)
	if cleared["thread"].(map[string]any)["goal"] != "" {
		t.Fatalf("clear left %v", cleared)
	}
}

func TestGoalEditKeepsABlockAndResumeStartsATurn(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodPatch, "/api/threads/"+id,
		map[string]any{"goal": "keep pursuing this"}, http.StatusOK)
	if err := h.app.Engine.BlockThreadGoal(id, "needs an external change"); err != nil {
		t.Fatal(err)
	}

	edited := h.json(http.MethodPatch, "/api/threads/"+id,
		map[string]any{"goal": "keep pursuing this, tighter", "goal_edit": true},
		http.StatusOK)
	thread := edited["thread"].(map[string]any)
	if thread["goal"] != "keep pursuing this, tighter" {
		t.Fatalf("edit lost the text: %v", thread)
	}
	if thread["goal_blocked"] != true {
		t.Fatalf("edit must keep the block: %v", thread)
	}

	resumed := h.json(http.MethodPatch, "/api/threads/"+id,
		map[string]any{"goal_resume": true}, http.StatusOK)
	thread = resumed["thread"].(map[string]any)
	if thread["goal_blocked"] != false {
		t.Fatalf("resume must clear the block: %v", thread)
	}
	if thread["running"] != true {
		t.Fatalf("resume must start a turn: %v", thread)
	}
	h.json(http.MethodPost, "/api/threads/"+id+"/interrupt", nil, http.StatusAccepted)
	h.waitTurnDone(id)
}

func TestGoalResumeWithoutOneIsRejected(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodPatch, "/api/threads/"+id,
		map[string]any{"goal_resume": true}, http.StatusBadRequest)
}

func TestCompactEndpointFoldsReplay(t *testing.T) {
	h := newHarness(t)
	h.app.Config.Swarm.CompactKeepMessages = 2
	id := h.newThread()

	empty := h.json(http.MethodPost, "/api/threads/"+id+"/compact", nil, http.StatusConflict)
	if empty["code"] != "nothing_to_compact" {
		t.Fatalf("empty compact code: %v", empty)
	}

	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "the first request"}, http.StatusAccepted)
	h.waitTurnDone(id)
	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "the second request"}, http.StatusAccepted)
	h.waitTurnDone(id)

	got := h.json(http.MethodPost, "/api/threads/"+id+"/compact", nil, http.StatusOK)
	thread := got["thread"].(map[string]any)
	if thread["compacted"] != true {
		t.Fatalf("compacted flag: %v", thread)
	}
	if thread["context_chars"] == nil {
		t.Fatal("compact response must include how full the replay is")
	}

	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "still running"}, http.StatusAccepted)
	h.json(http.MethodPost, "/api/threads/"+id+"/compact", nil, http.StatusConflict)
	h.json(http.MethodPost, "/api/threads/"+id+"/interrupt", nil, http.StatusAccepted)
	h.waitTurnDone(id)
}
