package server_test

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

func TestClientTaskPagesEarlierLines(t *testing.T) {
	h := newHarness(t)
	root := t.TempDir()
	var body strings.Builder
	body.WriteString("{\"type\":\"user\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"the request\"}]}}\n")
	for i := 0; i < 70; i++ {
		fmt.Fprintf(&body, "{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"line-%d\"}]}}\n", i)
	}
	body.WriteString("{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"latest reply\"}]}}\n")
	path := filepath.Join(root, "projects", "work", "paged.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := h.app.Engine.Config()
	cfg.Clients.Enabled = true
	cfg.Clients.ClaudeDir = root
	cfg.Clients.CodexDir = filepath.Join(root, "no-codex")
	cfg.Clients.CursorDir = filepath.Join(root, "no-cursor")
	h.json(http.MethodGet, "/api/clients/task?id=claude:paged&before=nope", nil, http.StatusBadRequest)
	q := url.Values{"id": {"claude:paged"}}
	tail := h.json(http.MethodGet, "/api/clients/task?"+q.Encode(), nil, http.StatusOK)
	if tail["older"] != true {
		t.Fatalf("tail = %v", tail["older"])
	}
	entries, _ := tail["entries"].([]any)
	if len(entries) == 0 || entryText(entries[0]) != "the request" || entryText(entries[len(entries)-1]) != "latest reply" {
		t.Fatalf("live edge = %v", entries)
	}
	before, _ := tail["before"].(float64)
	if before <= 0 {
		t.Fatalf("before = %v", tail["before"])
	}
	q.Set("before", fmt.Sprintf("%.0f", before))
	older := h.json(http.MethodGet, "/api/clients/task?"+q.Encode(), nil, http.StatusOK)
	found := false
	for _, raw := range older["entries"].([]any) {
		text := entryText(raw)
		if text == "latest reply" {
			t.Fatal("earlier page repeated the live reply")
		}
		if text == "line-0" {
			found = true
		}
	}
	if !found {
		t.Fatal("earlier page did not include the first reply")
	}
}

func entryText(raw any) string {
	row, _ := raw.(map[string]any)
	text, _ := row["text"].(string)
	return text
}
