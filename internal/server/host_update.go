package server

import (
	"sync"

	"github.com/LubyRuffy/eino-swarm/internal/update"
)

// ActiveWork includes the job that owns a Linux host update while its phone
// is disconnected. The idle watcher and the final idle commit use this count.
func (s *Server) ActiveWork() int {
	s.presence.mu.Lock()
	defer s.presence.mu.Unlock()
	return s.activeWorkLocked()
}

func (s *Server) activeWorkLocked() int {
	active := s.engine.ActiveWork()
	if s.presence.hostUpdate {
		active++
	}
	return active
}

// HoldHostUpdate admits a confirmed update against the idle commit under the
// same lock as shell presence. It does not register a user-facing client.
func (s *Server) HoldHostUpdate() (func(), error) {
	p := s.presence
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopping {
		return nil, ErrEngineStopping
	}
	if p.hostUpdate {
		return nil, update.ErrBusy
	}
	p.hostUpdate = true
	return sync.OnceFunc(func() {
		p.mu.Lock()
		p.hostUpdate = false
		p.mu.Unlock()
	}), nil
}
