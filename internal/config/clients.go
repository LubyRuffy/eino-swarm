package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultClientRecentDays is the sidebar window. Older tasks stay
	// behind More so a year of transcripts does not bury the list.
	DefaultClientRecentDays = 3
	// DefaultClientStaleSeconds is how long a session file may sit quiet
	// before a tool without an explicit finish marker counts as done.
	// Codex keeps an open task running past this; the window is for the
	// other two, whose files only show that they were written.
	DefaultClientStaleSeconds = 90
)

// ClientsConfig is the read-only mount of local agent transcripts.
// Enabled is the switch. Blank directories are filled with that tool's
// own default location on this machine; a set path is left alone.
type ClientsConfig struct {
	Enabled             bool   `yaml:"enabled" json:"enabled"`
	ClaudeDir           string `yaml:"claude_dir" json:"claude_dir"`
	CodexDir            string `yaml:"codex_dir" json:"codex_dir"`
	CursorDir           string `yaml:"cursor_dir" json:"cursor_dir"`
	RecentDays          int    `yaml:"recent_days" json:"recent_days"`
	RunningStaleSeconds int    `yaml:"running_stale_seconds" json:"running_stale_seconds"`
}

// DefaultClients is the install default: off, standard directories, three days.
func DefaultClients() ClientsConfig {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return ClientsConfig{
		Enabled:             false,
		ClaudeDir:           joinHome(home, ".claude"),
		CodexDir:            joinHome(home, ".codex"),
		CursorDir:           joinHome(home, ".cursor"),
		RecentDays:          DefaultClientRecentDays,
		RunningStaleSeconds: DefaultClientStaleSeconds,
	}
}

func joinHome(home, name string) string {
	if strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, name)
}

func (c *Config) normalizeClients() {
	d := DefaultClients()
	if strings.TrimSpace(c.Clients.ClaudeDir) == "" {
		c.Clients.ClaudeDir = d.ClaudeDir
	}
	if strings.TrimSpace(c.Clients.CodexDir) == "" {
		c.Clients.CodexDir = d.CodexDir
	}
	if strings.TrimSpace(c.Clients.CursorDir) == "" {
		c.Clients.CursorDir = d.CursorDir
	}
	if c.Clients.RecentDays <= 0 {
		c.Clients.RecentDays = d.RecentDays
	}
	if c.Clients.RunningStaleSeconds <= 0 {
		c.Clients.RunningStaleSeconds = d.RunningStaleSeconds
	}
}

// RecentWindow is how far back the first page looks.
func (c ClientsConfig) RecentWindow() time.Duration {
	days := c.RecentDays
	if days <= 0 {
		days = DefaultClientRecentDays
	}
	return time.Duration(days) * 24 * time.Hour
}

// RunningStale is the quiet gap that ends a session with no finish marker.
func (c ClientsConfig) RunningStale() time.Duration {
	sec := c.RunningStaleSeconds
	if sec <= 0 {
		sec = DefaultClientStaleSeconds
	}
	return time.Duration(sec) * time.Second
}
