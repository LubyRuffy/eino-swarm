package clients

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
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

func TestLongSessionShowsTheLiveTailThenTheEarlierPage(t *testing.T) {
	// The old cut ended on a notice and threw away the lines above the
	// tail. The latest reply has to stay on the first page, and Earlier
	// has to bring back what was above it without repeating that reply.
	root := t.TempDir()
	var body strings.Builder
	body.WriteString(claudeUserLine("the request"))
	body.WriteString(claudeAssistantLine("early-0"))
	body.WriteString(claudeBundleLine())
	body.WriteString(claudeAssistantLine("latest reply"))
	writeFileTime(t, filepath.Join(root, "projects", "work", "paged.jsonl"), body.String(), time.Now())
	cfg := claudeCfg(root)
	tail, ok := ReadPage(cfg, "claude:paged", time.Now(), 0, 2)
	if !ok || !tail.Older || tail.Before <= 0 {
		t.Fatalf("tail = %+v ok=%v", tail, ok)
	}
	if tail.Entries[0].Text != "the request" || tail.Entries[len(tail.Entries)-1].Text != "latest reply" {
		t.Fatalf("live edge = %+v", tail.Entries)
	}
	for _, e := range tail.Entries {
		if e.Text == "early-0" || e.Text == "bundle text" {
			t.Fatalf("tail swallowed an earlier line: %+v", tail.Entries)
		}
	}
	older, ok := ReadPage(cfg, "claude:paged", time.Now(), tail.Before, 2)
	if !ok || older.Entries[0].Text != "the request" {
		t.Fatalf("earlier page = %+v ok=%v", older, ok)
	}
	seen := map[string]bool{}
	for _, e := range older.Entries {
		if e.Text == "latest reply" {
			t.Fatal("earlier page repeated the live reply")
		}
		seen[e.Text] = true
	}
	if !seen["bundle text"] || !seen["checked the path"] || !seen["read"] {
		t.Fatalf("a line was split across pages: %+v", older.Entries)
	}
	for _, e := range tail.Entries[1:] {
		if seen[e.Text] {
			t.Fatalf("page overlap on %q", e.Text)
		}
	}
	if !older.Older || older.Before <= 0 {
		t.Fatalf("bundle page closed the session early: %v", texts(older.Entries))
	}
	first, ok := ReadPage(cfg, "claude:paged", time.Now(), older.Before, 2)
	if !ok || !hasText(first.Entries, "early-0") || first.Older {
		t.Fatalf("first page = %v older=%v ok=%v", texts(first.Entries), first.Older, ok)
	}
}

func TestALargeSessionTailIsTheRealEnd(t *testing.T) {
	// Bigger than the old head-plus-tail slice. The latest line sits past
	// that hole and still has to be the live edge.
	root := t.TempDir()
	var body strings.Builder
	body.WriteString(claudeUserLine("the request"))
	for i := 0; i < 20; i++ {
		body.WriteString(claudeAssistantLine("early-" + strconv.Itoa(i)))
	}
	body.WriteString("{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"" + strings.Repeat("m", 600<<10) + "\"}]}}\n")
	body.WriteString(claudeAssistantLine("latest reply"))
	writeFileTime(t, filepath.Join(root, "projects", "work", "wide.jsonl"), body.String(), time.Now())
	tail, ok := ReadPage(claudeCfg(root), "claude:wide", time.Now(), 0, 6)
	if !ok || tail.Entries[0].Text != "the request" || tail.Entries[len(tail.Entries)-1].Text != "latest reply" {
		t.Fatalf("wide tail = %+v ok=%v", texts(tail.Entries), ok)
	}
	if !tail.Older {
		t.Fatal("wide file hid the lines above the tail")
	}
	for _, e := range tail.Entries {
		if e.Text == "early-0" {
			t.Fatal("tail included the start of the file")
		}
	}
	page, ok := ReadPage(claudeCfg(root), "claude:wide", time.Now(), tail.Before, 30)
	if !ok {
		t.Fatal("earlier page missing")
	}
	found := false
	for _, e := range page.Entries {
		if e.Text == "early-0" {
			found = true
		}
		if e.Text == "latest reply" {
			t.Fatal("earlier page included the live reply")
		}
	}
	if !found {
		t.Fatalf("early-0 not on the earlier page: %v", texts(page.Entries))
	}
}

