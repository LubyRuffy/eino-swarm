//go:build windows

package terminal

import "fmt"

// Start is not implemented on Windows yet: creack/pty is a Unix PTY.
func Start(opts Options) (*Session, error) {
	return nil, fmt.Errorf("terminal: integrated terminals are not available on Windows yet")
}

func (s *Session) Resize(cols, rows int) error { return nil }

func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	file := s.pty
	s.pty = nil
	s.mu.Unlock()
	if file != nil {
		return file.Close()
	}
	return nil
}
