package server_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/app"
	"github.com/LubyRuffy/eino-swarm/internal/server"
)

func TestFOFAInstallOmitsTheModeAndRejectsCustomModelsUntilLocal(t *testing.T) {
	for _, key := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL"} {
		t.Setenv(key, "")
	}
	a, err := app.New(app.Options{DataDir: t.TempDir(), Mode: server.ModeWeb, Version: "test", NoAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	a.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	ts := httptest.NewServer(a.Server.Handler())
	t.Cleanup(func() {
		ts.Close()
		a.Shutdown(t.Context())
	})
	h := &harness{t: t, app: a, srv: ts}

	meta := h.json(http.MethodGet, "/api/meta", nil, http.StatusOK)
	if meta["product_mode"] != "fofa" {
		t.Fatalf("meta mode = %v", meta["product_mode"])
	}
	settings := h.json(http.MethodGet, "/api/settings", nil, http.StatusOK)["settings"].(map[string]any)
	product := settings["product"].(map[string]any)
	if product["mode"] != "fofa" {
		t.Fatalf("settings mode = %v", product["mode"])
	}
	raw, err := os.ReadFile(a.Config.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "product:") {
		t.Fatalf("default file wrote product:\n%s", raw)
	}
	before := settings["models"].(map[string]any)["providers"]

	id := h.json(http.MethodPost, "/api/threads", map[string]any{"title": "Needs an account"}, http.StatusCreated)["thread"].(map[string]any)["id"].(string)
	refused := h.json(http.MethodPost, "/api/threads/"+id+"/turns", map[string]any{"text": "start"}, http.StatusForbidden)
	if refused["code"] != "login_required" {
		t.Fatalf("turn refusal = %v", refused)
	}

	changed := h.json(http.MethodPut, "/api/settings", map[string]any{
		"models": map[string]any{
			"default": "default",
			"providers": []map[string]any{{
				"id": "default", "label": "Other", "base_url": "http://other.example/v1",
				"model": "m", "timeout_seconds": 30,
			}},
		},
	}, http.StatusForbidden)
	if changed["code"] != "custom_models_disabled" {
		t.Fatalf("model refusal = %v", changed)
	}
	kept := h.json(http.MethodGet, "/api/settings", nil, http.StatusOK)["settings"].(map[string]any)["models"].(map[string]any)["providers"]
	if strings.Contains(mustJSON(t, kept), "other.example") || mustJSON(t, kept) != mustJSON(t, before) {
		t.Fatalf("providers changed: before %s after %s", mustJSON(t, before), mustJSON(t, kept))
	}
	discovered := h.json(http.MethodPost, "/api/models/discover", map[string]any{
		"base_url": "http://other.example/v1",
	}, http.StatusForbidden)
	if discovered["code"] != "custom_models_disabled" {
		t.Fatalf("discover = %v", discovered)
	}

	unlocked := h.json(http.MethodPut, "/api/settings", map[string]any{
		"product": map[string]any{"mode": "local"},
	}, http.StatusOK)
	if unlocked["settings"].(map[string]any)["product"].(map[string]any)["mode"] != "local" {
		t.Fatalf("unlock = %v", unlocked["settings"])
	}
	raw, err = os.ReadFile(a.Config.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "mode: local") {
		t.Fatalf("local was not stored:\n%s", raw)
	}
	if meta = h.json(http.MethodGet, "/api/meta", nil, http.StatusOK); meta["product_mode"] != "local" {
		t.Fatalf("meta after unlock = %v", meta["product_mode"])
	}
	h.json(http.MethodPut, "/api/settings", map[string]any{
		"models": map[string]any{
			"default": "default",
			"providers": []map[string]any{{
				"id": "default", "label": "Other", "base_url": "http://127.0.0.1:1/v1",
				"model": "m", "api_key": "k", "timeout_seconds": 1,
			}},
		},
	}, http.StatusOK)
	again := h.json(http.MethodPost, "/api/threads/"+id+"/turns", map[string]any{"text": "start"}, http.StatusAccepted)
	if again["code"] == "login_required" {
		t.Fatalf("local still required a sign-in: %v", again)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
