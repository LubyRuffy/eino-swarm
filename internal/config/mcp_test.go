package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestMCPConfigPersistsAndSnapshotsOwnTheirSecrets(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := MCPServer{ID: "local", Transport: "stdio", Command: "command", Args: []string{"one"}, Env: map[string]string{"TOKEN": "secret"}, Headers: map[string]string{"X-Token": "secret"}}
	if s.Timeout() != time.Minute {
		t.Fatal(s.Timeout())
	}
	s.TimeoutSeconds = 2
	if s.Timeout() != 2*time.Second {
		t.Fatal(s.Timeout())
	}
	if err := cfg.Update(func(c *Config) error { c.MCP.Servers = []MCPServer{s}; return nil }); err != nil {
		t.Fatal(err)
	}
	copy := cfg.Snapshot()
	copy.MCP.Servers[0].Args[0] = "changed"
	copy.MCP.Servers[0].Env["TOKEN"] = "changed"
	copy.MCP.Servers[0].Headers["X-Token"] = "changed"
	if cfg.MCP.Servers[0].Args[0] != "one" || cfg.MCP.Servers[0].Env["TOKEN"] != "secret" || cfg.MCP.Servers[0].Headers["X-Token"] != "secret" {
		t.Fatal("snapshot aliases live config")
	}
	reloaded, err := Load(cfg.DataDir())
	if err != nil || reloaded.MCP.Servers[0].Env["TOKEN"] != "secret" {
		t.Fatalf("reload: %v", err)
	}
	info, err := os.Stat(cfg.Path())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("private config: %v %v", info, err)
	}
	if err := cfg.Update(func(c *Config) error { c.MCP.Servers = append(c.MCP.Servers, s); return nil }); err == nil {
		t.Fatal("duplicate accepted")
	}
	if len(cfg.MCP.Servers) != 1 {
		t.Fatal("invalid update published")
	}
}

func TestMCPConfigRejectsInvalidPrivateEntriesWithoutEchoingThem(t *testing.T) {
	valid := MCPServer{ID: "remote", Transport: "streamable_http", URL: "https://example.invalid/mcp"}
	for _, transport := range []string{"sse", "streamable_http"} {
		s := valid
		s.Transport = transport
		if err := s.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, change := range []func(*MCPServer){
		func(s *MCPServer) { s.ID = "bad.id" },
		func(s *MCPServer) { s.Transport = "unknown" },
		func(s *MCPServer) { s.TimeoutSeconds = -1 },
		func(s *MCPServer) { s.TimeoutSeconds = 3601 },
		func(s *MCPServer) { s.URL = "http://user:private-secret@example.invalid" },
		func(s *MCPServer) { s.URL = "file:///private-secret" },
		func(s *MCPServer) { s.URL = "http://%zz" },
		func(s *MCPServer) { s.Transport = "stdio"; s.Command = "\x00private-secret" },
		func(s *MCPServer) { s.Transport = "stdio"; s.Command = " " },
		func(s *MCPServer) { s.Transport = "stdio"; s.Command = "cmd"; s.Args = []string{"private-secret\x00"} },
		func(s *MCPServer) { s.Env = map[string]string{"": "private-secret"} },
		func(s *MCPServer) { s.Env = map[string]string{"BAD=KEY": "private-secret"} },
		func(s *MCPServer) { s.Env = map[string]string{"OK": "private-secret\x00"} },
		func(s *MCPServer) { s.Headers = map[string]string{"Bad Key": "private-secret"} },
		func(s *MCPServer) { s.Headers = map[string]string{"": "private-secret"} },
		func(s *MCPServer) { s.Headers = map[string]string{"X-Token": "private-secret\r\n"} },
	} {
		s := valid
		change(&s)
		if err := s.Validate(); err == nil || strings.Contains(err.Error(), "private-secret") {
			t.Fatalf("validation=%v", err)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/"+FileName, []byte("mcp:\n  servers:\n    - id: broken\n      transport: invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("invalid file loaded")
	}
}
