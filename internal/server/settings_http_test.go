package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/server"
)

// ---------- meta / settings ----------

func TestMetaTellsTheUIWhatItCanDo(t *testing.T) {
	h := newHarness(t)
	got := h.json(http.MethodGet, "/api/meta", nil, http.StatusOK)

	if got["version"] != "test" || got["mode"] != server.ModeWeb {
		t.Fatalf("meta=%v", got)
	}
	if got["mock"] != true {
		t.Fatal("a mock build must say so, or a demo answer looks real")
	}
	// the scripted provider needs no setup, so the UI must not show a setup banner
	if got["configured"] != true {
		t.Fatal("a mock build is always configured")
	}
	caps, ok := got["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("no capabilities: %v", got)
	}
	// in a browser there is no file manager to open, so the UI hides the button
	if caps["reveal"] != false {
		t.Fatalf("web mode must not advertise reveal: %v", caps)
	}
	if caps["open_url"] != false {
		t.Fatalf("web mode must not advertise open_url: %v", caps)
	}
	if got["data_dir"] == "" {
		t.Fatal("meta should report the data directory for troubleshooting")
	}
	if got["locale"] != "system" {
		t.Fatalf("a fresh install follows the system language, got %v", got["locale"])
	}
	ui, _ := got["ui"].(map[string]any)
	if ui["font"] != "system" || ui["font_size"] != "medium" || ui["content_width"] != "comfortable" {
		t.Fatalf("a fresh install keeps the current column and type, got %v", got["ui"])
	}
	// the composer builds its thinking-level menu from this, so it must arrive
	levels, ok := got["reasoning_levels"].([]any)
	if !ok || len(levels) != 3 || levels[0] != "low" || levels[2] != "high" {
		t.Fatalf("meta must offer low/medium/high thinking levels, got %v", got["reasoning_levels"])
	}
	swarm, ok := got["swarm"].(map[string]any)
	if !ok {
		t.Fatalf("no swarm limits: %v", got)
	}
	if swarm["manager_max_iterations"] != float64(200) || swarm["max_turns"] != float64(200) {
		t.Fatalf("the documented defaults are 200, got %v", swarm)
	}
}

// The API key must never come back out of the settings endpoint; the dialog
// only needs to know whether one is stored.
func TestSettingsNeverReturnsTheAPIKey(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodPut, "/api/settings", map[string]any{
		"models": map[string]any{
			"default": "default",
			"providers": []map[string]any{{
				"id": "default", "label": "Main", "base_url": "http://endpoint.invalid/v1",
				"model": "m", "api_key": "super-secret", "timeout_seconds": 60,
			}},
		},
	}, http.StatusOK)

	resp := h.do(http.MethodGet, "/api/settings", nil)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(raw), "super-secret") {
		t.Fatalf("the api key leaked through the settings endpoint: %s", raw)
	}
	var out struct {
		Settings struct {
			Models struct {
				Providers []struct {
					ID        string `json:"id"`
					HasAPIKey bool   `json:"has_api_key"`
					Ready     bool   `json:"ready"`
				}
			}
		}
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	p := out.Settings.Models.Providers[0]
	if !p.HasAPIKey || !p.Ready {
		t.Fatalf("the dialog cannot tell a key is stored: %+v", p)
	}

	// and the stored key survives a save that does not mention it
	h.json(http.MethodPut, "/api/settings", map[string]any{
		"models": map[string]any{
			"default": "default",
			"providers": []map[string]any{{
				"id": "default", "label": "Renamed", "base_url": "http://endpoint.invalid/v1",
				"model": "m2", "timeout_seconds": 60,
			}},
		},
	}, http.StatusOK)
	prov, _ := h.app.Config.Provider("default")
	if prov.APIKey != "super-secret" {
		t.Fatalf("saving other fields wiped the api key: %q", prov.APIKey)
	}
	if prov.Model != "m2" {
		t.Fatalf("the model was not updated: %q", prov.Model)
	}

	h.json(http.MethodPut, "/api/settings", map[string]any{
		"models": map[string]any{
			"default": "default",
			"providers": []map[string]any{{
				"id": "default", "label": "Renamed", "base_url": "http://endpoint.invalid/v1",
				"model": "m2", "catalog": []string{"m2", "m3"}, "timeout_seconds": 60,
			}},
		},
	}, http.StatusOK)
	prov, _ = h.app.Config.Provider("default")
	if len(prov.Catalog) != 2 || prov.Catalog[1] != "m3" {
		t.Fatalf("catalog not saved: %v", prov.Catalog)
	}
	h.json(http.MethodPut, "/api/settings", map[string]any{
		"models": map[string]any{
			"default": "default",
			"providers": []map[string]any{{
				"id": "default", "label": "Renamed", "base_url": "http://endpoint.invalid/v1",
				"model": "m2", "timeout_seconds": 60,
			}},
		},
	}, http.StatusOK)
	prov, _ = h.app.Config.Provider("default")
	if len(prov.Catalog) != 2 {
		t.Fatalf("omitting catalog wiped it: %v", prov.Catalog)
	}

	// an explicit empty string clears it
	h.json(http.MethodPut, "/api/settings", map[string]any{
		"models": map[string]any{
			"default": "default",
			"providers": []map[string]any{{
				"id": "default", "base_url": "http://endpoint.invalid/v1",
				"model": "m2", "api_key": "",
			}},
		},
	}, http.StatusOK)
	prov, _ = h.app.Config.Provider("default")
	if prov.APIKey != "" {
		t.Fatalf("an explicit empty key did not clear it: %q", prov.APIKey)
	}
}

