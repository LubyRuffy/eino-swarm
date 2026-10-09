package clients

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type catalogTask struct {
	info                      os.FileInfo
	task                      Task
	valid, open, staysRunning bool
}

type sessionScan struct {
	tool, root        string
	previous, current map[string]catalogTask
}

var catalogMu sync.Mutex
var catalogSnapshots = map[string]*sessionScan{}

// Retain parsed rows only for files in the latest walk of each configured root.
// Raw transcript buffers are never retained, and a root change drops its cache.
func beginSessionScan(tool, root string) *sessionScan {
	catalogMu.Lock()
	scan := &sessionScan{tool: tool, root: root, current: make(map[string]catalogTask)}
	if previous := catalogSnapshots[tool]; previous != nil && previous.root == root {
		scan.previous = previous.current
	}
	return scan
}

func (scan *sessionScan) finish() {
	scan.previous = nil
	catalogSnapshots[scan.tool] = scan
	catalogMu.Unlock()
}

func (scan *sessionScan) read(path string, now time.Time, stale time.Duration) (Task, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return Task{}, false
	}
	row, cached := scan.previous[path]
	if !cached || !os.SameFile(row.info, info) || row.info.Size() != info.Size() || !row.info.ModTime().Equal(info.ModTime()) || row.info.Mode() != info.Mode() {
		h, ok := catalogRead(path)
		if !ok {
			return Task{}, false
		}
		row = catalogTask{info: info, valid: hasTranscript(scan.tool, h)}
		if row.valid {
			row.task = parseCatalogTask(scan.tool, h)
			row.open = statusOf(scan.tool, h, h.mtime, stale) == StatusRunning
			// Explicit Codex task_started remains running until a finish marker;
			// Claude, Cursor and marker-free Codex rows age with the stale setting.
			row.staysRunning = scan.tool == ToolCodex && statusOf(scan.tool, h, h.mtime.Add(stale+time.Second), stale) == StatusRunning
		}
	}
	scan.current[path] = row
	if !row.valid {
		return Task{}, false
	}
	task := row.task
	task.Status = freshStatus(task.UpdatedAt, now, stale, row.open)
	if row.staysRunning {
		task.Status = StatusRunning
	}
	rememberSession(task.ID, path)
	return task, true
}

func parseCatalogTask(tool string, h hit) Task {
	task := Task{UpdatedAt: h.mtime}
	id := strings.TrimSuffix(filepath.Base(h.path), ".jsonl")
	switch tool {
	case ToolClaude:
		task.Title, task.CWD = claudeTitle(h.head)
	case ToolCodex:
		var sessionID string
		task.Title, task.CWD, sessionID = codexMeta(h.head)
		if sessionID != "" {
			id = sessionID
		}
	case ToolCursor:
		task.Title = cursorTitle(h.head)
	}
	task.ID = tool + ":" + id
	if task.Title == "" {
		task.Title = id
	}
	return task
}
