package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"gopkg.in/yaml.v3"
)

func defaultReasoningEffortValue(t *testing.T, cfg *Config) string {
	t.Helper()
	raw, err := json.Marshal(cfg.SwarmSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	var swarm struct {
		DefaultReasoningEffort *string `json:"default_reasoning_effort"`
	}
	if err := json.Unmarshal(raw, &swarm); err != nil {
		t.Fatal(err)
	}
	if swarm.DefaultReasoningEffort == nil {
		t.Fatal("swarm configuration is missing default_reasoning_effort")
	}
	return *swarm.DefaultReasoningEffort
}

// A missing setting must preserve the existing per-call reasoning behavior.
func TestDefaultReasoningEffortDefaultsToExistingCallBehavior(t *testing.T) {
	for _, key := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL"} {
		t.Setenv(key, "")
	}
	if got := defaultReasoningEffortValue(t, Default()); got != "" {
		t.Fatalf("fresh default=%q, want empty", got)
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
		if got := defaultReasoningEffortValue(t, cfg); got != "" {
			t.Fatalf("omitted setting=%q, want empty", got)
		}
	}
}

func TestDefaultReasoningEffortNormalizesAndPersists(t *testing.T) {
	for _, key := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL"} {
		t.Setenv(key, "")
	}
	for _, tc := range []struct{ name, value, want string }{
		{"blank", "", ""},
		{"high normalization", " HIGH ", "high"},
		{"low", "low", "low"},
		{"medium normalization", " Medium ", "medium"},
		{"invalid uses default", "unsupported", ""},
		{"inherit is not a level", "inherit", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			raw := "swarm:\n  default_reasoning_effort: " + strconv.Quote(tc.value) + "\n"
			if err := os.WriteFile(filepath.Join(dir, FileName), []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := defaultReasoningEffortValue(t, cfg); got != tc.want {
				t.Fatalf("loaded effort=%q, want %q", got, tc.want)
			}
			if err := cfg.Replace(cfg.Snapshot()); err != nil {
				t.Fatal(err)
			}
			stored, err := os.ReadFile(cfg.Path())
			if err != nil {
				t.Fatal(err)
			}
			var saved struct {
				Swarm map[string]any `yaml:"swarm"`
			}
			if err := yaml.Unmarshal(stored, &saved); err != nil {
				t.Fatal(err)
			}
			if got, present := saved.Swarm["default_reasoning_effort"]; !present || got != tc.want {
				t.Fatalf("saved effort=%v present=%t, want %q", got, present, tc.want)
			}
			again, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := defaultReasoningEffortValue(t, again); got != tc.want {
				t.Fatalf("reloaded effort=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestDefaultReasoningEffortReplaceNormalizesTheSavedCandidate(t *testing.T) {
	for _, key := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL"} {
		t.Setenv(key, "")
	}
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	next := cfg.Snapshot()
	if err := json.Unmarshal([]byte(`{"default_reasoning_effort":" HIGH ","max_concurrent":4}`), &next.Swarm); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Replace(next); err != nil {
		t.Fatal(err)
	}
	again, err := Load(cfg.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := defaultReasoningEffortValue(t, again); got != "high" || again.SwarmSnapshot().MaxConcurrent != 4 {
		t.Fatalf("saved candidate effort=%q concurrent=%d", got, again.SwarmSnapshot().MaxConcurrent)
	}
}
