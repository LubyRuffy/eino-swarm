package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A slow-thinking endpoint needs a longer initial wait without changing how
// quickly an already-started stream is declared silent.
func TestProviderFirstByteTimeoutIsIndependentOfStreamIdle(t *testing.T) {
	for _, tc := range []struct {
		name    string
		seconds int
		want    time.Duration
	}{
		{"omitted", 0, DefaultFirstByteTimeout},
		{"negative", -1, DefaultFirstByteTimeout},
		{"configured", 120, 120 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := Provider{TimeoutSeconds: 10, FirstByteTimeoutSeconds: tc.seconds}
			if got := p.FirstByteTimeout(); got != tc.want {
				t.Fatalf("first-byte wait=%v want %v", got, tc.want)
			}
			if p.Timeout() != 10*time.Second {
				t.Fatal("changing the initial wait must preserve stream idle")
			}
		})
	}
}

func TestProviderFirstByteTimeoutSurvivesSaveReloadAndOlderFiles(t *testing.T) {
	for _, tc := range []struct {
		name  string
		field string
		want  int
	}{
		{"older file", "", 30},
		{"invalid wait", "    first_byte_timeout_seconds: -2\n", 30},
		{"long wait", "    first_byte_timeout_seconds: 120\n", 120},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			raw := "models:\n  default: local\n  providers:\n  - id: local\n    model: test\n    timeout_seconds: 10\n" + tc.field
			if err := os.WriteFile(filepath.Join(dir, FileName), []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			p, _ := cfg.Provider("local")
			if p.FirstByteTimeoutSeconds != tc.want || p.TimeoutSeconds != 10 {
				t.Fatalf("loaded provider=%+v", p)
			}
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			reloaded, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			p, _ = reloaded.Provider("local")
			if p.FirstByteTimeoutSeconds != tc.want || p.TimeoutSeconds != 10 {
				t.Fatalf("reloaded provider=%+v", p)
			}
		})
	}
	if got := Default().Models.Providers[0].FirstByteTimeoutSeconds; got != 30 {
		t.Fatalf("fresh provider wait=%d", got)
	}
}
