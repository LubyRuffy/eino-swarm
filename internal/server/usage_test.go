package server_test

import (
	"net/http"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestThreadReportsTokenUsage(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()

	empty := h.json(http.MethodGet, "/api/threads/"+id, nil, http.StatusOK)
	usage, ok := empty["usage"].(map[string]any)
	if !ok {
		t.Fatalf("GET thread must include usage: %v", empty)
	}
	if usage["context_tokens"] != float64(0) {
		t.Fatalf("a new conversation is not full: %v", usage)
	}

	th, err := h.app.Store.GetThread(id)
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, Status: store.TurnDone}
	if err := h.app.Store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := h.app.Store.AppendLLMCall(&store.LLMCall{
		ThreadID: th.ID, TurnID: turn.ID, AgentID: "manager",
		PromptTokens: 71300, CompletionTokens: 1200, TotalTokens: 72500,
		CachedTokens: 40000,
	}); err != nil {
		t.Fatal(err)
	}

	got := h.json(http.MethodGet, "/api/threads/"+id, nil, http.StatusOK)
	usage = got["usage"].(map[string]any)
	if usage["context_tokens"] != float64(71300) {
		t.Fatalf("context_tokens=%v", usage["context_tokens"])
	}
	if usage["context_window"] != float64(provider.MockContextWindow) {
		t.Fatalf("mock window missing: %v", usage["context_window"])
	}
	turnTot := usage["turn"].(map[string]any)
	if turnTot["total_tokens"] != float64(72500) || turnTot["cached_tokens"] != float64(40000) {
		t.Fatalf("turn totals: %v", turnTot)
	}
}

func TestContextWindowSurvivesASaveThatOmitsIt(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodPut, "/api/settings", map[string]any{
		"models": map[string]any{
			"default": "default",
			"providers": []map[string]any{{
				"id": "default", "label": "", "base_url": "http://endpoint.invalid/v1",
				"model": "m", "timeout_seconds": 60, "context_window": 256000,
				"model_context": map[string]int{"m": 128000},
			}},
		},
	}, http.StatusOK)

	got := h.json(http.MethodGet, "/api/settings", nil, http.StatusOK)
	prov := got["settings"].(map[string]any)["models"].(map[string]any)["providers"].([]any)[0].(map[string]any)
	if prov["context_window"] != float64(256000) {
		t.Fatalf("context_window not returned: %v", prov)
	}
	windows := prov["model_context"].(map[string]any)
	if windows["m"] != float64(128000) {
		t.Fatalf("model_context not returned: %v", windows)
	}

	h.json(http.MethodPut, "/api/settings", map[string]any{
		"models": map[string]any{
			"default": "default",
			"providers": []map[string]any{{
				"id": "default", "label": "Renamed", "base_url": "http://endpoint.invalid/v1",
				"model": "m", "timeout_seconds": 60,
			}},
		},
	}, http.StatusOK)
	stored, _ := h.app.Config.Provider("default")
	if stored.ContextWindow != 256000 || stored.ModelContext["m"] != 128000 {
		t.Fatalf("omitting windows wiped them: %+v", stored)
	}
}

func TestDiscoverIncludesContextWindows(t *testing.T) {
	h := newHarness(t)
	discovered := h.json(http.MethodPost, "/api/models/discover",
		map[string]any{"provider_id": "default"}, http.StatusOK)
	names, ok := discovered["models"].([]any)
	if !ok || len(names) == 0 {
		t.Fatalf("offline discover must still return a catalog: %v", discovered)
	}
	windows, ok := discovered["context_windows"].(map[string]any)
	if !ok {
		t.Fatalf("discover must return context_windows, got %v", discovered)
	}
	for _, raw := range names {
		name, _ := raw.(string)
		if windows[name] != float64(provider.MockContextWindow) {
			t.Fatalf("offline discover window for %q: %v", name, windows)
		}
	}

	listed := h.json(http.MethodGet, "/api/models", nil, http.StatusOK)
	first := listed["models"].([]any)[0].(map[string]any)
	if first["context_window"] != float64(provider.MockContextWindow) {
		t.Fatalf("GET /api/models window=%v", first["context_window"])
	}
}