func TestSettingsPersistAndValidate(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodPut, "/api/settings", map[string]any{
		"swarm": map[string]any{"max_concurrent": 3, "agent_timeout_seconds": 42,
			"max_turns": 9, "manager_max_iterations": 11, "progress_interval_seconds": 7,
			"delta_coalesce_ms": 16, "auto_title": true,
			"title_provider": "default", "title_model": "tiny",
			"auto_compact_tokens": 12000},
		"tools": map[string]any{"disabled": []string{"exec"}, "web_search_max_results": 5},
		"log":   map[string]any{"level": "debug"},
	}, http.StatusOK)

	if h.app.Config.Swarm.MaxConcurrent != 3 || h.app.Config.Swarm.AgentTimeoutSeconds != 42 {
		t.Fatalf("swarm settings not applied: %+v", h.app.Config.Swarm)
	}
	if h.app.Config.Swarm.ProgressIntervalSeconds != 7 {
		t.Fatalf("progress interval not applied: %+v", h.app.Config.Swarm)
	}
	if h.app.Config.Swarm.DeltaCoalesceMS != 16 {
		t.Fatalf("delta coalesce not applied: %+v", h.app.Config.Swarm)
	}
	if !h.app.Config.Swarm.AutoTitle {
		t.Fatal("auto_title not applied")
	}
	if h.app.Config.Swarm.TitleProvider != "default" || h.app.Config.Swarm.TitleModel != "tiny" {
		t.Fatalf("title pin not applied: %+v", h.app.Config.Swarm)
	}
	if h.app.Config.Swarm.AutoCompactTokens != 12000 {
		t.Fatalf("auto-compact token budget not applied: %+v", h.app.Config.Swarm)
	}
	if !h.app.Config.Tools.IsDisabled("exec") {
		t.Fatal("tool toggle not applied")
	}
	// and they survive a restart, because they were written to the file
	raw, err := os.ReadFile(h.app.Config.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "exec") {
		t.Fatalf("settings were not persisted to disk:\n%s", raw)
	}

	h.json(http.MethodPut, "/api/settings", map[string]any{
		"models": map[string]any{"default": "x", "providers": []map[string]any{}},
	}, http.StatusBadRequest)
	resp := h.do(http.MethodPut, "/api/settings", "not an object")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("garbage body accepted: %d", resp.StatusCode)
	}
}

