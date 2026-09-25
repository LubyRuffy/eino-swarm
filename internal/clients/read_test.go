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

func TestReadKeepsTheCommandAndSkipsInjectedContext(t *testing.T) {
	root := t.TempDir()
	body := "{\"type\":\"user\",\"message\":{\"content\":\"<command-message>tool</command-message> <command-name>/tool</command-name>\"}}\n" +
		"{\"type\":\"user\",\"isMeta\":true,\"turnCompanion\":true,\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"injected context\"}]}}\n" +
		"{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"line one\\nline two\"}]}}\n"
	writeFileTime(t, filepath.Join(root, "projects", "work", "s1.jsonl"), body, time.Now())
	cfg := config.ClientsConfig{
		Enabled: true, ClaudeDir: root,
		CodexDir: filepath.Join(root, "no-codex"), CursorDir: filepath.Join(root, "no-cursor"),
		RecentDays: 3, RunningStaleSeconds: 90,
	}
	got, ok := Read(cfg, "claude:s1", time.Now())
	if !ok || got.Title != "/tool" {
		t.Fatalf("title = %+v ok=%v", got.Title, ok)
	}
	var texts []string
	for _, e := range got.Entries {
		texts = append(texts, e.Role+":"+e.Text)
	}
	joined := strings.Join(texts, "|")
	if !strings.Contains(joined, "user:/tool") || strings.Contains(joined, "injected context") || !strings.Contains(joined, "line one\nline two") {
		t.Fatalf("entries = %s", joined)
	}
}

func TestReadUsesThePathTheListAlreadyFound(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "elsewhere", "s1.jsonl")
	body := "{\"type\":\"user\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"open session\"}]}}\n"
	writeFileTime(t, path, body, time.Now())
	rememberSession("claude:cached", path)
	cfg := config.ClientsConfig{
		Enabled: true, ClaudeDir: filepath.Join(root, "missing"),
		CodexDir: filepath.Join(root, "no-codex"), CursorDir: filepath.Join(root, "no-cursor"),
		RecentDays: 3, RunningStaleSeconds: 90,
	}
	got, ok := Read(cfg, "claude:cached", time.Now())
	if !ok || got.Title != "open session" {
		t.Fatalf("cached read = %+v ok=%v", got, ok)
	}
}
