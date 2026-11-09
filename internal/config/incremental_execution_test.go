package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"gopkg.in/yaml.v3"
)

func incrementalExecutionValue(t *testing.T, cfg *Config) bool {
	t.Helper()
	raw, err := json.Marshal(cfg.SwarmSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		IncrementalExecution *bool `json:"incremental_execution"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.IncrementalExecution == nil {
		t.Fatal("swarm configuration is missing incremental_execution")
	}
	return *settings.IncrementalExecution
}

// Existing installations must keep their ordinary manager behavior until the
// operator explicitly enables the experiment.
func TestIncrementalExecutionDefaultsOffForFreshAndOlderConfigurations(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	if incrementalExecutionValue(t, Default()) {
		t.Fatal("incremental execution must be opt-in")
	}
	for _, raw := range []string{"", "swarm:\n  max_concurrent: 4\n"} {
		dir := t.TempDir()
		if raw != "" {
			if err := os.WriteFile(filepath.Join(dir, FileName), []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		cfg, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if incrementalExecutionValue(t, cfg) {
			t.Fatal("loading an omitted experiment choice enabled incremental execution")
		}
	}
}

func TestIncrementalExecutionSavedChoiceSurvivesNormalizationAndReload(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	for _, enabled := range []bool{true, false} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			dir := t.TempDir()
			raw := "swarm:\n  incremental_execution: " + strconv.FormatBool(enabled) + "\n"
			if err := os.WriteFile(filepath.Join(dir, FileName), []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := incrementalExecutionValue(t, cfg); got != enabled {
				t.Fatalf("normalized choice=%t, want %t", got, enabled)
			}
			if err := cfg.Replace(cfg.Snapshot()); err != nil {
				t.Fatal(err)
			}
			stored, err := os.ReadFile(cfg.Path())
			if err != nil {
				t.Fatal(err)
			}
			var persisted struct {
				Swarm map[string]any `yaml:"swarm"`
			}
			if err := yaml.Unmarshal(stored, &persisted); err != nil {
				t.Fatal(err)
			}
			if got, ok := persisted.Swarm["incremental_execution"].(bool); !ok || got != enabled {
				t.Fatalf("saved incremental_execution=%v, want %t", persisted.Swarm["incremental_execution"], enabled)
			}
			reloaded, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := incrementalExecutionValue(t, reloaded); got != enabled {
				t.Fatalf("reloaded choice=%t, want %t", got, enabled)
			}
		})
	}
}
