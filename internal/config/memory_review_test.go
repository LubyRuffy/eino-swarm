package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Review is background extraction. Blank is the background default (low),
// kept blank on the wire so a later default change still reaches it; a typo
// takes the default; only an explicit inherit follows the original turn.
func TestMemoryReviewEffortConfigNormalizesAndRoundTrips(t *testing.T) {
	for _, tc := range []struct{ name, yaml, want string }{
		{"legacy missing", "log:\n  level: info\n", ""},
		{"blank default", "memory:\n  review_reasoning_effort: ''\n", ""},
		{"explicit inherit", "memory:\n  review_reasoning_effort: ' Inherit '\n", "inherit"},
		{"low", "memory:\n  review_reasoning_effort: low\n", "low"},
		{"medium normalization", "memory:\n  review_reasoning_effort: ' MEDIUM '\n", "medium"},
		{"high normalization", "memory:\n  review_reasoning_effort: HIGH\n", "high"},
		{"invalid takes the default", "memory:\n  review_reasoning_effort: unsupported\n", ""},
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

// Only the blank default is optional: an endpoint that rejects it may be
// asked again without it. A level the human chose, or the conversation's own
// level under inherit, keeps the endpoint's error.
func TestMemoryReviewEffortInheritsOnlyWhenAskedTo(t *testing.T) {
	for _, tc := range []struct {
		configured, inherited, want string
		optional                    bool
	}{
		{"low", "high", "low", false}, {" MEDIUM ", "high", "medium", false}, {"HIGH", "low", "high", false},
		{"", "high", "low", true}, {"", "", "low", true}, {"unsupported", "high", "low", true},
		{"inherit", "high", "high", false}, {"INHERIT", "", "", false}, {"inherit", "max", "", false},
		{"none", "", "low", true}, {"off", "high", "low", true},
	} {
		got, optional := (MemoryConfig{ReviewReasoningEffort: tc.configured}).ReviewEffort(tc.inherited)
		if got != tc.want || optional != tc.optional {
			t.Fatalf("review effort setting=%q inherited=%q got=%q optional=%v want=%q optional=%v", tc.configured, tc.inherited, got, optional, tc.want, tc.optional)
		}
	}
}

func TestPolicyEffortDefaultsToBlankLowAndRoundTrips(t *testing.T) {
	for _, tc := range []struct{ name, yaml, want string }{
		{"legacy missing", "log:\n  level: info\n", ""},
		{"explicit inherit", "swarm:\n  policy_reasoning_effort: inherit\n", "inherit"},
		{"high", "swarm:\n  policy_reasoning_effort: ' HIGH '\n", "high"},
		{"invalid takes the default", "swarm:\n  policy_reasoning_effort: max\n", ""},
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
			if cfg.Swarm.PolicyReasoningEffort != tc.want {
				t.Fatalf("loaded %q want %q", cfg.Swarm.PolicyReasoningEffort, tc.want)
			}
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			again, err := Load(dir)
			if err != nil || again.Swarm.PolicyReasoningEffort != tc.want {
				t.Fatalf("round trip %q err=%v want %q", again.Swarm.PolicyReasoningEffort, err, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		setting, conversation, want string
		optional                    bool
	}{
		{"", "high", "low", true}, {"inherit", "high", "high", false}, {"inherit", "", "", false}, {"medium", "", "medium", false},
	} {
		got, optional := (SwarmConfig{PolicyReasoningEffort: tc.setting}).PolicyEffort(tc.conversation)
		if got != tc.want || optional != tc.optional {
			t.Fatalf("policy effort setting=%q conversation=%q got %q/%v want %q/%v", tc.setting, tc.conversation, got, optional, tc.want, tc.optional)
		}
	}
	if got := AuxiliaryReasoningChoices(); len(got) != 4 || got[0] != ReasoningInherit {
		t.Fatalf("choices=%v", got)
	}
}

// A blank or negative cap would let one listing flood every later model call
// in a turn, so the stored value is repaired and the getter never returns it.
func TestToolResultLimitIsRepairedAndRoundTrips(t *testing.T) {
	for _, key := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL"} {
		t.Setenv(key, "")
	}
	for _, tc := range []struct {
		yaml string
		want int
	}{
		{"log:\n  level: info\n", DefaultToolResultMaxChars},
		{"tools:\n  result_max_chars: -5\n", DefaultToolResultMaxChars},
		{"tools:\n  result_max_chars: 12000\n", 12000},
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte(tc.yaml), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Tools.ResultMaxChars != tc.want || cfg.Tools.ToolResultLimit() != tc.want {
			t.Fatalf("%q: stored %d limit %d, want %d", tc.yaml, cfg.Tools.ResultMaxChars, cfg.Tools.ToolResultLimit(), tc.want)
		}
	}
	if got := (ToolsConfig{}).ToolResultLimit(); got != DefaultToolResultMaxChars {
		t.Fatalf("an unset cap must use the default, got %d", got)
	}
}