func TestCursorAndCodexSessionsPageTheSameWay(t *testing.T) {
	root := t.TempDir()
	cursorPath := filepath.Join(root, "cursor", "projects", "demo", "agent-transcripts", "u1", "u1.jsonl")
	writeFileTime(t, cursorPath, cursorUserLine("cursor request")+cursorAssistantLine("cursor reply"), time.Now())
	codexPath := filepath.Join(root, "codex", "sessions", "rollout-abc.jsonl")
	writeFileTime(t, codexPath, codexUserLine("codex request")+codexAssistantLine("codex reply"), time.Now())
	cfg := config.ClientsConfig{
		Enabled: true, ClaudeDir: filepath.Join(root, "no-claude"),
		CodexDir: filepath.Join(root, "codex"), CursorDir: filepath.Join(root, "cursor"),
		RecentDays: 3, RunningStaleSeconds: 90,
	}
	cursor, ok := Read(cfg, "cursor:u1", time.Now())
	if !ok || !hasText(cursor.Entries, "cursor request") || !hasText(cursor.Entries, "cursor reply") || cursor.Older {
		t.Fatalf("cursor = %+v ok=%v", cursor.Entries, ok)
	}
	codex, ok := Read(cfg, "codex:abc", time.Now())
	if !ok || !hasText(codex.Entries, "codex request") || !hasText(codex.Entries, "codex reply") || codex.Older {
		t.Fatalf("codex = %+v ok=%v", codex.Entries, ok)
	}
}

func claudeCfg(root string) config.ClientsConfig {
	return config.ClientsConfig{
		Enabled: true, ClaudeDir: root,
		CodexDir: filepath.Join(root, "no-codex"), CursorDir: filepath.Join(root, "no-cursor"),
		RecentDays: 3, RunningStaleSeconds: 90,
	}
}

func claudeUserLine(text string) string {
	return "{\"type\":\"user\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"" + text + "\"}]}}\n"
}

func claudeAssistantLine(text string) string {
	return "{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"" + text + "\"}]}}\n"
}

func claudeBundleLine() string {
	return "{\"type\":\"assistant\",\"message\":{\"content\":[" +
		"{\"type\":\"thinking\",\"thinking\":\"checked the path\"}," +
		"{\"type\":\"tool_use\",\"name\":\"read\"}," +
		"{\"type\":\"text\",\"text\":\"bundle text\"}]}}\n"
}

func cursorUserLine(text string) string {
	return "{\"role\":\"user\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"" + text + "\"}]}}\n"
}

func cursorAssistantLine(text string) string {
	return "{\"role\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"" + text + "\"}]}}\n"
}

func codexUserLine(text string) string {
	return "{\"payload\":{\"type\":\"message\",\"role\":\"user\",\"content\":[{\"type\":\"text\",\"text\":\"" + text + "\"}]}}\n"
}

func codexAssistantLine(text string) string {
	return "{\"payload\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"" + text + "\"}]}}\n"
}

func texts(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Role + ":" + e.Text
	}
	return out
}

func TestALongRequestDoesNotHideTheLiveEdge(t *testing.T) {
	root := t.TempDir()
	var body strings.Builder
	body.WriteString(claudeUserLine("first request"))
	body.WriteString(claudeUserLine("second request"))
	body.WriteString(claudeAssistantLine("latest reply"))
	writeFileTime(t, filepath.Join(root, "projects", "work", "req.jsonl"), body.String(), time.Now())
	cfg := claudeCfg(root)
	tail, ok := ReadPage(cfg, "claude:req", time.Now(), 0, 2)
	if !ok || tail.Entries[0].Text != "first request" || tail.Entries[len(tail.Entries)-1].Text != "latest reply" || !tail.Older {
		t.Fatalf("tail = %v ok=%v", texts(tail.Entries), ok)
	}
	if hasText(tail.Entries, "second request") {
		t.Fatal("the second request sat on the live edge")
	}
	older, ok := ReadPage(cfg, "claude:req", time.Now(), tail.Before, 2)
	if !ok || !hasText(older.Entries, "second request") || hasText(older.Entries, "latest reply") {
		t.Fatalf("earlier request = %v ok=%v", texts(older.Entries), ok)
	}
	inside, ok := ReadPage(cfg, "claude:req", time.Now(), 1, 2)
	if !ok || len(inside.Entries) != 1 || inside.Entries[0].Text != "first request" || inside.Older {
		t.Fatalf("cursor inside the request = %v older=%v ok=%v", texts(inside.Entries), inside.Older, ok)
	}
	forced, ok := ReadPage(cfg, "claude:req", time.Now(), -1, 0)
	if !ok || forced.Entries[len(forced.Entries)-1].Text != "latest reply" {
		t.Fatalf("negative cursor = %v ok=%v", texts(forced.Entries), ok)
	}
}