// A language switch is a PUT of only ui, so it must not wipe swarm settings,
// and GET /api/meta must report the pin so the next load paints Chinese
// before Settings is opened.
func TestLocaleRoundTripsThroughSettingsAndMeta(t *testing.T) {
	h := newHarness(t)
	before := h.app.Config.Swarm.MaxConcurrent
	out := h.json(http.MethodPut, "/api/settings", map[string]any{
		"ui": map[string]any{"locale": "zh"},
	}, http.StatusOK)
	settings, _ := out["settings"].(map[string]any)
	ui, _ := settings["ui"].(map[string]any)
	if ui["locale"] != "zh" {
		t.Fatalf("settings did not echo the language: %v", out)
	}
	if h.app.Config.UI.Locale != "zh" {
		t.Fatalf("locale not stored: %+v", h.app.Config.UI)
	}
	if h.app.Config.Swarm.MaxConcurrent != before {
		t.Fatalf("a language PUT must not rewrite swarm: %+v", h.app.Config.Swarm)
	}
	meta := h.json(http.MethodGet, "/api/meta", nil, http.StatusOK)
	if meta["locale"] != "zh" {
		t.Fatalf("meta must report the pin so boot can apply it: %v", meta["locale"])
	}

	h.json(http.MethodPut, "/api/settings", map[string]any{
		"ui": map[string]any{"locale": "nope"},
	}, http.StatusOK)
	if h.app.Config.UI.Locale != "system" {
		t.Fatalf("junk must become system, got %q", h.app.Config.UI.Locale)
	}
}

// Font, size and column width ride the same ui object as locale. A
// language-only PUT must not reset them; GET /api/meta must report them so
// the next load paints before Settings is opened.
func TestUIChromeRoundTripsThroughSettingsAndMeta(t *testing.T) {
	h := newHarness(t)
	before := h.app.Config.Swarm.MaxConcurrent
	out := h.json(http.MethodPut, "/api/settings", map[string]any{
		"ui": map[string]any{
			"font":          "serif",
			"font_size":     "large",
			"content_width": "full",
		},
	}, http.StatusOK)
	settings, _ := out["settings"].(map[string]any)
	ui, _ := settings["ui"].(map[string]any)
	if ui["font"] != "serif" || ui["font_size"] != "large" || ui["content_width"] != "full" {
		t.Fatalf("settings did not echo chrome: %v", out)
	}
	if ui["locale"] != "system" {
		t.Fatalf("a font PUT must keep the language: %v", ui)
	}
	if h.app.Config.UI.Font != "serif" || h.app.Config.UI.FontSize != "large" ||
		h.app.Config.UI.ContentWidth != "full" {
		t.Fatalf("chrome not stored: %+v", h.app.Config.UI)
	}
	if h.app.Config.Swarm.MaxConcurrent != before {
		t.Fatalf("a chrome PUT must not rewrite swarm: %+v", h.app.Config.Swarm)
	}

	meta := h.json(http.MethodGet, "/api/meta", nil, http.StatusOK)
	metaUI, _ := meta["ui"].(map[string]any)
	if metaUI["font"] != "serif" || metaUI["font_size"] != "large" ||
		metaUI["content_width"] != "full" {
		t.Fatalf("meta must report chrome so boot can apply it: %v", meta["ui"])
	}

	h.json(http.MethodPut, "/api/settings", map[string]any{
		"ui": map[string]any{"locale": "zh"},
	}, http.StatusOK)
	if h.app.Config.UI.Locale != "zh" {
		t.Fatalf("locale not stored: %+v", h.app.Config.UI)
	}
	if h.app.Config.UI.Font != "serif" || h.app.Config.UI.ContentWidth != "full" {
		t.Fatalf("a language PUT must not reset chrome: %+v", h.app.Config.UI)
	}

	h.json(http.MethodPut, "/api/settings", map[string]any{
		"ui": map[string]any{"font": "nope", "content_width": "wide"},
	}, http.StatusOK)
	if h.app.Config.UI.Font != "system" {
		t.Fatalf("junk font must become system, got %q", h.app.Config.UI.Font)
	}
	if h.app.Config.UI.ContentWidth != "comfortable" {
		t.Fatalf("junk width must become comfortable, got %q", h.app.Config.UI.ContentWidth)
	}
	if h.app.Config.UI.Locale != "zh" {
		t.Fatalf("a font PUT must keep the language: %+v", h.app.Config.UI)
	}
}

