package server_test

import (
	"net/http"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A list refresh and an automatic detail load are not a human reading the
// answer. Only the explicit watermark acknowledgment may remove attention.
func TestCompletedConversationStaysUnreadUntilExactReadAcknowledgment(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	finish := func() int64 {
		turn := &store.Turn{ThreadID: id}
		if err := h.app.Store.CreateTurn(turn); err != nil {
			t.Fatal(err)
		}
		ev := &store.Event{ThreadID: id, TurnID: turn.ID, AgentID: swarm.DefaultManagerID, Kind: "done"}
		if err := h.app.Store.AppendEvent(ev); err != nil {
			t.Fatal(err)
		}
		return ev.Seq
	}
	seq := finish()
	get := func() map[string]any {
		return h.json(http.MethodGet, "/api/threads/"+id, nil, http.StatusOK)["thread"].(map[string]any)
	}
	if got := get(); got["unread"] != true || got["completed_seq"] != float64(seq) {
		t.Fatalf("completed answer disappeared without a read: %+v", got)
	}
	listed := h.json(http.MethodGet, "/api/threads", nil, http.StatusOK)["threads"].([]any)
	if row := listed[0].(map[string]any); row["unread"] != true {
		t.Fatalf("listing lost completion: %+v", row)
	}
	newer := finish()
	h.json(http.MethodPost, "/api/threads/"+id+"/read", map[string]any{"completed_seq": seq}, http.StatusOK)
	if got := get(); got["unread"] != true || got["completed_seq"] != float64(newer) {
		t.Fatalf("old read swallowed a newer completion: %+v", got)
	}
	h.json(http.MethodPost, "/api/threads/"+id+"/read", map[string]any{"completed_seq": newer}, http.StatusOK)
	if got := get(); got["unread"] != false {
		t.Fatalf("viewed answer stayed unread: %+v", got)
	}
	// Retrying or arriving out of order cannot regress the persistent cursor.
	h.json(http.MethodPost, "/api/threads/"+id+"/read", map[string]any{"completed_seq": seq}, http.StatusOK)
	if got := get(); got["unread"] != false {
		t.Fatalf("old acknowledgment regressed read: %+v", got)
	}
}

func TestCompletionReadRejectsInvalidAndMissingConversation(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	for _, body := range []any{map[string]any{}, map[string]any{"completed_seq": -1}, map[string]any{"completed_seq": "x"}} {
		h.json(http.MethodPost, "/api/threads/"+id+"/read", body, http.StatusBadRequest)
	}
	h.json(http.MethodPost, "/api/threads/"+id+"/read", map[string]any{"completed_seq": 99}, http.StatusBadRequest)
	h.json(http.MethodPost, "/api/threads/missing/read", map[string]any{"completed_seq": 1}, http.StatusNotFound)
}

func TestCompletionReadResponseHandlesConcurrentConversationDeletion(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	turn := &store.Turn{ThreadID: id}
	if err := h.app.Store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	ev := &store.Event{ThreadID: id, TurnID: turn.ID, Kind: "done"}
	if err := h.app.Store.AppendEvent(ev); err != nil {
		t.Fatal(err)
	}
	// Deterministically model a delete between the acknowledgment and the
	// response snapshot; a missing conversation must not produce stale detail.
	if err := h.app.Store.DB().Exec(`CREATE TRIGGER delete_after_read AFTER UPDATE OF read_completed_seq ON threads BEGIN DELETE FROM threads WHERE id = NEW.id; END`).Error; err != nil {
		t.Fatal(err)
	}
	h.json(http.MethodPost, "/api/threads/"+id+"/read", map[string]any{"completed_seq": ev.Seq}, http.StatusNotFound)
}