func TestEmptyFileAndAReplyWithNoRequest(t *testing.T) {
	root := t.TempDir()
	writeFileTime(t, filepath.Join(root, "projects", "work", "empty.jsonl"), "", time.Now())
	writeFileTime(t, filepath.Join(root, "projects", "work", "reply.jsonl"), claudeAssistantLine("only reply"), time.Now())
	cfg := claudeCfg(root)
	empty, ok := Read(cfg, "claude:empty", time.Now())
	if !ok || len(empty.Entries) != 0 || empty.Older {
		t.Fatalf("empty = %+v ok=%v", empty, ok)
	}
	reply, ok := Read(cfg, "claude:reply", time.Now())
	if !ok || len(reply.Entries) != 1 || reply.Entries[0].Text != "only reply" || reply.Older {
		t.Fatalf("reply = %+v ok=%v", reply, ok)
	}
}

func TestOpeningScanGrowsPastAPartialFirstLine(t *testing.T) {
	root := t.TempDir()
	var body strings.Builder
	body.WriteString(claudeUserLine(strings.Repeat("q", int(readBytes)+32)))
	body.WriteString(claudeAssistantLine("after the long request"))
	writeFileTime(t, filepath.Join(root, "projects", "work", "longreq.jsonl"), body.String(), time.Now())
	got, ok := Read(claudeCfg(root), "claude:longreq", time.Now())
	if !ok || len(got.Entries) != 2 || got.Entries[1].Text != "after the long request" {
		t.Fatalf("long request = %v ok=%v", texts(got.Entries), ok)
	}
	if !strings.HasSuffix(got.Entries[0].Text, "…") {
		t.Fatal("the long request was not clipped")
	}
}

func TestPartialWindowAndOversizeLineAreSkipped(t *testing.T) {
	var saw int
	walkLines([]byte("no newline"), 0, true, false, func(int64, []byte) { saw++ })
	if saw != 0 {
		t.Fatal("a partial window was parsed")
	}
	huge := append([]byte{'{'}, bytes.Repeat([]byte{'x'}, 2<<20)...)
	if visibleEntries(ToolClaude, huge) != nil {
		t.Fatal("an oversize line was parsed")
	}
	f, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if readAt(f, 0, 4) != nil {
		t.Fatal("a closed file returned bytes")
	}
	if readAt(f, 0, 0) != nil {
		t.Fatal("a zero read returned bytes")
	}
	if keepTail(nil, 1) != nil {
		t.Fatal("an empty window produced a page")
	}
	if _, covered := scanBody(f, ToolClaude, 10, 4, 4, 1); !covered {
		t.Fatal("a cursor already at the request was treated as a page")
	}
	skipped := parseMarked(ToolClaude, []byte(claudeAssistantLine("too early")+claudeAssistantLine("kept")), 0, false, true, 8, 8)
	if len(skipped) != 0 {
		t.Fatalf("lines outside the window were kept: %+v", skipped)
	}
}

func TestLimitOneStillKeepsTheRequestAndTheLiveEdge(t *testing.T) {
	root := t.TempDir()
	writeFileTime(t, filepath.Join(root, "projects", "work", "one.jsonl"), claudeUserLine("the request")+claudeAssistantLine("latest reply"), time.Now())
	got, ok := ReadPage(claudeCfg(root), "claude:one", time.Now(), 0, 1)
	if !ok || len(got.Entries) != 2 || got.Entries[0].Text != "the request" || got.Entries[1].Text != "latest reply" {
		t.Fatalf("limit 1 = %v ok=%v", texts(got.Entries), ok)
	}
	bare := strings.TrimSuffix(claudeAssistantLine("no newline"), "\n")
	writeFileTime(t, filepath.Join(root, "projects", "work", "bare.jsonl"), bare, time.Now())
	line, ok := Read(claudeCfg(root), "claude:bare", time.Now())
	if !ok || len(line.Entries) != 1 || line.Entries[0].Text != "no newline" {
		t.Fatalf("bare line = %v ok=%v", texts(line.Entries), ok)
	}
	if _, ok := Read(claudeCfg(root), "claude:missing", time.Now()); ok {
		t.Fatal("a missing session was readable")
	}
	locked := filepath.Join(root, "projects", "work", "locked.jsonl")
	writeFileTime(t, locked, claudeUserLine("secret"), time.Now())
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
	if _, ok := Read(claudeCfg(root), "claude:locked", time.Now()); ok {
		t.Fatal("an unreadable session was opened")
	}
}

func hasText(entries []Entry, text string) bool {
	for _, e := range entries {
		if e.Text == text {
			return true
		}
	}
	return false
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
