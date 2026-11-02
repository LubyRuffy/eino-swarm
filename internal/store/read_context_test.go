package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestContextOwnerReadsRoundTripMissingAndAlreadyCancelled(t *testing.T) {
	s := open(t)
	thread := Thread{Title: "context owner"}
	if err := s.CreateThread(&thread); err != nil {
		t.Fatal(err)
	}
	turn := Turn{ThreadID: thread.ID}
	if err := s.CreateTurn(&turn); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetThreadContext(context.Background(), thread.ID); err != nil || got.ID != thread.ID {
		t.Fatalf("thread read %+v %v", got, err)
	}
	if got, err := s.GetTurnContext(context.Background(), turn.ID); err != nil || got.ID != turn.ID {
		t.Fatalf("turn read %+v %v", got, err)
	}
	if _, err := s.GetThreadContext(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.GetTurnContext(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.GetThreadContext(ctx, thread.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled thread read %v", err)
	}
	if _, err := s.GetTurnContext(ctx, turn.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled turn read %v", err)
	}
	if _, err := s.GetThread(thread.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetTurn(turn.ID); err != nil {
		t.Fatal(err)
	}
}

// Hold the real pool's only SQLite connection and observe its WaitCount,
// then cancel without releasing that connection. A pre-call ctx check alone
// cannot pass: the blocked driver acquisition must honor cancellation too.
func TestContextOwnerReadCancellationInterruptsARealOccupiedConnection(t *testing.T) {
	for _, kind := range []string{"thread", "turn"} {
		t.Run(kind, func(t *testing.T) {
			s := open(t)
			thread := Thread{Title: "held owner"}
			if err := s.CreateThread(&thread); err != nil {
				t.Fatal(err)
			}
			turn := Turn{ThreadID: thread.ID}
			if err := s.CreateTurn(&turn); err != nil {
				t.Fatal(err)
			}
			database, err := s.DB().DB()
			if err != nil {
				t.Fatal(err)
			}
			connection, err := database.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = connection.Close() }()
			waits := database.Stats().WaitCount
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				var err error
				if kind == "thread" {
					_, err = s.GetThreadContext(ctx, thread.ID)
				} else {
					_, err = s.GetTurnContext(ctx, turn.ID)
				}
				result <- err
			}()
			deadline := time.NewTimer(2 * time.Second)
			defer deadline.Stop()
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			for database.Stats().WaitCount == waits {
				select {
				case <-ticker.C:
				case <-deadline.C:
					t.Fatal("read never waited for the occupied connection")
				}
			}
			cancel()
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("blocked %s read cancellation %v", kind, err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("cancelled read still depended on releasing another owner's connection")
			}
		})
	}
}
