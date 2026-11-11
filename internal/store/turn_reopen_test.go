package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestReopenFailedTurnKeepsElapsedTimeAndRejectsASecondOpen(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "turns.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	withThreads(t, s, "thread")
	turn := &Turn{ThreadID: "thread", UserText: "Inspect the assigned work.", Status: TurnRunning}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishTurn(turn.ID, TurnError, "partial", "the service refused"); err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC().Add(-2500 * time.Millisecond)
	if err := s.ReopenFailedTurn(turn.ID, started); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != TurnRunning || got.Error != "" || got.Final != "" || got.EndedAt != nil {
		t.Fatalf("reopened turn: %+v", got)
	}
	if got.StartedAt.Sub(started).Abs() > time.Second {
		t.Fatalf("started at %s, want %s", got.StartedAt, started)
	}
	if err := s.ReopenFailedTurn(turn.ID, time.Now().UTC()); !errors.Is(err, ErrTurnNotFailed) {
		t.Fatalf("second reopen: %v", err)
	}
	again, err := s.GetTurn(turn.ID)
	if err != nil || again.Status != TurnRunning || again.Error != "" {
		t.Fatalf("second reopen changed the running turn: %+v, %v", again, err)
	}

	done := &Turn{ThreadID: "thread", UserText: "A finished request.", Status: TurnRunning}
	if err := s.CreateTurn(done); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishTurn(done.ID, TurnDone, "answered", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.ReopenFailedTurn(done.ID, time.Now().UTC()); !errors.Is(err, ErrTurnNotFailed) {
		t.Fatalf("done turn: %v", err)
	}
	finished, err := s.GetTurn(done.ID)
	if err != nil || finished.Status != TurnDone || finished.Final != "answered" {
		t.Fatalf("done turn changed: %+v, %v", finished, err)
	}
	if err := s.ReopenFailedTurn("missing", time.Now().UTC()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing turn: %v", err)
	}
}
