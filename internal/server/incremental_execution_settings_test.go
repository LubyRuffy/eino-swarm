package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func settingsIncrementalExecution(t *testing.T, h *harness) bool {
	t.Helper()
	resp := h.do(http.MethodGet, "/api/settings", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/settings: status %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var out struct {
		Settings struct {
			Swarm struct {
				IncrementalExecution *bool `json:"incremental_execution"`
			} `json:"swarm"`
		} `json:"settings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Settings.Swarm.IncrementalExecution == nil {
		t.Fatal("GET /api/settings is missing swarm.incremental_execution")
	}
	return *out.Settings.Swarm.IncrementalExecution
}

func putIncrementalExecutionSettings(t *testing.T, h *harness, body map[string]any, wantStatus int) {
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

func assertStoredIncrementalExecution(t *testing.T, h *harness, want bool) {
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
		IncrementalExecution *bool `json:"incremental_execution"`
	}
	if err := json.Unmarshal(raw, &swarm); err != nil {
		t.Fatal(err)
	}
	if swarm.IncrementalExecution == nil || *swarm.IncrementalExecution != want {
		t.Fatalf("saved incremental_execution is missing or differs from %t", want)
	}
}

func TestSettingsIncrementalExecutionDefaultsOff(t *testing.T) {
	if settingsIncrementalExecution(t, newHarness(t)) {
		t.Fatal("a new installation must keep the experiment disabled")
	}
}

func TestSettingsIncrementalExecutionExplicitChoiceIsSaved(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			h := newHarness(t)
			putIncrementalExecutionSettings(t, h, map[string]any{
				"swarm": map[string]any{"incremental_execution": enabled},
			}, http.StatusOK)
			if got := settingsIncrementalExecution(t, h); got != enabled {
				t.Fatalf("live choice=%t, want %t", got, enabled)
			}
			assertStoredIncrementalExecution(t, h, enabled)
		})
	}
}

// Older settings clients save partial swarm objects. Neither an omitted field
// nor JSON null should silently change an experiment chosen elsewhere.
func TestSettingsIncrementalExecutionSurvivesPartialSaves(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			h := newHarness(t)
			putIncrementalExecutionSettings(t, h, map[string]any{
				"swarm": map[string]any{"incremental_execution": enabled},
			}, http.StatusOK)
			patches := []map[string]any{
				{},
				{"swarm": nil},
				{"swarm": map[string]any{}},
				{"swarm": map[string]any{"max_concurrent": 4}},
				{"swarm": map[string]any{"incremental_execution": nil}},
				{"swarm": map[string]any{"unknown_future_option": true, "max_concurrent": 5}},
			}
			for i, patch := range patches {
				putIncrementalExecutionSettings(t, h, patch, http.StatusOK)
				if got := settingsIncrementalExecution(t, h); got != enabled {
					t.Fatalf("partial save %d changed choice to %t, want %t", i, got, enabled)
				}
				assertStoredIncrementalExecution(t, h, enabled)
			}
		})
	}
}

func TestSettingsIncrementalExecutionRejectsInvalidBooleanAtomically(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"string", "true"},
		{"number", 1},
		{"array", []any{true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			putIncrementalExecutionSettings(t, h, map[string]any{
				"swarm": map[string]any{"incremental_execution": true},
			}, http.StatusOK)
			before, err := os.ReadFile(h.app.Config.Path())
			if err != nil {
				t.Fatal(err)
			}
			putIncrementalExecutionSettings(t, h, map[string]any{
				"swarm": map[string]any{"incremental_execution": tc.value, "max_concurrent": 7},
			}, http.StatusBadRequest)
			after, err := os.ReadFile(h.app.Config.Path())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) || !settingsIncrementalExecution(t, h) {
				t.Fatal("an invalid boolean changed the live or saved configuration")
			}
		})
	}
}
