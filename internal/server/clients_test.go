package server_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClientsStayHiddenUntilTheSwitchIsOn(t *testing.T) {
	h := newHarness(t)
	off := h.json(http.MethodGet, "/api/clients", nil, http.StatusOK)
	if off["enabled"] != false {
		t.Fatalf("default catalog = %v", off)
	}
	root := t.TempDir()
	path := filepath.Join(root, "projects", "work", "s1.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("{\"type\":\"user\",\"cwd\":\"/work/demo\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"fresh task\"}]}}\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
	cfg := h.app.Engine.Config()
	cfg.Clients.Enabled = true
	cfg.Clients.ClaudeDir = root
	cfg.Clients.CodexDir = filepath.Join(root, "no-codex")
	cfg.Clients.CursorDir = filepath.Join(root, "no-cursor")
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var on map[string]any
	for {
		on = h.json(http.MethodGet, "/api/clients", nil, http.StatusOK)
		if on["enabled"] == true && on["pending"] != true {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("enabled catalog = %v", on)
		}
		time.Sleep(5 * time.Millisecond)
	}
	tools, _ := on["tools"].([]any)
	if len(tools) != 3 {
		t.Fatalf("tools = %v", on["tools"])
	}
	first, _ := tools[0].(map[string]any)
	tasks, _ := first["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("claude tasks = %v", first)
	}
}
