package lease

import (
	"context"
	"testing"
	"time"
)

func TestShouldStopKeepsTheEngineWhileAnyoneIsAttached(t *testing.T) {
	busy := []Load{
		{Clients: 1, Age: time.Hour, QuietFor: time.Hour},
		{PhoneOnline: true, Age: time.Hour, QuietFor: time.Hour},
		{RunningTurns: 1, Age: time.Hour, QuietFor: time.Hour},
		{Age: StartGrace - time.Second, QuietFor: time.Hour},
		{Age: time.Hour, QuietFor: IdleGrace - time.Millisecond},
	}
	for _, load := range busy {
		if ShouldStop(load) {
			t.Fatalf("still in use, must not exit: %+v", load)
		}
	}
}

func TestShouldStopExitsOnlyAfterTheIdleGrace(t *testing.T) {
	load := Load{Age: StartGrace, QuietFor: IdleGrace}
	if !ShouldStop(load) {
		t.Fatalf("nothing attached and nothing running must exit: %+v", load)
	}
}

func TestWatcherReturnsOnceIdle(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		(Watcher{StartGrace: time.Millisecond, IdleGrace: 20 * time.Millisecond, Poll: 5 * time.Millisecond}).Run(
			context.Background(), time.Now().Add(-time.Second),
			func() (int, bool, int) { return 0, false, 0 },
		)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("an idle engine must exit")
	}
}

func TestWatcherStaysWhileAClientIsConnected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		(Watcher{StartGrace: time.Millisecond, IdleGrace: 10 * time.Millisecond, Poll: 5 * time.Millisecond}).Run(
			ctx, time.Now().Add(-time.Second),
			func() (int, bool, int) { return 1, false, 0 },
		)
	}()
	select {
	case <-done:
		t.Fatal("a connected client must keep the engine")
	case <-time.After(80 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel must unblock the watcher")
	}
}

func TestWatcherUsesTheDefaultIntervals(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	(Watcher{}).Run(ctx, time.Now(), func() (int, bool, int) { return 0, false, 0 })
}
