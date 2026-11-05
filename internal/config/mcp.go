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
	return nil
}

func (c *Config) validateMCP() error {
	seen := make(map[string]bool, len(c.MCP.Servers))
	for _, s := range c.MCP.Servers {
		if err := s.Validate(); err != nil {
			return err
		}
		if seen[s.ID] {
			return fmt.Errorf("mcp: duplicate server id")
		}
		seen[s.ID] = true
	}
	return nil
}
