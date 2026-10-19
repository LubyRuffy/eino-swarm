package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMemoryReviewEffortConfigNormalizesAndRoundTripsWithoutOptingIn(t *testing.T) {
	for _, tc := range []struct{ name, yaml, want string }{
		{"legacy missing", "log:\n  level: info\n", ""},
		{"blank inherits", "memory:\n  review_reasoning_effort: ''\n", ""},
		{"low", "memory:\n  review_reasoning_effort: low\n", "low"},
		{"medium normalization", "memory:\n  review_reasoning_effort: ' MEDIUM '\n", "medium"},
		{"high normalization", "memory:\n  review_reasoning_effort: HIGH\n", "high"},
		{"invalid inherits", "memory:\n  review_reasoning_effort: unsupported\n", ""},
		{"off cannot disable", "memory:\n  review_reasoning_effort: off\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL"} {
				t.Setenv(key, "")
			}
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, FileName), []byte(tc.yaml), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			assertMemoryReviewEffortWire(t, cfg.Memory, tc.want)
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			again, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			assertMemoryReviewEffortWire(t, again.Memory, tc.want)
		})
	}
	assertMemoryReviewEffortWire(t, Default().Memory, "")
}

func assertMemoryReviewEffortWire(t *testing.T, memory MemoryConfig, want string) {
	t.Helper()
	raw, err := json.Marshal(memory)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if got, ok := wire["review_reasoning_effort"]; !ok || got != want {
		t.Fatalf("review effort wire=%v present=%v want=%q", got, ok, want)
	}
}

func TestMemoryReviewEffortOverridesOnlyKnownExplicitLevels(t *testing.T) {
	for _, tc := range []struct{ configured, inherited, want string }{
		{"low", "high", "low"}, {" MEDIUM ", "high", "medium"}, {"HIGH", "low", "high"},
		{"", "high", "high"}, {"", "", ""}, {"unsupported", "high", "high"},
		{"none", "", ""}, {"off", "high", "high"}, {"max", "medium", "medium"},
	} {
		if got := (MemoryConfig{ReviewReasoningEffort: tc.configured}).ReviewEffort(tc.inherited); got != tc.want {
			t.Fatalf("review effort override=%q inherited=%q got=%q want=%q", tc.configured, tc.inherited, got, tc.want)
		}
	}
}
