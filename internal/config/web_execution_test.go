package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWebExecutionDefaultsToAutomaticAccountRouting(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(cfg.Tools)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["web_execution"] != "auto" {
		t.Fatalf("missing account-aware tool default: %s", raw)
	}
}

func TestWebExecutionRejectsAnUnknownSavedMode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("tools:\n  web_execution: unknown\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("unknown web execution mode was silently accepted")
	}
}

func TestWebExecutionSelectionSurvivesSaveAndReload(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"", WebExecutionAuto}, {"auto", WebExecutionAuto},
		{" Local ", WebExecutionLocal}, {"CLOUD", WebExecutionCloud},
	} {
		t.Run(tc.input, func(t *testing.T) {
			cfg, err := Load(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := cfg.Update(func(next *Config) error { next.Tools.WebExecution = tc.input; return nil }); err != nil {
				t.Fatal(err)
			}
			restored, err := Load(cfg.DataDir())
			if err != nil || restored.Tools.WebExecution != tc.want {
				t.Fatalf("execution selection did not survive reload: %v, %v", restored, err)
			}
		})
	}
	if err := Default().Update(func(next *Config) error { next.Tools.WebExecution = "unknown"; return nil }); err == nil {
		t.Fatal("an invalid live execution selection was accepted")
	}
}
