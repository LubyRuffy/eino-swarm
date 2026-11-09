package server_test

import (
	"net/http"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A model switch alone must not silently rewrite old workers; explicit repair
// must be reachable over the same HTTP boundary the desktop uses.
func TestRemovedWorkerProviderHasExplicitHTTPRecovery(t *testing.T) {
	h := newHarness(t)
	th, err := h.app.Engine.CreateThread("Worker recovery", "", "")
	if err != nil {
		t.Fatal(err)
	}
	source := &store.Turn{ThreadID: th.ID, ProviderID: "removed", Model: "original"}
	if err := h.app.Store.CreateTurn(source); err != nil {
		t.Fatal(err)
	}
	if err := h.app.Store.FinishTurn(source.ID, store.TurnDone, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := h.app.Store.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: source.ID, Kind: swarm.NotifySpawned.String(), AgentID: "worker-1", Role: "worker", Text: "Inspect progress", ConfigTag: source.ID, WorkerGeneration: 1}); err != nil {
		t.Fatal(err)
	}
	h.json("POST", "/api/threads/"+th.ID+"/worker-model-recovery", map[string]any{"provider_id": th.ProviderID, "model": ""}, http.StatusOK)
}

func TestWorkerRecoveryHTTPRejectsMissingBodyOwnerAndInvalidTarget(t *testing.T) {
	h := newHarness(t)
	th, err := h.app.Engine.CreateThread("Recovery refusal", "", "")
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/threads/" + th.ID + "/worker-model-recovery"
	h.json("POST", path, nil, http.StatusBadRequest)
	h.json("POST", path, map[string]any{}, http.StatusBadRequest)
	h.json("POST", path, map[string]any{"provider_id": th.ProviderID}, http.StatusBadRequest)
	h.json("POST", "/api/threads/missing/worker-model-recovery", map[string]any{"provider_id": th.ProviderID}, http.StatusNotFound)
	response := h.do("POST", path, map[string]any{"provider_id": "missing"})
	defer response.Body.Close()
	if response.StatusCode < 400 {
		t.Fatal("invalid target was accepted")
	}
}
