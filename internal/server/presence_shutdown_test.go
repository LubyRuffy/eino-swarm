package server

import (
	"errors"
	"sync"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
)

// Admission and the final idle decision share a mutex: either the socket is
// counted or the holder has committed shutdown and the attach must be refused.
func TestIdleShutdownAndShellAttachCannotBothWin(t *testing.T) {
	for i := 0; i < 100; i++ {
		p := newPresence()
		s := &Server{presence: p, engine: &engine.Engine{}}
		id, err := p.reserve("desktop", 1)
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var stopped, attached bool
		var release func()
		go func() { defer wg.Done(); <-start; stopped = s.TryIdleShutdown() }()
		go func() { defer wg.Done(); <-start; _, release, attached = p.attach(id) }()
		close(start)
		wg.Wait()
		if stopped == attached {
			t.Fatalf("exactly one must win: shutdown=%v attach=%v", stopped, attached)
		}
		if attached {
			if s.TryIdleShutdown() || len(s.Clients()) != 1 {
				t.Fatal("a live shell did not pin the engine")
			}
			release()
			if !s.TryIdleShutdown() {
				t.Fatal("released presence still pinned engine")
			}
		}
		if !p.isStopping() {
			t.Fatal("shutdown did not close admission")
		}
		if _, err := p.reserve("web", 0); !errors.Is(err, ErrEngineStopping) {
			t.Fatalf("late reserve: %v", err)
		}
		if _, _, ok := p.attach(id); ok {
			t.Fatal("late attach entered a stopping engine")
		}
	}
}
