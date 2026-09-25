package clients

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

const (
	entryCap   = 60
	entryRunes = 400
	readBytes  = 256 << 10
)

// Entry is one visible line of a foreign session. Role is user, assistant,
// or tool. This is a reading, not a turn the engine can steer.
type Entry struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// Transcript is the read-only body of one local agent task.
type Transcript struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
	Entries   []Entry   `json:"entries"`
	Truncated bool      `json:"truncated,omitempty"`
}

// Read loads one session by the id List assigned. The switch must be on.
// A missing id or a path outside the configured root is not found.
func Read(cfg config.ClientsConfig, id string, now time.Time) (Transcript, bool) {
	if !cfg.Enabled {
		return Transcript{}, false
	}
	tool, rest, ok := splitTaskID(id)
	if !ok {
		return Transcript{}, false
	}
	path, ok := findSession(cfg, tool, rest)
	if !ok {
		return Transcript{}, false
	}
	h, ok := readHit(path)
	if !ok {
		return Transcript{}, false
	}
	body, clipped := readSession(path)
	entries := entriesFrom(tool, body)
	truncated := clipped
	if entries == nil {
		entries = []Entry{}
	}
	if len(entries) > entryCap {
		entries = entries[len(entries)-entryCap:]
		truncated = true
	}
	title := titleOf(tool, h)
	if title == "" {
		title = rest
	}
	return Transcript{
		ID:        id,
		Title:     title,
		Status:    statusOf(tool, h, now, cfg.RunningStale()),
		UpdatedAt: h.mtime,
		Entries:   entries,
		Truncated: truncated,
	}, true
}

