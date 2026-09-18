//go:build !windows

package terminal

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

// Start opens a PTY in dir and runs the user's shell there.
func Start(opts Options) (*Session, error) {
	cmd, shell, err := commandFor(opts)
	if err != nil {
		return nil, err
	}
	cols, rows := ClampSize(opts.Cols, opts.Rows)
	file, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})
	if err != nil {
		return nil, fmt.Errorf("terminal: start: %w", err)
	}
	return &Session{pty: file, cmd: cmd, Cwd: opts.Dir, Shell: shell}, nil
}

func commandFor(opts Options) (*exec.Cmd, string, error) {
	dir := opts.Dir
	info, err := os.Stat(dir)
	if err != nil {
		return nil, "", fmt.Errorf("terminal: working directory: %w", err)
	}
	if !info.IsDir() {
		return nil, "", fmt.Errorf("terminal: %s is not a directory", dir)
	}
	shell, args := Shell()
	if opts.Command != "" {
		shell, args = opts.Command, opts.Args
	}
	cmd := exec.Command(shell, args...)
	cmd.Dir = dir
	cmd.Env = withTermEnv(os.Environ())
	// Do not set Setpgid. pty.StartWithSize already sets Setsid+Setctty so
	// the shell owns a session; both flags together is fork/exec EPERM
	// on Darwin, which is how this used to open a dead panel.
	return cmd, shell, nil
}

// Resize tells the kernel the new cell size so full-screen tools reflow.
func (s *Session) Resize(cols, rows int) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	file := s.pty
	s.mu.Unlock()
	if file == nil {
		return nil
	}
	cols, rows = ClampSize(cols, rows)
	return pty.Setsize(file, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

// Close kills the process group and the PTY. Idempotent.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	file := s.pty
	cmd := s.cmd
	s.pty = nil
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Process.Kill()
	}
	if file != nil {
		return file.Close()
	}
	return nil
}
