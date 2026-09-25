// Package clients reads local agent transcripts for a progress list.
// It never writes those files and never starts or resumes a process.
package clients

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

const (
	ToolClaude = "claude"
	ToolCodex  = "codex"
	ToolCursor = "cursor"

	StatusRunning = "running"
	StatusDone    = "done"

	// PageSize is how many tasks one page shows per tool. The first page
	// is still only the recent window; More walks the rest five at a time.
	PageSize   = 5
	titleRunes = 80
	edgeBytes  = 64 << 10
)

// Task is one foreign session row. Status is running or done.
type Task struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CWD       string    `json:"cwd,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	Status    string    `json:"status"`
}

// Group is one tool's page of tasks.
type Group struct {
	ID    string `json:"id"`
	Tasks []Task `json:"tasks"`
	More  bool   `json:"more"`
	Next  string `json:"next,omitempty"`
}

// Catalog is the sidebar payload. Tools is empty when the switch is off.
type Catalog struct {
	Enabled bool    `json:"enabled"`
	Tools   []Group `json:"tools"`
}

// List reads the three configured roots. before is the exclusive upper
// bound of an older page (unix milliseconds). Zero before is the recent window.
func List(cfg config.ClientsConfig, now time.Time, beforeMs int64) Catalog {
	if !cfg.Enabled {
		return Catalog{Tools: []Group{}}
	}
	stale := cfg.RunningStale()
	windowStart := now.Add(-cfg.RecentWindow())
	var before time.Time
	if beforeMs > 0 {
		before = time.UnixMilli(beforeMs)
	}
	tools := []Group{
		page(ToolClaude, scanClaude(cfg.ClaudeDir, now, stale), windowStart, before),
		page(ToolCodex, scanCodex(cfg.CodexDir, now, stale), windowStart, before),
		page(ToolCursor, scanCursor(cfg.CursorDir, now, stale), windowStart, before),
	}
	return Catalog{Enabled: true, Tools: tools}
}

func page(id string, tasks []Task, windowStart, before time.Time) Group {
	sortTasks(tasks)
	g := Group{ID: id, Tasks: []Task{}}
	var pool []Task
	var beyond bool
	if before.IsZero() {
		for _, task := range tasks {
			if task.UpdatedAt.Before(windowStart) {
				beyond = true
				continue
			}
			pool = append(pool, task)
		}
	} else {
		for _, task := range tasks {
			if task.UpdatedAt.Before(before) {
				pool = append(pool, task)
			}
		}
	}
	shown, more, next := takePage(pool)
	g.Tasks = shown
	g.More = more
	g.Next = next
	if !g.More && beyond {
		g.More = true
		g.Next = unixMilli(windowStart)
	}
	return g
}

func takePage(tasks []Task) (shown []Task, more bool, next string) {
	if len(tasks) == 0 {
		return []Task{}, false, ""
	}
	if len(tasks) <= PageSize {
		return tasks, false, ""
	}
	shown = tasks[:PageSize]
	return shown, true, unixMilli(shown[len(shown)-1].UpdatedAt)
}

func unixMilli(t time.Time) string {
	return strconv.FormatInt(t.UnixMilli(), 10)
}

func sortTasks(tasks []Task) {
	for i := 1; i < len(tasks); i++ {
		j := i
		for j > 0 && tasks[j].UpdatedAt.After(tasks[j-1].UpdatedAt) {
			tasks[j], tasks[j-1] = tasks[j-1], tasks[j]
			j--
		}
	}
}

type hit struct {
	path  string
	mtime time.Time
	head  []byte
	tail  []byte
}

func scanClaude(root string, now time.Time, stale time.Duration) []Task {
	var out []Task
	projects := filepath.Join(root, "projects")
	entries, err := os.ReadDir(projects)
	if err != nil {
		return out
	}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		dir := filepath.Join(projects, ent.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			h, ok := readHit(filepath.Join(dir, f.Name()))
			if !ok {
				continue
			}
			title, cwd := claudeTitle(h.head)
			if title == "" {
				title = strings.TrimSuffix(f.Name(), ".jsonl")
			}
			out = append(out, Task{
				ID:        ToolClaude + ":" + strings.TrimSuffix(f.Name(), ".jsonl"),
				Title:     title,
				CWD:       cwd,
				UpdatedAt: h.mtime,
				Status:    freshStatus(h.mtime, now, stale, claudeOpen(h.tail)),
			})
		}
	}
	return out
}

func scanCodex(root string, now time.Time, stale time.Duration) []Task {
	var out []Task
	sessions := filepath.Join(root, "sessions")
	_ = filepath.WalkDir(sessions, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
			return nil
		}
		h, ok := readHit(path)
		if !ok {
			return nil
		}
		title, cwd, id := codexMeta(h.head)
		if id == "" {
			id = strings.TrimSuffix(name, ".jsonl")
		}
		if title == "" {
			title = id
		}
		out = append(out, Task{
			ID:        ToolCodex + ":" + id,
			Title:     title,
			CWD:       cwd,
			UpdatedAt: h.mtime,
			Status:    codexStatus(h.tail, h.mtime, now, stale),
		})
		return nil
	})
	return out
}

func scanCursor(root string, now time.Time, stale time.Duration) []Task {
	var out []Task
	projects := filepath.Join(root, "projects")
	entries, err := os.ReadDir(projects)
	if err != nil {
		return out
	}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		transcripts := filepath.Join(projects, ent.Name(), "agent-transcripts")
		ids, err := os.ReadDir(transcripts)
		if err != nil {
			continue
		}
		for _, id := range ids {
			if !id.IsDir() {
				continue
			}
			path := filepath.Join(transcripts, id.Name(), id.Name()+".jsonl")
			h, ok := readHit(path)
			if !ok {
				continue
			}
			title := cursorTitle(h.head)
			if title == "" {
				title = id.Name()
			}
			out = append(out, Task{
				ID:        ToolCursor + ":" + id.Name(),
				Title:     title,
				UpdatedAt: h.mtime,
				Status:    cursorStatus(h.tail, h.mtime, now, stale),
			})
		}
	}
	return out
}

func freshStatus(mtime, now time.Time, stale time.Duration, open bool) string {
	if open && now.Sub(mtime) <= stale {
		return StatusRunning
	}
	return StatusDone
}

func claudeOpen(tail []byte) bool {
	saw := false
	for _, raw := range linesOf(tail) {
		var row struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		if row.Type == "user" || row.Type == "assistant" {
			saw = true
		}
	}
	return saw
}

func codexStatus(tail []byte, mtime, now time.Time, stale time.Duration) string {
	state := ""
	for _, raw := range linesOf(tail) {
		var row struct {
			Type    string `json:"type"`
			Payload struct {
				Type string `json:"type"`
			} `json:"payload"`
		}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		switch {
		case row.Type == "event_msg" && row.Payload.Type == "task_complete":
			state = StatusDone
		case row.Type == "event_msg" && row.Payload.Type == "task_started":
			state = StatusRunning
		case row.Type == "event_msg" && (row.Payload.Type == "token_count" || row.Payload.Type == "item_completed"):
			if state != StatusDone {
				state = StatusRunning
			}
		}
	}
	if state == StatusRunning {
		return StatusRunning
	}
	if state == StatusDone {
		return StatusDone
	}
	return freshStatus(mtime, now, stale, true)
}

func cursorStatus(tail []byte, mtime, now time.Time, stale time.Duration) string {
	last := ""
	for _, raw := range linesOf(tail) {
		var row struct {
			Role string `json:"role"`
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		kind := row.Role
		if kind == "" {
			kind = row.Type
		}
		switch kind {
		case "turn_ended":
			last = StatusDone
		case "user", "assistant":
			last = StatusRunning
		}
	}
	if last == StatusDone {
		return StatusDone
	}
	if last == StatusRunning {
		return freshStatus(mtime, now, stale, true)
	}
	return StatusDone
}

func claudeTitle(head []byte) (string, string) {
	var title, cwd string
	for _, raw := range linesOf(head) {
		var row struct {
			Type    string `json:"type"`
			CWD     string `json:"cwd"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		if cwd == "" && strings.TrimSpace(row.CWD) != "" {
			cwd = row.CWD
		}
		if title == "" && (row.Type == "user" || row.Type == "assistant") {
			title = textOf(row.Message.Content)
		}
		if title != "" && cwd != "" {
			break
		}
	}
	if title == "" {
		title = baseName(cwd)
	}
	return clip(title), cwd
}

