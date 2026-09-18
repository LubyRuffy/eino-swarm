package server_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestPreemptSteerThroughTheAPI(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	idle := h.json(http.MethodPost, "/api/threads/"+id+"/preempt", nil, http.StatusConflict)
	if idle["code"] != "idle" {
		t.Fatalf("idle preempt: %v", idle)
	}
	h.json(http.MethodDelete, "/api/threads/"+id+"/steers/0", nil, http.StatusBadRequest)
	h.json(http.MethodDelete, "/api/threads/"+id+"/steers/nope", nil, http.StatusBadRequest)
	idleDel := h.json(http.MethodDelete, "/api/threads/"+id+"/steers/1", nil, http.StatusConflict)
	if idleDel["code"] != "idle" {
		t.Fatalf("idle retract: %v", idleDel)
	}
}

func TestMutatingAPIRefusesAForeignOrigin(t *testing.T) {
	h := newHarness(t)
	req, err := http.NewRequest(http.MethodPost, h.srv.URL+"/api/threads", strings.NewReader(`{"title":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d want 403", resp.StatusCode)
	}
}
