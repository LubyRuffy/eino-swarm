package remote

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func TestPhoneListOmitsClientsUntilTheSwitchIsOn(t *testing.T) {
	e := testEngine(t)
	off := Handle(e, config.RemoteConfig{}, Request{ID: "l", Op: OpList}, "relay", "s")
	if !off.OK || off.Clients == nil || off.Clients.Enabled {
		t.Fatalf("off list = %+v", off.Clients)
	}
	root := t.TempDir()
	path := filepath.Join(root, "projects", "demo", "agent-transcripts", "u1", "u1.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("{\"role\":\"user\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"cursor live\"}]}}\n{\"role\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ack\"}]}}\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := e.Config()
	cfg.Clients.Enabled = true
	cfg.Clients.ClaudeDir = filepath.Join(root, "no-claude")
	cfg.Clients.CodexDir = filepath.Join(root, "no-codex")
	cfg.Clients.CursorDir = root
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	var on Response
	for {
		on = Handle(e, config.RemoteConfig{}, Request{ID: "c", Op: OpClients}, "relay", "s")
		if on.OK && on.Clients != nil && !on.Clients.Pending && len(on.Clients.Tools) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("clients op = %+v", on.Clients)
		}
		time.Sleep(5 * time.Millisecond)
	}
	var found bool
	for _, tool := range on.Clients.Tools {
		if tool.ID != "cursor" {
			continue
		}
		if len(tool.Tasks) != 1 || tool.Tasks[0].Status != "running" {
			t.Fatalf("cursor = %+v", tool)
		}
		found = true
	}
	if !found {
		t.Fatalf("tools = %+v", on.Clients.Tools)
	}
	paged := Handle(e, config.RemoteConfig{}, Request{ID: "p", Op: OpList, Group: "recent"}, "relay", "s")
	if paged.Clients != nil {
		t.Fatalf("a section page must not resend clients: %+v", paged.Clients)
	}
}

func TestClientReadPagesEarlierLines(t *testing.T) {
	e := testEngine(t)
	root := t.TempDir()
	path := filepath.Join(root, "projects", "work", "paged.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	body.WriteString("{\"type\":\"user\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"the request\"}]}}\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&body, "{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"line-%d\"}]}}\n", i)
	}
	body.WriteString("{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"latest reply\"}]}}\n")
	if err := os.WriteFile(path, []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := e.Config()
	cfg.Clients.Enabled = true
	cfg.Clients.ClaudeDir = root
	cfg.Clients.CodexDir = filepath.Join(root, "no-codex")
	cfg.Clients.CursorDir = filepath.Join(root, "no-cursor")
	tail := Handle(e, config.RemoteConfig{}, Request{ID: "r", Op: OpClientRead, TaskID: "claude:paged"}, "relay", "s")
	if !tail.OK || tail.ClientView == nil || !tail.ClientView.Older || tail.ClientView.Before <= 0 {
		t.Fatalf("tail = %+v", tail.ClientView)
	}
	last := tail.ClientView.Entries[len(tail.ClientView.Entries)-1].Text
	if tail.ClientView.Entries[0].Text != "the request" || last != "latest reply" {
		t.Fatalf("live edge = %+v", tail.ClientView.Entries)
	}
	older := Handle(e, config.RemoteConfig{}, Request{
		ID: "o", Op: OpClientRead, TaskID: "claude:paged", Before: tail.ClientView.Before,
	}, "relay", "s")
	if !older.OK || older.ClientView == nil {
		t.Fatalf("earlier = %+v", older)
	}
	found := false
	for _, entry := range older.ClientView.Entries {
		if entry.Text == "latest reply" {
			t.Fatal("earlier page repeated the live reply")
		}
		if entry.Text == "line-0" {
			found = true
		}
	}
	if !found {
		t.Fatal("earlier page did not include the first reply")
	}
	bad := Handle(e, config.RemoteConfig{}, Request{
		ID: "b", Op: OpClientRead, TaskID: "claude:paged", Before: -1,
	}, "relay", "s")
	if bad.OK || bad.Code != "bad_request" {
		t.Fatalf("negative before = %+v", bad)
	}
}
