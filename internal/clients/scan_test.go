package clients

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func TestLongTranscriptKeepsTheOpeningTitle(t *testing.T) {
	root := t.TempDir()
	var body strings.Builder
	body.WriteString("{\"type\":\"user\",\"cwd\":\"/work/demo\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"opening title\"}]}}\n")
	pad := strings.Repeat("x", 200)
	for i := 0; i < 800; i++ {
		body.WriteString("{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"" + pad + "\"}]}}\n")
	}
	path := filepath.Join(root, "projects", "work", "long.jsonl")
	writeFileTime(t, path, body.String(), time.Now())
	cfg := config.ClientsConfig{
		Enabled: true, ClaudeDir: root,
		CodexDir: filepath.Join(root, "no-codex"), CursorDir: filepath.Join(root, "no-cursor"),
		RecentDays: 3, RunningStaleSeconds: 90,
	}
	cat := List(cfg, time.Now(), 0)
	if len(cat.Tools[0].Tasks) != 1 || cat.Tools[0].Tasks[0].Title != "opening title" {
		t.Fatalf("long file title = %+v", cat.Tools[0].Tasks)
	}
}

func TestListHidesSessionsUntilTheSwitchIsOn(t *testing.T) {
	root := t.TempDir()
	writeClaude(t, root, "s1", "alpha task", time.Now())
	cfg := config.ClientsConfig{Enabled: false, ClaudeDir: root, RecentDays: 3, RunningStaleSeconds: 90}
	cat := List(cfg, time.Now(), 0)
	if cat.Enabled || len(cat.Tools) != 0 {
		t.Fatalf("off switch still listed: %+v", cat)
	}
}

func TestRecentWindowKeepsThreeDaysAndMoreOpensOlder(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-2 * time.Hour)
	old := now.Add(-5 * 24 * time.Hour)
	writeClaude(t, root, "new", "fresh task", fresh)
	writeClaude(t, root, "old", "stale task", old)
	cfg := config.ClientsConfig{
		Enabled: true, ClaudeDir: root, CodexDir: filepath.Join(root, "missing-codex"),
		CursorDir:  filepath.Join(root, "missing-cursor"),
		RecentDays: 3, RunningStaleSeconds: 90,
	}
	cat := List(cfg, now, 0)
	if !cat.Enabled || len(cat.Tools) != 3 {
		t.Fatalf("tools = %+v", cat.Tools)
	}
	claude := cat.Tools[0]
	if claude.ID != ToolClaude || len(claude.Tasks) != 1 || claude.Tasks[0].Title != "fresh task" {
		t.Fatalf("recent = %+v", claude)
	}
	if !claude.More || claude.Next == "" {
		t.Fatal("older task did not offer more")
	}
	before, _ := strconv.ParseInt(claude.Next, 10, 64)
	older := List(cfg, now, before)
	if len(older.Tools[0].Tasks) != 1 || older.Tools[0].Tasks[0].Title != "stale task" {
		t.Fatalf("older page = %+v", older.Tools[0])
	}
	if older.Tools[0].Tasks[0].Status != StatusDone {
		t.Fatalf("quiet old session status = %s", older.Tools[0].Tasks[0].Status)
	}
}

func TestRecentPageShowsFiveThenMore(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 6; i++ {
		writeClaude(t, root, "r"+strconv.Itoa(i), "row "+strconv.Itoa(i), now.Add(-time.Duration(i)*time.Hour))
	}
	cfg := config.ClientsConfig{
		Enabled: true, ClaudeDir: root,
		CodexDir: filepath.Join(root, "missing-codex"), CursorDir: filepath.Join(root, "missing-cursor"),
		RecentDays: 3, RunningStaleSeconds: 90,
	}
	cat := List(cfg, now, 0)
	claude := cat.Tools[0]
	if len(claude.Tasks) != PageSize || !claude.More {
		t.Fatalf("first page = %+v", claude)
	}
	if claude.Tasks[0].Title != "row 0" {
		t.Fatalf("newest = %s", claude.Tasks[0].Title)
	}
	before, _ := strconv.ParseInt(claude.Next, 10, 64)
	next := List(cfg, now, before)
	if len(next.Tools[0].Tasks) != 1 || next.Tools[0].Tasks[0].Title != "row 5" || next.Tools[0].More {
		t.Fatalf("second page = %+v", next.Tools[0])
	}
}

