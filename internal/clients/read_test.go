package clients

import (
	"encoding/json"
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
	raw, err := json.Marshal(got)
	if err != nil || !strings.Contains(string(raw), `"entries":[`) {
		t.Fatalf("entries json = %s err=%v", raw, err)
	}
	onlyMeta := "{\"type\":\"user\",\"isMeta\":true,\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"injected context\"}]}}\n"
	writeFileTime(t, filepath.Join(root, "projects", "work", "s2.jsonl"), onlyMeta, time.Now())
	empty, ok := Read(cfg, "claude:s2", time.Now())
	raw, err = json.Marshal(empty)
	if !ok || err != nil || !strings.Contains(string(raw), `"entries":[]`) {
		t.Fatalf("empty entries json = %s ok=%v err=%v", raw, ok, err)
	}
}

func TestLongSessionStillShowsTheOpeningRequest(t *testing.T) {
	entries := []Entry{{Role: "user", Text: "the request"}}
	for i := 0; i < entryCap; i++ {
		entries = append(entries, Entry{Role: "tool", Text: "tool"})
	}
	got, cut := TrimKeepingRequest(entries, entryCap)
	if !cut || len(got) != entryCap || got[0].Role != "user" || got[0].Text != "the request" {
		t.Fatalf("trimmed = %+v cut=%v", got[:1], cut)
	}
	if got[1].Role != "tool" {
		t.Fatalf("tail did not follow the request: %+v", got[1])
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

func TestThinkingStaysNextToTheToolItPreceded(t *testing.T) {
	claude := []byte("{\"type\":\"assistant\",\"message\":{\"content\":[" +
		"{\"type\":\"thinking\",\"thinking\":\"checked the path\"}," +
		"{\"type\":\"tool_use\",\"name\":\"read\"}," +
		"{\"type\":\"text\",\"text\":\"done\"}]}}\n")
	got := entriesFrom(ToolClaude, claude)
	if len(got) != 3 || got[0].Role != "thinking" || got[1].Role != "tool" || got[2].Role != "assistant" {
		t.Fatalf("order = %+v", got)
	}
	if got[0].Text != "checked the path" || got[1].Text != "read" || got[2].Text != "done" {
		t.Fatalf("text = %+v", got)
	}
	blank := entriesFrom(ToolClaude, []byte("{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"thinking\",\"thinking\":\"\"}]}}\n"))
	if len(blank) != 0 {
		t.Fatalf("empty thought = %+v", blank)
	}
	codex := []byte("{\"payload\":{\"type\":\"reasoning\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"checked the path\"}]}}\n" +
		"{\"payload\":{\"type\":\"custom_tool_call\",\"name\":\"read\"}}\n")
	got = entriesFrom(ToolCodex, codex)
	if len(got) != 2 || got[0].Role != "thinking" || got[1].Role != "tool" || got[1].Text != "read" {
		t.Fatalf("codex = %+v", got)
	}
}
