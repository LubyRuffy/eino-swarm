package config

import (
	"strings"
	"testing"
)

func TestClientsDefaultOffAndRepairBlankDirs(t *testing.T) {
	cfg := Default()
	if cfg.Clients.Enabled {
		t.Fatal("local clients must start off")
	}
	if cfg.Clients.RecentDays != DefaultClientRecentDays {
		t.Fatalf("recent days = %d", cfg.Clients.RecentDays)
	}
	cfg.normalizeClients()
	if cfg.Clients.ClaudeDir == "" || cfg.Clients.CodexDir == "" || cfg.Clients.CursorDir == "" {
		t.Fatalf("blank dirs were not filled: %+v", cfg.Clients)
	}
	custom := cfg.Clients
	custom.Enabled = true
	custom.ClaudeDir = t.TempDir()
	custom.RecentDays = 0
	custom.RunningStaleSeconds = 0
	cfg.Clients = custom
	cfg.normalizeClients()
	if !cfg.Clients.Enabled {
		t.Fatal("an explicit on switch was cleared")
	}
	if cfg.Clients.ClaudeDir != custom.ClaudeDir {
		t.Fatalf("custom dir replaced: %s", cfg.Clients.ClaudeDir)
	}
	if cfg.Clients.RecentDays != DefaultClientRecentDays || cfg.Clients.RunningStaleSeconds != DefaultClientStaleSeconds {
		t.Fatalf("broken numbers not repaired: %+v", cfg.Clients)
	}
	// A sample prompt must not become a directory default.
	for _, dir := range []string{cfg.Clients.ClaudeDir, cfg.Clients.CodexDir, cfg.Clients.CursorDir} {
		if strings.Contains(strings.ToLower(dir), "notes.md") {
			t.Fatalf("example leaked into %s", dir)
		}
	}
}
