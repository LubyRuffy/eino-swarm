package terminal

import (
	"os"
	"strings"
	"testing"
)

func TestHubCapsConcurrentSessions(t *testing.T) {
	h := NewHub(2)
	if !h.Acquire() || !h.Acquire() {
		t.Fatal("the first two slots must be free")
	}
	if h.Acquire() {
		t.Fatal("the cap is two")
	}
	if h.Count() != 2 {
		t.Fatalf("count=%d", h.Count())
	}
	h.Release()
	if !h.Acquire() {
		t.Fatal("a released slot must be reusable")
	}
	h.Release()
	h.Release()
	h.Release() // extra release must not go negative
	if h.Count() != 0 {
		t.Fatalf("count=%d after extra release", h.Count())
	}
}

func TestNewHubDefaultCap(t *testing.T) {
	h := NewHub(0)
	if h.max != MaxSessions {
		t.Fatalf("max=%d", h.max)
	}
}

func TestClampSize(t *testing.T) {
	cols, rows := ClampSize(0, 0)
	if cols != 10 || rows != 5 {
		t.Fatalf("zero clamped to %d×%d", cols, rows)
	}
	cols, rows = ClampSize(9999, 9999)
	if cols != 500 || rows != 200 {
		t.Fatalf("huge clamped to %d×%d", cols, rows)
	}
	cols, rows = ClampSize(80, 24)
	if cols != 80 || rows != 24 {
		t.Fatalf("passthrough %d×%d", cols, rows)
	}
}

func TestShellUsesEnvAndLoginFlag(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	name, args := Shell()
	if name != "/bin/zsh" || len(args) != 1 || args[0] != "-l" {
		t.Fatalf("zsh: %s %v", name, args)
	}
	t.Setenv("SHELL", "/bin/sh")
	name, args = Shell()
	if name != "/bin/sh" || args != nil {
		t.Fatalf("sh must not be a login shell: %s %v", name, args)
	}
	t.Setenv("SHELL", "")
	name, args = Shell()
	if name != "/bin/sh" {
		t.Fatalf("empty SHELL: %s", name)
	}
}

func TestAttachExposesTheFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "pty")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := Attach(f, "/tmp/ws", "/bin/sh")
	if s.PTY() != f || s.Cwd != "/tmp/ws" || s.Shell != "/bin/sh" {
		t.Fatalf("%+v", s)
	}
}

func TestNilHubAcquire(t *testing.T) {
	var h *Hub
	if !h.Acquire() {
		t.Fatal("a missing hub must not block a session")
	}
	h.Release()
	if h.Count() != 0 {
		t.Fatalf("count=%d", h.Count())
	}
}

func TestSessionPTYNil(t *testing.T) {
	if (*Session)(nil).PTY() != nil {
		t.Fatal("nil session")
	}
	if (&Session{}).PTY() != nil {
		t.Fatal("empty session")
	}
}

func TestWithTermEnvReplacesTerm(t *testing.T) {
	got := withTermEnv([]string{"HOME=/tmp", "TERM=dumb", "COLORTERM=no", "PATH=/bin"})
	var term, color string
	for _, kv := range got {
		name, val, _ := strings.Cut(kv, "=")
		switch name {
		case "TERM":
			if term != "" {
				t.Fatalf("duplicate TERM: %v", got)
			}
			term = val
		case "COLORTERM":
			if color != "" {
				t.Fatalf("duplicate COLORTERM: %v", got)
			}
			color = val
		}
	}
	if term != "xterm-256color" || color != "truecolor" {
		t.Fatalf("TERM=%q COLORTERM=%q from %v", term, color, got)
	}
}