func TestBreathingFollowsEachToolsFinishMarker(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	writeCodex(t, root, "c1", "codex open", now.Add(-time.Hour), "task_started")
	writeCodex(t, root, "c2", "codex shut", now, "task_complete")
	writeCursor(t, root, "u1", "cursor live", now, "")
	writeCursor(t, root, "u2", "cursor shut", now, "turn_ended")
	cfg := config.ClientsConfig{
		Enabled:   true,
		ClaudeDir: filepath.Join(root, "no-claude"),
		CodexDir:  root, CursorDir: root,
		RecentDays: 3, RunningStaleSeconds: 90,
	}
	cat := List(cfg, now, 0)
	got := map[string]string{}
	for _, g := range cat.Tools {
		for _, task := range g.Tasks {
			got[task.Title] = task.Status
		}
	}
	if got["codex open"] != StatusRunning || got["codex shut"] != StatusDone {
		t.Fatalf("codex = %v", got)
	}
	if got["cursor live"] != StatusRunning || got["cursor shut"] != StatusDone {
		t.Fatalf("cursor = %v", got)
	}
	// A phrase that was only an illustration must not be invented as a title.
	if _, ok := got["read notes.md"]; ok {
		t.Fatal("sample prompt became a task title")
	}
}

func TestTitleSkipsToolWrappers(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	claude := `{"type":"user","cwd":"/work/demo","message":{"content":"<command-message>tool</command-message> <command-name>/tool</command-name>"}}` + "\n" +
		`{"type":"user","isMeta":true,"turnCompanion":true,"message":{"content":[{"type":"text","text":"Base directory for this skill: injected context that is not the request"}]}}` + "\n" +
		`{"type":"user","message":{"content":[{"type":"text","text":"open session"}]}}` + "\n"
	writeFileTime(t, filepath.Join(root, "claude", "projects", "work", "s1.jsonl"), claude, now)
	cursor := `{"role":"user","message":{"content":[{"type":"text","text":"<timestamp>stamp</timestamp> <user_query>open session</user_query>"}]}}` + "\n"
	writeFileTime(t, filepath.Join(root, "cursor", "projects", "p", "agent-transcripts", "u1", "u1.jsonl"), cursor, now)
	cfg := config.ClientsConfig{
		Enabled:    true,
		ClaudeDir:  filepath.Join(root, "claude"),
		CodexDir:   filepath.Join(root, "no-codex"),
		CursorDir:  filepath.Join(root, "cursor"),
		RecentDays: 3, RunningStaleSeconds: 90,
	}
	cat := List(cfg, now, 0)
	var titles []string
	for _, g := range cat.Tools {
		for _, task := range g.Tasks {
			titles = append(titles, task.Title)
		}
	}
	if len(titles) != 2 || titles[0] != "/tool" || titles[1] != "open session" {
		t.Fatalf("titles = %v", titles)
	}
}

func writeClaude(t *testing.T, root, id, title string, mtime time.Time) {
	t.Helper()
	dir := filepath.Join(root, "projects", "work", id+".jsonl")
	body := `{"type":"user","cwd":"/work/demo","message":{"content":[{"type":"text","text":"` + title + `"}]}}` + "\n"
	writeFileTime(t, dir, body, mtime)
}

func writeCodex(t *testing.T, root, id, title string, mtime time.Time, marker string) {
	t.Helper()
	dir := filepath.Join(root, "sessions", "2026", "09", "25", "rollout-"+id+".jsonl")
	body := `{"type":"session_meta","payload":{"cwd":"/work/demo","session_id":"` + id + `"}}` + "\n" +
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"` + title + `"}]}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"` + marker + `"}}` + "\n"
	writeFileTime(t, dir, body, mtime)
}

func writeCursor(t *testing.T, root, id, title string, mtime time.Time, end string) {
	t.Helper()
	dir := filepath.Join(root, "projects", "demo", "agent-transcripts", id, id+".jsonl")
	body := `{"role":"user","message":{"content":[{"type":"text","text":"` + title + `"}]}}` + "\n" +
		`{"role":"assistant","message":{"content":[{"type":"text","text":"ack"}]}}` + "\n"
	if end != "" {
		body += `{"role":"` + end + `"}` + "\n"
	}
	writeFileTime(t, dir, body, mtime)
}

func writeFileTime(t *testing.T, path, body string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}
