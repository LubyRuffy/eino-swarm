package clients

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func TestReadShowsTheSessionWithoutOpeningATurn(t *testing.T) {
	root := t.TempDir()
	body := "{\"type\":\"user\",\"cwd\":\"/work/demo\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"open session\"}]}}\n" +
		"{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"tool_use\",\"name\":\"read\"},{\"type\":\"text\",\"text\":\"found the file\"}]}}\n"
	writeFileTime(t, filepath.Join(root, "projects", "work", "s1.jsonl"), body, time.Now())
	cfg := config.ClientsConfig{
		Enabled: true, ClaudeDir: root,
		CodexDir: filepath.Join(root, "no-codex"), CursorDir: filepath.Join(root, "no-cursor"),
		RecentDays: 3, RunningStaleSeconds: 90,
	}
	got, ok := Read(cfg, "claude:s1", time.Now())
	if !ok || got.Title != "open session" || got.Status != StatusRunning {
		t.Fatalf("transcript = %+v ok=%v", got, ok)
	}
	var roles []string
	for _, e := range got.Entries {
		roles = append(roles, e.Role+":"+e.Text)
	}
	joined := strings.Join(roles, "|")
	if !strings.Contains(joined, "user:open session") || !strings.Contains(joined, "tool:read") || !strings.Contains(joined, "assistant:found the file") {
		t.Fatalf("entries = %s", joined)
	}
	if _, ok := Read(cfg, "claude:../s1", time.Now()); ok {
		t.Fatal("a path in the id was readable")
	}
	cfg.Enabled = false
	if _, ok := Read(cfg, "claude:s1", time.Now()); ok {
		t.Fatal("switch off still read a session")
	}
}
