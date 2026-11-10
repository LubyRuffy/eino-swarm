package config

import (
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
		{"legacy memory section", "memory:\n  enabled: true\n  auto_review: true\n", true},
		{"explicit on", "memory:\n  auto_dream: true\n", true},
		{"explicit off", "memory:\n  auto_dream: false\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.yaml != "" {
				if err := os.WriteFile(filepath.Join(dir, FileName), []byte(tc.yaml), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Memory.AutoDream != tc.want {
				t.Fatalf("loaded auto_dream=%t, want %t", cfg.Memory.AutoDream, tc.want)
			}
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			reloaded, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if reloaded.Memory.AutoDream != tc.want {
				t.Fatalf("reloaded auto_dream=%t, want %t", reloaded.Memory.AutoDream, tc.want)
			}
		})
	}
}
