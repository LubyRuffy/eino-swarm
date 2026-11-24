package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAutoDreamDefaultsOnAndKeepsExplicitChoiceOnReload(t *testing.T) {
	for _, name := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL"} {
		t.Setenv(name, "")
	}
	for _, tc := range []struct {
		name string
		yaml string
		want bool
	}{
		{"new installation", "", true},
		{"legacy memory section", "memory:\n  enabled: false\n  auto_review: false\n", true},
		{"explicit on", "memory:\n  auto_dream: true\n", true},
		{"explicit off", "memory:\n  auto_dream: false\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.yaml != "" {
				if err := os.WriteFile(filepath.Join(dir, FileName), []byte(tc.yaml), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			assertAutoDreamSetting(t, cfg, tc.want)
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			reloaded, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			assertAutoDreamSetting(t, reloaded, tc.want)
		})
	}
}

func assertAutoDreamSetting(t *testing.T, cfg *Config, want bool) {
	t.Helper()
	raw, err := json.Marshal(cfg.Memory)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		AutoDream *bool `json:"auto_dream"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.AutoDream == nil || *wire.AutoDream != want {
		t.Fatalf("memory must expose auto_dream=%t, got %s", want, raw)
	}
}