func splitTaskID(id string) (tool, rest string, ok bool) {
	tool, rest, ok = strings.Cut(id, ":")
	if !ok || strings.TrimSpace(rest) == "" {
		return "", "", false
	}
	if strings.Contains(rest, "/") || strings.Contains(rest, `\`) || rest == "." || rest == ".." {
		return "", "", false
	}
	switch tool {
	case ToolClaude, ToolCodex, ToolCursor:
		return tool, rest, true
	default:
		return "", "", false
	}
}

var (
	pathMu sync.Mutex
	paths  = map[string]string{}
)

// rememberSession keeps the file List already opened, so a later read does
// not walk every transcript again.
func rememberSession(id, path string) {
	if id == "" || path == "" {
		return
	}
	pathMu.Lock()
	paths[id] = path
	pathMu.Unlock()
}

func cachedSession(id string) (string, bool) {
	pathMu.Lock()
	path, ok := paths[id]
	pathMu.Unlock()
	if !ok {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}

func findSession(cfg config.ClientsConfig, tool, rest string) (string, bool) {
	if path, ok := cachedSession(tool + ":" + rest); ok {
		return path, true
	}
	switch tool {
	case ToolClaude:
		return walkFile(filepath.Join(cfg.ClaudeDir, "projects"), rest+".jsonl")
	case ToolCursor:
		return walkFile(filepath.Join(cfg.CursorDir, "projects"), filepath.Join(rest, rest+".jsonl"))
	case ToolCodex:
		return walkCodex(filepath.Join(cfg.CodexDir, "sessions"), rest)
	default:
		return "", false
	}
}

func walkFile(root, suffix string) (string, bool) {
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return nil
		}
		if strings.HasSuffix(filepath.ToSlash(path), filepath.ToSlash(suffix)) {
			found = path
		}
		return nil
	})
	return found, found != ""
}

func walkCodex(root, id string) (string, bool) {
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found != "" {
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		if strings.Contains(name, id) {
			found = path
			return nil
		}
		h, ok := readHit(path)
		if !ok {
			return nil
		}
		_, _, sid := codexMeta(h.head)
		if sid == id {
			found = path
		}
		return nil
	})
	return found, found != ""
}

func readSession(path string) ([]byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		return nil, false
	}
	if st.Size() <= readBytes*2 {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, false
		}
		return b, false
	}
	head := make([]byte, readBytes)
	if _, err := f.Read(head); err != nil {
		return nil, false
	}
	if _, err := f.Seek(-readBytes, io.SeekEnd); err != nil {
		return nil, false
	}
	tail := make([]byte, readBytes)
	if _, err := f.Read(tail); err != nil {
		return nil, false
	}
	return append(append(dropPartial(head, true), '\n'), dropPartial(tail, false)...), true
}

func titleOf(tool string, h hit) string {
	switch tool {
	case ToolClaude:
		title, _ := claudeTitle(h.head)
		return title
	case ToolCodex:
		title, _, _ := codexMeta(h.head)
		return title
	default:
		return cursorTitle(h.head)
	}
}

func statusOf(tool string, h hit, now time.Time, stale time.Duration) string {
	switch tool {
	case ToolCodex:
		return codexStatus(h.tail, h.mtime, now, stale)
	case ToolCursor:
		return cursorStatus(h.tail, h.mtime, now, stale)
	default:
		return freshStatus(h.mtime, now, stale, claudeOpen(h.tail))
	}
}

func entriesFrom(tool string, body []byte) []Entry {
	var out []Entry
	for _, raw := range linesOf(body) {
		switch tool {
		case ToolCodex:
			out = append(out, codexEntries(raw)...)
		case ToolCursor:
			out = append(out, cursorEntries(raw)...)
		default:
			out = append(out, claudeEntries(raw)...)
		}
	}
	return out
}

func claudeEntries(raw []byte) []Entry {
	var row struct {
		Type          string `json:"type"`
		IsMeta        bool   `json:"isMeta"`
		TurnCompanion bool   `json:"turnCompanion"`
		Message       struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(raw, &row) != nil || row.IsMeta || row.TurnCompanion {
		return nil
	}
	if row.Type != "user" && row.Type != "assistant" {
		return nil
	}
	return textsAndTools(row.Type, row.Message.Content)
}

func cursorEntries(raw []byte) []Entry {
	var row struct {
		Role    string `json:"role"`
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(raw, &row) != nil {
		return nil
	}
	if row.Role != "user" && row.Role != "assistant" {
		return nil
	}
	return textsAndTools(row.Role, row.Message.Content)
}

func codexEntries(raw []byte) []Entry {
	var row struct {
		Payload struct {
			Type    string          `json:"type"`
			Role    string          `json:"role"`
			Name    string          `json:"name"`
			Content json.RawMessage `json:"content"`
		} `json:"payload"`
	}
	if json.Unmarshal(raw, &row) != nil {
		return nil
	}
	if row.Payload.Type == "function_call" && strings.TrimSpace(row.Payload.Name) != "" {
		return []Entry{{Role: "tool", Text: clipEntry(row.Payload.Name)}}
	}
	role := row.Payload.Role
	if role != "user" && role != "assistant" {
		return nil
	}
	return textsAndTools(role, row.Payload.Content)
}

func textsAndTools(role string, content json.RawMessage) []Entry {
	var out []Entry
	if text := textOf(content); text != "" {
		out = append(out, Entry{Role: role, Text: clipEntry(text)})
	}
	for _, name := range toolNames(content) {
		out = append(out, Entry{Role: "tool", Text: clipEntry(name)})
	}
	return out
}

func toolNames(raw json.RawMessage) []string {
	var blocks []struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return nil
	}
	var names []string
	for _, b := range blocks {
		if (b.Type == "tool_use" || b.Type == "tool_call") && strings.TrimSpace(b.Name) != "" {
			names = append(names, b.Name)
		}
	}
	return names
}

func clipEntry(s string) string {
	if utf8.RuneCountInString(s) <= entryRunes {
		return s
	}
	r := []rune(s)
	return string(r[:entryRunes-1]) + "…"
}
