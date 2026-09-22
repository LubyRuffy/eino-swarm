package server

import (
	"testing"
	"time"
)

func TestPresenceGCDropsAReserveThatNeverConnected(t *testing.T) {
	p := newPresence()
	p.mu.Lock()
	p.slots["old"] = &presenceSlot{
		client:  PresenceClient{ID: "old", Surface: "web"},
		created: time.Now().Add(-2 * time.Minute),
	}
	p.slots["fresh"] = &presenceSlot{
		client:  PresenceClient{ID: "fresh", Surface: "tui"},
		created: time.Now(),
	}
	p.mu.Unlock()
	p.gc()
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.slots["old"]; ok {
		t.Fatal("a reserve that never connected must not pin the process")
	}
	if _, ok := p.slots["fresh"]; !ok {
		t.Fatal("a fresh reserve is still inside the minute")
	}
}

func TestClientsIsEmptyWithoutARegistry(t *testing.T) {
	if (*Server)(nil).Clients() != nil {
		t.Fatal("nil server")
	}
	if (&Server{}).Clients() != nil {
		t.Fatal("server without presence")
	}
}
