package server

import (
	"errors"
	"sync"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/update"
)

func TestConfirmedHostUpdateAndIdleShutdownCannotBothWin(t *testing.T) {
	for range 100 {
		s := &Server{presence: newPresence(), engine: &engine.Engine{}}
		var release func()
		var holdErr error
		var stopped bool
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); <-start; release, holdErr = s.HoldHostUpdate() }()
		go func() { defer wg.Done(); <-start; stopped = s.TryIdleShutdown() }()
		close(start)
		wg.Wait()
		if stopped {
			if !errors.Is(holdErr, ErrEngineStopping) {
				t.Fatalf("late confirmation did not fail against committed idle: %v", holdErr)
			}
			continue
		}
		if holdErr != nil || s.ActiveWork() != 1 || len(s.Clients()) != 0 || s.TryIdleShutdown() {
			t.Fatalf("the engine lost ownership of an admitted update: %v", holdErr)
		}
		if _, err := s.HoldHostUpdate(); !errors.Is(err, update.ErrBusy) {
			t.Fatalf("a second installer took the hold: %v", err)
		}
		release()
		release()
		if s.ActiveWork() != 0 || !s.TryIdleShutdown() {
			t.Fatal("finished update prevents idle shutdown")
		}
	}
}
