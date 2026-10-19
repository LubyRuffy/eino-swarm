package server_test

import (
	"net/http"
	"testing"
)

func TestRestartHandoffIsIdempotentAndKeepsTheAPIReadable(t *testing.T) {
	h := newHarness(t)
	thread := h.newThread()
	before := h.json(http.MethodGet, "/api/engine/restart", nil, http.StatusOK)
	if before["ready"] == true {
		t.Fatal("an engine that has not prepared a handoff cannot be ready to exit")
	}
	for range 2 {
		got := h.json(http.MethodPost, "/api/engine/restart", nil, http.StatusOK)
		if got["ready"] != true || got["stage"] != "ready" {
			t.Fatalf("handoff was not durably acknowledged: %v", got)
		}
	}
	h.json(http.MethodGet, "/api/threads/"+thread, nil, http.StatusOK)
	got := h.json(http.MethodPost, "/api/threads/"+thread+"/turns", map[string]any{"text": "continue"}, http.StatusConflict)
	if got["code"] != "restarting" {
		t.Fatalf("new work must receive a recoverable restart response: %v", got)
	}
	meta := h.json(http.MethodGet, "/api/meta", nil, http.StatusOK)
	if meta["capabilities"].(map[string]any)["restart_handoff"] != true {
		t.Fatal("the launcher cannot discover automatic handoff support")
	}
}

func TestRestartHandoffRejectsForeignOrigins(t *testing.T) {
	h := newHarness(t)
	req, err := http.NewRequest(http.MethodPost, h.srv.URL+"/api/engine/restart", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://foreign.invalid")
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign restart status = %d", resp.StatusCode)
	}
	if h.app.Engine.GetRestartStatus().Ready {
		t.Fatal("foreign page prepared a restart")
	}
}

func TestRestartHandoffDoesNotAcknowledgeAStorageFailure(t *testing.T) {
	h := newHarness(t)
	thread := h.newThread()
	h.app.Engine.Config().Swarm.ManagerMaxIterations = 1
	if _, err := h.app.Engine.StartTurn(thread, "continue the assigned work"); err != nil {
		t.Fatal(err)
	}
	if err := h.app.Store.Close(); err != nil {
		t.Fatal(err)
	}
	resp := h.do(http.MethodPost, "/api/engine/restart", nil)
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("failed checkpoint was acknowledged")
	}
}
