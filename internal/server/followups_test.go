package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func enqueueWhileRunning(t *testing.T, h *harness, threadID, text string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp := h.do(http.MethodPost, "/api/threads/"+threadID+"/followups",
			map[string]any{"text": text})
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		if resp.StatusCode == http.StatusAccepted {
			fu, _ := body["followup"].(map[string]any)
			if fu["id"] == "" {
				t.Fatalf("enqueue returned no id: %s", raw)
			}
			return fu
		}
		if resp.StatusCode == http.StatusConflict && body["code"] == "idle" {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		t.Fatalf("enqueue status %d: %s", resp.StatusCode, raw)
	}
	t.Fatal("never managed to queue a follow-up while a turn was running")
	return nil
}

func TestFollowupQueueWaitsThenStartsTheNextTurn(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "do the first piece"}, http.StatusAccepted)

	fu := enqueueWhileRunning(t, h, id, "then the next piece")
	listed := h.json(http.MethodGet, "/api/threads/"+id+"/followups", nil, http.StatusOK)
	rows, _ := listed["followups"].([]any)
	if len(rows) != 1 {
		t.Fatalf("want one queued follow-up, got %v", listed)
	}

	h.waitTurnDone(id)
	deadline := time.Now().Add(30 * time.Second)
	var turns []store.Turn
	for time.Now().Before(deadline) {
		var err error
		turns, err = h.app.Store.ListTurns(id)
		if err != nil {
			t.Fatal(err)
		}
		if len(turns) >= 2 {
			if turns[1].Status == store.TurnRunning {
				h.waitTurnDone(id)
				continue
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(turns) < 2 || turns[1].UserText != "then the next piece" {
		t.Fatalf("follow-up did not become the next turn: %+v", turns)
	}
	empty := h.json(http.MethodGet, "/api/threads/"+id+"/followups", nil, http.StatusOK)
	if rows, _ = empty["followups"].([]any); len(rows) != 0 {
		t.Fatalf("queue still populated after flush: %v", empty)
	}
	if fu["text"] != "then the next piece" {
		t.Fatalf("enqueue body: %v", fu)
	}
}

func TestSteerFollowupInjectsThroughTheAPI(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "do the first piece"}, http.StatusAccepted)
	fu := enqueueWhileRunning(t, h, id, "narrow it")
	fid, _ := fu["id"].(string)

	h.json(http.MethodPost, "/api/threads/"+id+"/followups/"+fid+"/steer",
		nil, http.StatusAccepted)
	listed := h.json(http.MethodGet, "/api/threads/"+id+"/followups", nil, http.StatusOK)
	if rows, _ := listed["followups"].([]any); len(rows) != 0 {
		t.Fatalf("steered follow-up still queued: %v", listed)
	}
	h.waitTurnDone(id)
}

func TestDeleteFollowupAndIdleEnqueue(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	idle := h.json(http.MethodPost, "/api/threads/"+id+"/followups",
		map[string]any{"text": "later"}, http.StatusConflict)
	if idle["code"] != "idle" {
		t.Fatalf("idle enqueue: %v", idle)
	}
	h.json(http.MethodPost, "/api/threads/"+id+"/followups",
		map[string]any{"text": "  "}, http.StatusBadRequest)

	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "do the first piece"}, http.StatusAccepted)
	fu := enqueueWhileRunning(t, h, id, "drop me")
	fid, _ := fu["id"].(string)
	h.json(http.MethodDelete, "/api/threads/"+id+"/followups/"+fid, nil, http.StatusNoContent)
	h.json(http.MethodDelete, "/api/threads/"+id+"/followups/"+fid, nil, http.StatusNotFound)
	h.json(http.MethodPost, "/api/threads/"+id+"/interrupt", nil, http.StatusAccepted)
	h.waitTurnDone(id)
}

func TestRequeueFollowupMovesToTheBackThroughTheAPI(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "do the first piece"}, http.StatusAccepted)
	first := enqueueWhileRunning(t, h, id, "first in line")
	second := enqueueWhileRunning(t, h, id, "second in line")
	fid, _ := first["id"].(string)

	patched := h.json(http.MethodPatch, "/api/threads/"+id+"/followups/"+fid,
		map[string]any{"text": "  first edited  "}, http.StatusOK)
	fu, _ := patched["followup"].(map[string]any)
	if fu["id"] != fid || fu["text"] != "first edited" {
		t.Fatalf("requeue body: %v", patched)
	}
	listed := h.json(http.MethodGet, "/api/threads/"+id+"/followups", nil, http.StatusOK)
	rows, _ := listed["followups"].([]any)
	if len(rows) != 2 {
		t.Fatalf("want two queued follow-ups, got %v", listed)
	}
	a, _ := rows[0].(map[string]any)
	b, _ := rows[1].(map[string]any)
	if a["id"] != second["id"] || a["text"] != "second in line" {
		t.Fatalf("front after edit: %v", a)
	}
	if b["id"] != fid || b["text"] != "first edited" {
		t.Fatalf("back after edit: %v", b)
	}

	h.json(http.MethodPatch, "/api/threads/"+id+"/followups/"+fid,
		map[string]any{"text": "   "}, http.StatusBadRequest)
	h.json(http.MethodPatch, "/api/threads/"+id+"/followups/fu_missing",
		map[string]any{"text": "x"}, http.StatusNotFound)
	h.json(http.MethodPost, "/api/threads/"+id+"/interrupt", nil, http.StatusAccepted)
	h.waitTurnDone(id)
}