func codexMeta(head []byte) (title, cwd, id string) {
	for _, raw := range linesOf(head) {
		var row struct {
			Type    string `json:"type"`
			Payload struct {
				CWD       string          `json:"cwd"`
				SessionID string          `json:"session_id"`
				Type      string          `json:"type"`
				Role      string          `json:"role"`
				Content   json.RawMessage `json:"content"`
			} `json:"payload"`
		}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		if row.Type == "session_meta" {
			if cwd == "" {
				cwd = row.Payload.CWD
			}
			if id == "" {
				id = row.Payload.SessionID
			}
		}
		if title == "" && row.Payload.Role == "user" {
			title = textOf(row.Payload.Content)
		}
		if title != "" && id != "" {
			break
		}
	}
	if title == "" {
		title = baseName(cwd)
	}
	return clip(title), cwd, id
}

func cursorTitle(head []byte) string {
	for _, raw := range linesOf(head) {
		var row struct {
			Role    string `json:"role"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(raw, &row) != nil {
			continue
		}
		if row.Role == "user" {
			if text := textOf(row.Message.Content); text != "" {
				return clip(text)
			}
		}
	}
	return ""
}

func textOf(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return strings.TrimSpace(s)
		}
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	for _, b := range blocks {
		if strings.TrimSpace(b.Text) != "" && (b.Type == "" || b.Type == "text" || b.Type == "input_text") {
			return strings.TrimSpace(b.Text)
		}
	}
	return ""
}

func baseName(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}

func clip(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= titleRunes {
		return s
	}
	r := []rune(s)
	return string(r[:titleRunes-1]) + "…"
}

func linesOf(buf []byte) [][]byte {
	var out [][]byte
	sc := bufio.NewScanner(bytes.NewReader(buf))
	sc.Buffer(make([]byte, 0, 64*1024), 2<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		out = append(out, append([]byte(nil), line...))
	}
	return out
}

func readHit(path string) (hit, bool) {
	f, err := os.Open(path)
	if err != nil {
		return hit{}, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		return hit{}, false
	}
	h := hit{path: path, mtime: st.ModTime()}
	if st.Size() <= edgeBytes*2 {
		b, err := io.ReadAll(f)
		if err != nil {
			return hit{}, false
		}
		h.head, h.tail = b, b
		return h, true
	}
	head := make([]byte, edgeBytes)
	if _, err := io.ReadFull(f, head); err != nil {
		return hit{}, false
	}
	if _, err := f.Seek(-edgeBytes, io.SeekEnd); err != nil {
		return hit{}, false
	}
	tail := make([]byte, edgeBytes)
	if _, err := io.ReadFull(f, tail); err != nil {
		return hit{}, false
	}
	h.head = dropPartial(head, true)
	h.tail = dropPartial(tail, false)
	return h, true
}

func dropPartial(buf []byte, keepHead bool) []byte {
	if keepHead {
		// The read may end mid-line. Keep every complete line.
		i := bytes.LastIndexByte(buf, '\n')
		if i < 0 {
			return buf
		}
		return buf[:i]
	}
	// The tail read may start mid-line. Drop that fragment.
	i := bytes.IndexByte(buf, '\n')
	if i < 0 {
		return nil
	}
	return buf[i+1:]
}
