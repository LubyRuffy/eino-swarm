package server_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestSteerFollowupHTTPRejectsMissingOrForeignQueuedMessages(t *testing.T) {
	h := newHarness(t)
	first, other := h.newThread(), h.newThread()
	f, err := h.app.Store.EnqueueFollowup(first, "only this conversation may consume me")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/api/threads/missing/followups/" + f.ID + "/steer",
		"/api/threads/" + first + "/followups/missing/steer",
		"/api/threads/" + other + "/followups/" + f.ID + "/steer",
	} {
		body := h.json(http.MethodPost, path, nil, http.StatusNotFound)
		message, ok := body["error"].(string)
		if !ok || message == "" || body["steered"] != nil || body["turn"] != nil {
			t.Fatalf("missing row looked accepted: %+v", body)
		}
	}
	rows, err := h.app.Store.ListFollowups(first)
	if err != nil || len(rows) != 1 || rows[0].ID != f.ID || rows[0].Text != f.Text {
		t.Fatalf("foreign steering consumed a row: %+v err=%v", rows, err)
	}
	for _, id := range []string{first, other} {
		turns, err := h.app.Store.ListTurns(id)
		if err != nil || len(turns) != 0 {
			t.Fatalf("rejected HTTP request started work: %+v err=%v", turns, err)
		}
	}
}

func TestSteerFollowupHTTPReportsStorageFailureAndKeepsTheQueue(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	f, err := h.app.Store.EnqueueFollowup(id, "keep this request")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.app.Store.DB().Exec(`CREATE TRIGGER refuse_http_followup_delete BEFORE DELETE ON followups BEGIN SELECT RAISE(ABORT, 'HTTP followup delete unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	body := h.json(http.MethodPost, "/api/threads/"+id+"/followups/"+f.ID+"/steer", nil, http.StatusBadRequest)
	message, _ := body["error"].(string)
	if !strings.Contains(message, "HTTP followup delete unavailable") || body["steered"] != nil || body["turn"] != nil {
		t.Fatalf("storage failure was hidden or misclassified: %+v", body)
	}
	rows, err := h.app.Store.ListFollowups(id)
	if err != nil || len(rows) != 1 || rows[0].ID != f.ID || rows[0].Text != f.Text {
		t.Fatalf("failed HTTP steering lost the row: %+v err=%v", rows, err)
	}
	turns, err := h.app.Store.ListTurns(id)
	if err != nil || len(turns) != 0 {
		t.Fatalf("failed HTTP steering started work: %+v err=%v", turns, err)
	}
}
