package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const DefaultMCPTimeoutSeconds = 60

// DefaultMCPToolPageSize is the list_tools proxy's optional per-call limit.
const DefaultMCPToolPageSize = 10

// MCPConfig lists explicitly configured external tool servers. No server is
// seeded from the environment or connected merely by opening settings.
type MCPConfig struct {
	Servers []MCPServer `yaml:"servers" json:"servers"`
}

// MCPServer is one local command or remote endpoint. Env and Headers are
// private configuration; HTTP readers receive only their key names.
type MCPServer struct {
	ID             string            `yaml:"id" json:"id"`
	Name           string            `yaml:"name" json:"name"`
	Transport      string            `yaml:"transport" json:"transport"`
	Command        string            `yaml:"command,omitempty" json:"command"`
	Args           []string          `yaml:"args,omitempty" json:"args"`
	Env            map[string]string `yaml:"env,omitempty" json:"-"`
	URL            string            `yaml:"url,omitempty" json:"url"`
	Headers        map[string]string `yaml:"headers,omitempty" json:"-"`
	Enabled        bool              `yaml:"enabled" json:"enabled"`
	TimeoutSeconds int               `yaml:"timeout_seconds" json:"timeout_seconds"`
	// DisabledTools are remote tool names the composer switched off. An empty
	// list leaves every discovered tool available. This does not disable the
	// server, and it is not part of the session fingerprint.
	DisabledTools []string `yaml:"disabled_tools,omitempty" json:"disabled_tools,omitempty"`
}

// Timeout bounds connection, discovery and each tool call independently.
func (s MCPServer) Timeout() time.Duration {
	if s.TimeoutSeconds <= 0 {
		return DefaultMCPTimeoutSeconds * time.Second
	}
	return time.Duration(s.TimeoutSeconds) * time.Second
}

var mcpIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`)

// Validate rejects invalid inputs without including private values in errors.
func (s MCPServer) Validate() error {
	if !mcpIDPattern.MatchString(s.ID) {
		return fmt.Errorf("mcp: id must contain 1–32 letters, digits, underscores or hyphens")
	}
	if s.TimeoutSeconds < 0 || s.TimeoutSeconds > 3600 {
		return fmt.Errorf("mcp: timeout_seconds must be between 0 and 3600")
	}
	switch s.Transport {
	case "stdio":
		if strings.TrimSpace(s.Command) == "" || strings.ContainsRune(s.Command, 0) {
			return fmt.Errorf("mcp: stdio requires a command")
		}
		for _, arg := range s.Args {
			if strings.ContainsRune(arg, 0) {
				return fmt.Errorf("mcp: command argument contains a null byte")
			}
		}
	case "streamable_http", "sse":
		u, err := url.Parse(s.URL)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
			return fmt.Errorf("mcp: remote transport requires an HTTP(S) URL without user info or fragment")
		}
	default:
		return fmt.Errorf("mcp: transport must be stdio, streamable_http or sse")
	}
	for key, value := range s.Env {
		if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, 0) {
			return fmt.Errorf("mcp: invalid environment entry")
		}
	}
	for key, value := range s.Headers {
		if key == "" || strings.IndexFunc(key, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", r))
		}) >= 0 || strings.ContainsAny(value, "\r\n\x00") {
			return fmt.Errorf("mcp: invalid request header")
		}
	}
	if _, err := cleanMCPDisabledTools(s.DisabledTools); err != nil {
		return err
	}
	return nil
}

const maxMCPDisabledTools = 256

// cleanMCPDisabledTools keeps the first occurrence of each remote name.
// Names are not checked against a live catalog: the server may be offline,
// and a tool that later disappears stays recorded until the user turns it on.
func cleanMCPDisabledTools(names []string) ([]string, error) {
	if len(names) > maxMCPDisabledTools {
		return nil, fmt.Errorf("mcp: too many disabled tools")
	}
	if len(names) == 0 {
		return []string{}, nil
	}
	out := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name == "" || len(name) > maxMCPDisabledTools || strings.ContainsRune(name, 0) {
			return nil, fmt.Errorf("mcp: invalid disabled tool name")
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out, nil
}

// NormalizeDisabledTools returns the stored remote-tool exceptions. Callers
// that publish a server use it so duplicates are not written twice.
func NormalizeDisabledTools(names []string) ([]string, error) {
	return cleanMCPDisabledTools(names)
}

func (s *MCPServer) normalizeDisabledTools() error {
	cleaned, err := NormalizeDisabledTools(s.DisabledTools)
	if err != nil {
		return err
	}
	s.DisabledTools = cleaned
	return nil
}

func (c *Config) validateMCP() error {
	seen := make(map[string]bool, len(c.MCP.Servers))
	for i := range c.MCP.Servers {
		if err := c.MCP.Servers[i].normalizeDisabledTools(); err != nil {
			return err
		}
		if err := c.MCP.Servers[i].Validate(); err != nil {
			return err
		}
		if seen[c.MCP.Servers[i].ID] {
			return fmt.Errorf("mcp: duplicate server id")
		}
		seen[c.MCP.Servers[i].ID] = true
	}
	return nil
}
