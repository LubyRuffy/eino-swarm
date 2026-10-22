package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestIdleShutdownRefusesBothNewAndAlreadyReservedShells(t *testing.T) {
	h := newHarness(t)
	reserved := h.json(http.MethodPost, "/api/presence", map[string]any{"surface": "desktop"}, http.StatusOK)
	if !h.app.Server.TryIdleShutdown() {
		t.Fatal("an unconnected reservation must not pin the engine")
	}
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/presence", map[string]any{"surface": "desktop"}},
		{http.MethodGet, "/api/presence/" + reserved["id"].(string), nil},
	} {
		resp := h.do(tc.method, tc.path, tc.body)
		var failure struct {
			Code string `json:"code"`
		}
		err := json.NewDecoder(resp.Body).Decode(&failure)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusServiceUnavailable || failure.Code != "engine_stopping" {
			t.Fatalf("%s %s: status=%d code=%s err=%v", tc.method, tc.path, resp.StatusCode, failure.Code, err)
		}
	}
}

func TestIdleCommitRechecksAnActualRunningTurn(t *testing.T) {
	h := newHarness(t)
	th, err := h.app.Engine.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.Engine.StartTurn(th.ID, "continue the offline task"); err != nil {
		t.Fatal(err)
	}
	if len(h.app.Engine.Running()) != 1 || h.app.Server.TryIdleShutdown() {
		t.Fatal("idle commit lost a running turn")
	}
}

// Restart readiness still owns its checkpointed registries. An idle manager
// list cannot authorize shutdown while the handoff owns that executable work.
func TestIdleCommitRetainsRestartOwnership(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	status, err := h.app.Engine.PrepareRestart(ctx)
	if err != nil || !status.Ready {
		t.Fatalf("restart preparation failed: status=%+v err=%v", status, err)
	}
	active, managers := h.app.Engine.ActiveWork(), len(h.app.Engine.Running())
	if active == 0 || managers != 0 {
		t.Fatalf("fixture lost handoff ownership: active=%d managers=%d", active, managers)
	}
	if h.app.Server.TryIdleShutdown() {
		t.Fatalf("idle shutdown committed during handoff: stage=%s active=%d managers=%d", status.Stage, active, managers)
	}
	h.json(http.MethodPost, "/api/presence", map[string]any{"surface": "web"}, http.StatusOK)
}
