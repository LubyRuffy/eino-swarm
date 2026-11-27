package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGraphDefaultsOnAndIgnoresTheOlderKeys(t *testing.T) {
	dir := t.TempDir()
	for _, test := range []struct {
		value string
		want  bool
	}{{"swarm:\n  max_concurrent: 4\n", true}, {"swarm:\n  work_ledger: false\n  knowledge: false\n", true}, {"swarm:\n  graph: false\n", false}} {
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte(test.value), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got := cfg.SwarmSnapshot().Graph; got != test.want {
			t.Fatalf("graph=%v, want %v for %q", got, test.want, test.value)
		}
	}
}
