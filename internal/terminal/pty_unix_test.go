//go:build !windows

package terminal

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestStartRunsInTheGivenDirectory(t *testing.T) {
	dir := t.TempDir()
	s := startOrSkip(t, Options{
		Dir:     dir,
		Cols:    80,
		Rows:    24,
		Command: "/bin/pwd",
	})
	got := readPTY(t, s, dir)
	if !strings.Contains(got, dir) {
		t.Fatalf("pwd output %q does not contain the working directory %q", got, dir)
	}
	if err := s.Resize(100, 30); err != nil {
		t.Fatalf("Resize: %v", err)
	}
}

func TestCommandForUsesTheDirectoryAndOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SHELL", "/bin/zsh")
	cmd, shell, err := commandFor(Options{Dir: dir, Command: "/bin/pwd"})
	if err != nil {
		t.Fatal(err)
	}
	if shell != "/bin/pwd" || cmd.Path != "/bin/pwd" || cmd.Dir != dir {
		t.Fatalf("cmd=%q dir=%q shell=%q", cmd.Path, cmd.Dir, shell)
	}
	joined := strings.Join(cmd.Env, ",")
	if !strings.Contains(joined, "TERM=xterm-256color") {
		t.Fatalf("env: %v", cmd.Env)
	}
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Setpgid {
		t.Fatal("Setpgid plus pty Setsid is EPERM on Darwin; leave the group to Setsid")
	}
	cmd, shell, err = commandFor(Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if shell != "/bin/zsh" || len(cmd.Args) < 2 || cmd.Args[1] != "-l" {
		t.Fatalf("login zsh: %s %v", shell, cmd.Args)
	}
}

func TestCommandForDoesNotSetPgid(t *testing.T) {
	dir := t.TempDir()
	cmd, _, err := commandFor(Options{Dir: dir, Command: "/bin/pwd"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Setpgid {
		t.Fatal("Setpgid plus pty Setsid is EPERM on Darwin")
	}
}

func TestCommandForRefusesAMissingDirectory(t *testing.T) {
	_, _, err := commandFor(Options{Dir: "/no/such/zwai-terminal-dir"})
	if err == nil {
		t.Fatal("missing dir must fail")
	}
}

func TestCommandForRefusesAFile(t *testing.T) {
	path := t.TempDir() + "/file"
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := commandFor(Options{Dir: path}); err == nil {
		t.Fatal("a file must not be a working directory")
	}
}

func TestCloseClosesAStandInFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "pty")
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{pty: f}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if s.PTY() != nil {
		t.Fatal("Close must drop the file")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestCloseKillsARunningCommand(t *testing.T) {
	dir := t.TempDir()
	s := startOrSkip(t, Options{
		Dir:     dir,
		Cols:    20,
		Rows:    8,
		Command: "/bin/sleep",
		Args:    []string{"30"},
	})
	if s.cmd == nil || s.cmd.Process == nil {
		t.Fatal("sleep must have a process")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestNilSessionCloseAndResize(t *testing.T) {
	var missing *Session
	if err := missing.Close(); err != nil {
		t.Fatalf("nil Close: %v", err)
	}
	if err := missing.Resize(80, 24); err != nil {
		t.Fatalf("nil Resize: %v", err)
	}
	if err := (&Session{}).Resize(80, 24); err != nil {
		t.Fatalf("empty Resize: %v", err)
	}
}

func TestResizeOnAStandInFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "pty")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := &Session{pty: f}
	_ = s.Resize(40, 12)
}

func startOrSkip(t *testing.T, opts Options) *Session {
	t.Helper()
	s, err := Start(opts)
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skip(err.Error())
		}
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func readPTY(t *testing.T, s *Session, want string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var buf bytes.Buffer
	tmp := make([]byte, 1024)
	for time.Now().Before(deadline) {
		file := s.PTY()
		if file == nil {
			break
		}
		_ = file.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, err := file.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
			if strings.Contains(buf.String(), want) {
				return buf.String()
			}
		}
		if err == io.EOF && buf.Len() > 0 {
			break
		}
	}
	return buf.String()
}
