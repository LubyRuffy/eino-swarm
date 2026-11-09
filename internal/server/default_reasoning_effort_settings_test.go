package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func settingsDefaultReasoningEffort(t *testing.T, h *harness) string {
	t.Helper()
	resp := h.do(http.MethodGet, "/api/settings", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/settings: status %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var out struct {
		Settings struct {
			Swarm struct {
				DefaultReasoningEffort *string `json:"default_reasoning_effort"`
			} `json:"swarm"`
		} `json:"settings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Settings.Swarm.DefaultReasoningEffort == nil {
		t.Fatal("GET /api/settings is missing swarm.default_reasoning_effort")
	}
	return *out.Settings.Swarm.DefaultReasoningEffort
}

func putDefaultReasoningEffortSettings(t *testing.T, h *harness, body map[string]any, wantStatus int) {
	t.Helper()
	resp := h.do(http.MethodPut, "/api/settings", body)
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("PUT /api/settings: status %d, want %d", resp.StatusCode, wantStatus)
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatal(err)
	}
}

func assertStoredDefaultReasoningEffort(t *testing.T, h *harness, want string) {
	t.Helper()
	stored, err := config.Load(h.app.Config.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(stored.SwarmSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	var swarm struct {
		DefaultReasoningEffort *string `json:"default_reasoning_effort"`
	}
	if err := json.Unmarshal(raw, &swarm); err != nil {
		t.Fatal(err)
	}
	if swarm.DefaultReasoningEffort == nil || *swarm.DefaultReasoningEffort != want {
		t.Fatalf("saved default_reasoning_effort is missing or differs from %q", want)
	}
}

func TestSettingsDefaultReasoningEffortDefaultsBlank(t *testing.T) {
	if got := settingsDefaultReasoningEffort(t, newHarness(t)); got != "" {
		t.Fatalf("fresh default=%q, want empty", got)
	}
}

func TestSettingsDefaultReasoningEffortNormalizesAndSavesExplicitValues(t *testing.T) {
	for _, tc := range []struct{ name, value, want string }{
		{"high normalization", " HIGH ", "high"},
		{"low", "low", "low"},
		{"medium normalization", " Medium ", "medium"},
		{"invalid uses default", "unsupported", ""},
		{"explicit empty clears", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			putDefaultReasoningEffortSettings(t, h, map[string]any{
				"swarm": map[string]any{"default_reasoning_effort": "high"},
			}, http.StatusOK)
			putDefaultReasoningEffortSettings(t, h, map[string]any{
				"swarm": map[string]any{"default_reasoning_effort": tc.value},
			}, http.StatusOK)
			if got := settingsDefaultReasoningEffort(t, h); got != tc.want {
				t.Fatalf("live effort=%q, want %q", got, tc.want)
			}
			assertStoredDefaultReasoningEffort(t, h, tc.want)
		})
	}
}

// Older clients do not know this field. Their partial save, including null,
// must not erase a default configured by a different client.
func TestSettingsDefaultReasoningEffortSurvivesPartialAndNullSaves(t *testing.T) {
	h := newHarness(t)
	putDefaultReasoningEffortSettings(t, h, map[string]any{
		"swarm": map[string]any{"default_reasoning_effort": "high"},
	}, http.StatusOK)
	patches := []map[string]any{
		{},
		{"swarm": nil},
		{"swarm": map[string]any{}},
		{"swarm": map[string]any{"max_concurrent": 4}},
		{"swarm": map[string]any{"default_reasoning_effort": nil}},
		{"swarm": map[string]any{"unknown_future_option": true, "max_concurrent": 5}},
	}
	for i, patch := range patches {
		putDefaultReasoningEffortSettings(t, h, patch, http.StatusOK)
		if got := settingsDefaultReasoningEffort(t, h); got != "high" {
			t.Fatalf("partial save %d changed effort=%q, want high", i, got)
		}
		assertStoredDefaultReasoningEffort(t, h, "high")
	}
}

func TestSettingsDefaultReasoningEffortRejectsNonStringAtomically(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"boolean", true},
		{"number", 1},
		{"array", []any{"high"}},
		{"object", map[string]any{"effort": "high"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			putDefaultReasoningEffortSettings(t, h, map[string]any{
				"swarm": map[string]any{"default_reasoning_effort": "high"},
			}, http.StatusOK)
			before, err := os.ReadFile(h.app.Config.Path())
			if err != nil {
				t.Fatal(err)
			}
			beforeSwarm := h.app.Config.SwarmSnapshot()
			putDefaultReasoningEffortSettings(t, h, map[string]any{
				"swarm": map[string]any{"default_reasoning_effort": tc.value, "max_concurrent": 7},
			}, http.StatusBadRequest)
			after, err := os.ReadFile(h.app.Config.Path())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) || h.app.Config.SwarmSnapshot() != beforeSwarm {
				t.Fatal("an invalid effort type changed the live or saved configuration")
			}
			if got := settingsDefaultReasoningEffort(t, h); got != "high" {
				t.Fatalf("invalid effort type changed stored choice to %q", got)
			}
		})
	}
}
