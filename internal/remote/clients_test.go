package remote

import (
	"os"
	"path/filepath"
	"testing"

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
	cfg.Clients.CursorDir = root
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	on := Handle(e, config.RemoteConfig{}, Request{ID: "c", Op: OpClients}, "relay", "s")
	if !on.OK || on.Clients == nil || !on.Clients.Enabled {
		t.Fatalf("clients op = %+v", on.Clients)
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
