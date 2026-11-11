package server_test

import (
	"net/http"
	"testing"
)

func TestRetryWithoutAFailedTurnIsConflict(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	got := h.json(http.MethodPost, "/api/threads/"+id+"/retry", map[string]any{"turn_id": "tn_missing"}, http.StatusConflict)
	if got["code"] != "no_failed_turn" {
		t.Fatalf("missing failure: %+v", got)
	}
	h.json(http.MethodPost, "/api/threads/"+id+"/retry", map[string]any{}, http.StatusBadRequest)
	h.json(http.MethodPost, "/api/threads/"+id+"/retry", map[string]any{"turn_id": "   "}, http.StatusBadRequest)
}
