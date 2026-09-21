package server_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestSettingsSearchDefaultsOff(t *testing.T) {
	h := newHarness(t)
	got := h.json(http.MethodGet, "/api/settings", nil, http.StatusOK)
	settings, _ := got["settings"].(map[string]any)
	searchCfg, _ := settings["search"].(map[string]any)
	if searchCfg["embedding"] != false {
		t.Fatalf("embedding must ship off: %v", searchCfg)
	}
	if searchCfg["embedding_model"] != "" || searchCfg["embedding_provider"] != "" {
		t.Fatalf("no baked-in embedding pin: %v", searchCfg)
	}
}

func TestSearchFindsAConversationByBodyNotTitle(t *testing.T) {
	h := newHarness(t)
	created := h.json(http.MethodPost, "/api/threads", map[string]any{"title": "gamma"}, http.StatusCreated)
	thread, _ := created["thread"].(map[string]any)
	id, _ := thread["id"].(string)
	if id == "" {
		t.Fatalf("create: %v", created)
	}
	turn := &store.Turn{ThreadID: id, Status: store.TurnDone}
	if err := h.app.Store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := h.app.Store.AppendMessages(id, turn.ID, []store.Message{
		{Role: "user", Content: "unique-body needle sits in this request"},
	}); err != nil {
		t.Fatal(err)
	}

	out := h.json(http.MethodGet, "/api/search?q=unique-body", nil, http.StatusOK)
	hits, _ := out["hits"].([]any)
	if len(hits) == 0 {
		t.Fatalf("no hits: %v", out)
	}
	first, _ := hits[0].(map[string]any)
	if first["thread_id"] != id {
		t.Fatalf("want the body hit, got %v", out)
	}
	if out["embedding"] != false {
		t.Fatal("embedding ships off; the response must not claim semantic ranking")
	}
}

func TestSearchBlankQueryIsEmptyNotAWildcard(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodPost, "/api/threads", map[string]any{"title": "alpha"}, http.StatusCreated)
	out := h.json(http.MethodGet, "/api/search?q=", nil, http.StatusOK)
	hits, _ := out["hits"].([]any)
	if len(hits) != 0 {
		t.Fatalf("blank query leaked rows: %v", out)
	}
}

func TestSettingsPersistSearchEmbeddingPin(t *testing.T) {
	h := newHarness(t)
	out := h.json(http.MethodPut, "/api/settings", map[string]any{
		"search": map[string]any{
			"embedding": true, "embedding_provider": "default", "embedding_model": "named-embed",
		},
	}, http.StatusOK)
	settings, _ := out["settings"].(map[string]any)
	searchCfg, _ := settings["search"].(map[string]any)
	if searchCfg["embedding"] != true || searchCfg["embedding_model"] != "named-embed" {
		t.Fatalf("settings did not echo the pin: %v", out)
	}
	if !h.app.Config.Search.SemanticEnabled() {
		t.Fatal("live config must enable semantic search after PUT")
	}
	got := h.json(http.MethodGet, "/api/settings", nil, http.StatusOK)
	settings, _ = got["settings"].(map[string]any)
	searchCfg, _ = settings["search"].(map[string]any)
	if searchCfg["embedding"] != true || searchCfg["embedding_model"] != "named-embed" {
		t.Fatalf("GET lost the pin: %v", got)
	}
}

func TestSearchFindsShortCJKInTheBody(t *testing.T) {
	h := newHarness(t)
	created := h.json(http.MethodPost, "/api/threads", map[string]any{"title": "gamma"}, http.StatusCreated)
	thread, _ := created["thread"].(map[string]any)
	id, _ := thread["id"].(string)
	turn := &store.Turn{ThreadID: id, Status: store.TurnDone}
	if err := h.app.Store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := h.app.Store.AppendMessages(id, turn.ID, []store.Message{
		{Role: "user", Content: "会话里写了训练计划"},
	}); err != nil {
		t.Fatal(err)
	}
	out := h.json(http.MethodGet, "/api/search?q="+urlQuery("训练"), nil, http.StatusOK)
	hits, _ := out["hits"].([]any)
	if len(hits) == 0 {
		t.Fatalf("two-character CJK must hit: %v", out)
	}
}

func TestSearchReportsEmbeddingOnceAModelIsPinned(t *testing.T) {
	h := newHarness(t)
	created := h.json(http.MethodPost, "/api/threads", map[string]any{"title": "gamma"}, http.StatusCreated)
	thread, _ := created["thread"].(map[string]any)
	id, _ := thread["id"].(string)
	turn := &store.Turn{ThreadID: id, Status: store.TurnDone}
	if err := h.app.Store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := h.app.Store.AppendMessages(id, turn.ID, []store.Message{
		{Role: "user", Content: "unique-body needle sits in this request"},
	}); err != nil {
		t.Fatal(err)
	}
	h.json(http.MethodPut, "/api/settings", map[string]any{
		"search": map[string]any{
			"embedding": true, "embedding_provider": "default", "embedding_model": "named-embed",
		},
	}, http.StatusOK)
	h.app.Search.WaitIdle()
	out := h.json(http.MethodGet, "/api/search?q=unique-body&limit=5", nil, http.StatusOK)
	if out["embedding"] != true {
		t.Fatalf("pinned model must claim semantic ranking: %v", out)
	}
	hits, _ := out["hits"].([]any)
	if len(hits) == 0 {
		t.Fatalf("hybrid search lost the keyword hit: %v", out)
	}
}

func urlQuery(q string) string {
	return strings.ReplaceAll(url.QueryEscape(q), "+", "%20")
}
