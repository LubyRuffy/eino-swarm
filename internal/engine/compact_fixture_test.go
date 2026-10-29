package engine

import (
	"testing"
	"time"
)

func newManualCompactEngine(t *testing.T) *Engine {
	t.Helper()
	e := newTestEngine(t)
	// These cases exercise manual compact, including its fallback model seam.
	// Refuse optional post-turn actors before creating turns: a terminal row
	// or the finish gate does not join a wrapper not yet registered on wg.
	e.sessions.stop()
	if !e.sessions.wait(5 * time.Second) {
		t.Fatal("manual compact fixture has an active background briefing")
	}
	return e
}
