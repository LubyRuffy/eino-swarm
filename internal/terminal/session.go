// Package terminal spawns a login shell on a PTY. The HTTP layer names a
// conversation or a project; this package never takes a client-supplied path.
package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// MaxSessions is a process-wide cap so a stuck click handler cannot
	// fork-bomb the machine. Eight is enough for a person; the agents have
	// their own exec tool.
	MaxSessions = 8
	DefaultCols = 80
	DefaultRows = 24
)

// Options is how a session starts. Dir is already resolved by the engine.
type Options struct {
	Dir  string
	Cols int
	Rows int
	// Command overrides the user's shell. Tests use it so a .zshrc cannot
	// hang the suite; production leaves it empty.
	Command string
	Args    []string
}

// Session is one live PTY. Close kills the process group.
type Session struct {
	mu    sync.Mutex
	pty   *os.File
	cmd   *exec.Cmd
	Cwd   string
	Shell string
}

// PTY is the slave the WebSocket copies to and from. Close nils it; callers
// must tolerate a nil file.
func (s *Session) PTY() *os.File {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pty
}

// Attach wraps an already-open PTY so the WebSocket copy loop can be tested
// without fork/exec.
func Attach(file *os.File, cwd, shell string) *Session {
	return &Session{pty: file, Cwd: cwd, Shell: shell}
}

// Hub caps how many sessions this process will keep.
type Hub struct {
	mu  sync.Mutex
	n   int
	max int
}

func NewHub(max int) *Hub {
	if max <= 0 {
		max = MaxSessions
	}
	return &Hub{max: max}
}

func (h *Hub) Acquire() bool {
	if h == nil {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.n >= h.max {
		return false
	}
	h.n++
	return true
}

func (h *Hub) Release() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.n > 0 {
		h.n--
	}
}

func (h *Hub) Count() int {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.n
}

// ClampSize keeps a resize inside what a PTY ioctl will accept.
func ClampSize(cols, rows int) (int, int) {
	if cols < 10 {
		cols = 10
	}
	if cols > 500 {
		cols = 500
	}
	if rows < 5 {
		rows = 5
	}
	if rows > 200 {
		rows = 200
	}
	return cols, rows
}

// Shell is the executable a new session runs. $SHELL if set, otherwise the
// POSIX fallback. zsh/bash get -l so the same rc files as Terminal.app apply.
func Shell() (name string, args []string) {
	name = strings.TrimSpace(os.Getenv("SHELL"))
	if name == "" {
		name = "/bin/sh"
	}
	switch filepath.Base(name) {
	case "zsh", "bash":
		return name, []string{"-l"}
	default:
		return name, nil
	}
}

func withTermEnv(env []string) []string {
	out := make([]string, 0, len(env)+2)
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if name == "TERM" || name == "COLORTERM" {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "TERM=xterm-256color", "COLORTERM=truecolor")
}
