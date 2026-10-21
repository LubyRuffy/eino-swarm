package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func cloudProviderRequest(fields map[string]any) map[string]any {
	row := map[string]any{"id": "default", "base_url": "http://provider.invalid/v1", "model": "fixture"}
	for k, v := range fields {
		row[k] = v
	}
	return map[string]any{"models": map[string]any{"default": "default", "providers": []any{row}}}
}

func settingsProvider(out map[string]any) map[string]any {
	return out["settings"].(map[string]any)["models"].(map[string]any)["providers"].([]any)[0].(map[string]any)
}

func TestCloudProviderSettingsKeepOptionsAndWriteOnlyAPIKey(t *testing.T) {
	h := newHarness(t)
	original := settingsProvider(h.json(http.MethodGet, "/api/settings", nil, http.StatusOK))
	if original["api_mode"] != "" {
		t.Fatalf("legacy provider protocol=%v", original["api_mode"])
	}
	if tools, ok := original["builtin_tools"].([]any); !ok || len(tools) != 0 {
		t.Fatalf("empty cloud tools must be an array: %+v", original)
	}
	tools := []any{map[string]any{"type": "web_search", "filters": map[string]any{"allowed_domains": []any{"fixture.invalid"}}}}
	out := h.json(http.MethodPut, "/api/settings", cloudProviderRequest(map[string]any{
		"api_mode": "responses", "builtin_tools": tools, "api_key": "secret-cloud-provider-fixture",
	}), http.StatusOK)
	row := settingsProvider(out)
	if row["api_mode"] != "responses" || row["has_api_key"] != true {
		t.Fatalf("provider fields not returned: %+v", row)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-cloud-provider-fixture") || row["api_key"] != nil {
		t.Fatal("Settings exposed API key")
	}
	rawTools, _ := json.Marshal(row["builtin_tools"])
	want, _ := json.Marshal(tools)
	if !bytes.Equal(rawTools, want) {
		t.Fatalf("cloud options changed: %s want %s", rawTools, want)
	}

	// Older clients saving a provider row must preserve fields they do not know.
	h.json(http.MethodPut, "/api/settings", cloudProviderRequest(nil), http.StatusOK)
	saved, err := config.Load(h.app.Config.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	p, _ := saved.Provider("default")
	rawTools, _ = json.Marshal(p.BuiltinTools)
	if p.APIMode != "responses" || !bytes.Equal(rawTools, want) || p.APIKey != "secret-cloud-provider-fixture" {
		t.Fatal("an older settings payload erased cloud configuration or credentials")
	}
	reread := settingsProvider(h.json(http.MethodGet, "/api/settings", nil, http.StatusOK))
	if reread["api_mode"] != "responses" {
		t.Fatalf("GET lost API mode: %+v", reread)
	}

	// JSON null has the same meaning as omission for optional settings fields;
	// only an explicit empty array clears the tool list.
	nullRow := settingsProvider(h.json(http.MethodPut, "/api/settings", cloudProviderRequest(map[string]any{
		"api_mode": nil, "builtin_tools": nil, "api_key": nil,
	}), http.StatusOK))
	nullTools, _ := json.Marshal(nullRow["builtin_tools"])
	if nullRow["api_mode"] != "responses" || !bytes.Equal(nullTools, want) || nullRow["has_api_key"] != true {
		t.Fatalf("null optional fields erased stored provider settings: %+v", nullRow)
	}

	cleared := settingsProvider(h.json(http.MethodPut, "/api/settings", cloudProviderRequest(map[string]any{
		"api_mode": "chat_completions", "builtin_tools": []any{},
	}), http.StatusOK))
	if cleared["api_mode"] != "chat_completions" || len(cleared["builtin_tools"].([]any)) != 0 {
		t.Fatalf("explicit [] must disable cloud tools: %+v", cleared)
	}
}

func TestCloudProviderSettingsRejectInvalidRequestsWithoutPartialChanges(t *testing.T) {
	for _, fields := range []map[string]any{
		{"api_mode": "unsupported"},
		{"builtin_tools": []any{map[string]any{"type": "web_search"}}},
		{"api_mode": "responses", "builtin_tools": []any{map[string]any{"type": "function"}}},
		{"api_mode": "responses", "builtin_tools": []any{map[string]any{"type": "unknown_tool"}}},
		{"api_mode": "responses", "builtin_tools": []any{map[string]any{"type": "web_search"}, map[string]any{"type": "web_search"}}},
		{"api_mode": "responses", "builtin_tools": []any{map[string]any{"type": 3}}},
		{"api_mode": "responses", "builtin_tools": []any{nil}},
		{"api_mode": "responses", "builtin_tools": []any{"web_search"}},
	} {
		h := newHarness(t)
		before, err := os.ReadFile(h.app.Config.Path())
		if err != nil {
			t.Fatal(err)
		}
		req := cloudProviderRequest(fields)
		req["ui"] = map[string]any{"locale": "zh"}
		h.json(http.MethodPut, "/api/settings", req, http.StatusBadRequest)
		after, err := os.ReadFile(h.app.Config.Path())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) || h.app.Config.UI.Locale != "system" {
			t.Fatal("invalid provider request partially updated live or saved settings")
		}
	}
}