func TestModelsAndToolsEndpoints(t *testing.T) {
	h := newHarness(t)
	models := h.json(http.MethodGet, "/api/models", nil, http.StatusOK)
	if len(models["models"].([]any)) == 0 {
		t.Fatal("no models listed")
	}
	if models["default"] == "" {
		t.Fatal("no default model reported")
	}

	tools := h.json(http.MethodGet, "/api/tools", nil, http.StatusOK)
	catalog := tools["catalog"].([]any)
	enabled := tools["enabled"].([]any)
	if len(catalog) < 10 {
		t.Fatalf("the tool catalog looks truncated: %d", len(catalog))
	}
	if len(enabled) == 0 || len(enabled) > len(catalog) {
		t.Fatalf("enabled=%d catalog=%d", len(enabled), len(catalog))
	}
	first := catalog[0].(map[string]any)
	for _, field := range []string{"name", "title", "summary", "group"} {
		if first[field] == nil || first[field] == "" {
			t.Fatalf("catalog entries must be renderable: %v", first)
		}
	}

	listed := models["models"].([]any)[0].(map[string]any)
	for _, field := range []string{"id", "provider_id", "model"} {
		if listed[field] == nil || listed[field] == "" {
			t.Fatalf("selectable models must be addressable: %v", listed)
		}
	}
	discovered := h.json(http.MethodPost, "/api/models/discover",
		map[string]any{"provider_id": "default"}, http.StatusOK)
	names := discovered["models"].([]any)
	if len(names) == 0 {
		t.Fatal("offline discover must still return a catalog")
	}
	h.json(http.MethodPost, "/api/models/discover",
		map[string]any{"provider_id": "nope"}, http.StatusBadRequest)
	// a row that has not been saved yet still lists against the URL in the form
	unsaved := h.json(http.MethodPost, "/api/models/discover",
		map[string]any{"provider_id": "nope", "base_url": "http://endpoint.invalid/v1"},
		http.StatusOK)
	if len(unsaved["models"].([]any)) == 0 {
		t.Fatal("discover against an unsaved URL returned nothing")
	}
}

// Personality is install-wide: a PUT must persist it, an omitted section
// must keep it, and an empty string must clear it.
func TestPersonalitySettingsRoundTrip(t *testing.T) {
	h := newHarness(t)
	got := h.json(http.MethodGet, "/api/settings", nil, http.StatusOK)["settings"].(map[string]any)
	persona, ok := got["personality"].(map[string]any)
	if !ok {
		t.Fatalf("settings do not include personality: %v", got)
	}
	if persona["instructions"] != "" {
		t.Fatalf("a fresh install has no personality, got %v", persona)
	}

	h.json(http.MethodPut, "/api/settings", map[string]any{
		"personality": map[string]any{"instructions": "prefer compact replies"},
	}, http.StatusOK)
	reread := h.json(http.MethodGet, "/api/settings", nil, http.StatusOK)["settings"].(map[string]any)["personality"].(map[string]any)
	if reread["instructions"] != "prefer compact replies" {
		t.Fatalf("personality not persisted: %v", reread)
	}

	before := h.app.Config.Swarm.MaxConcurrent
	h.json(http.MethodPut, "/api/settings", map[string]any{
		"swarm": map[string]any{"max_concurrent": before},
	}, http.StatusOK)
	kept := h.json(http.MethodGet, "/api/settings", nil, http.StatusOK)["settings"].(map[string]any)["personality"].(map[string]any)
	if kept["instructions"] != "prefer compact replies" {
		t.Fatalf("omitting personality wiped it: %v", kept)
	}

	h.json(http.MethodPut, "/api/settings", map[string]any{
		"personality": map[string]any{"instructions": ""},
	}, http.StatusOK)
	cleared := h.json(http.MethodGet, "/api/settings", nil, http.StatusOK)["settings"].(map[string]any)["personality"].(map[string]any)
	if cleared["instructions"] != "" {
		t.Fatalf("empty instructions did not clear personality: %v", cleared)
	}
}
