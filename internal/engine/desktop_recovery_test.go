package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestDesktopRecoveryHasATurnTraceWithoutChangingItsWork(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	d := DesktopRecoveryInput{SessionID: "session-1", Generation: 1, Attempt: 1, ShellPID: 123, EnginePID: 456, ThreadID: th.ID, TurnID: turn.ID, Phase: "replacing", Reason: "paint_timeout", At: time.Now().UTC(), DurationMS: 20000}
	if err := e.RecordDesktopRecovery(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("trace=%+v %v", events, err)
	}
	var got DesktopRecoveryInput
	if events[0].Kind != "desktop_recovery" || events[0].AgentID != "" || json.Unmarshal([]byte(events[0].Text), &got) != nil || got.SessionID != d.SessionID || got.EnginePID != d.EnginePID {
		t.Fatalf("lost recovery provenance: %+v %+v", events[0], got)
	}
	select {
	case ev := <-sub.C:
		if ev.Seq != events[0].Seq {
			t.Fatal("live and durable recovery order differ")
		}
	default:
		t.Fatal("missing recovery event")
	}
	loaded, err := e.store.GetTurn(turn.ID)
	if err != nil || loaded.Status != store.TurnRunning {
		t.Fatalf("recovery changed task: %+v %v", loaded, err)
	}
	calls, err := e.store.ListLLMCalls(turn.ID)
	if err != nil || len(calls) != 0 {
		t.Fatal("recovery fabricated model work", err)
	}
}

func TestDesktopRecoveryRejectsWrongAttributionAndLostStorage(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	d := DesktopRecoveryInput{ThreadID: "another", TurnID: turn.ID}
	if err := e.RecordDesktopRecovery(context.Background(), d); !errors.Is(err, ErrInvalidDesktopRecovery) {
		t.Fatal("wrong turn attribution accepted", err)
	}
	d.ThreadID = th.ID
	d.TurnID = "missing"
	if err := e.RecordDesktopRecovery(context.Background(), d); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("fabricated turn accepted", err)
	}
	d.TurnID = turn.ID
	e.droppedTurns[turn.ID] = struct{}{}
	if err := e.RecordDesktopRecovery(context.Background(), d); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("rewound turn got late recovery", err)
	}
	delete(e.droppedTurns, turn.ID)
	if err := e.store.DB().Exec("CREATE TRIGGER recovery_disk_failure BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT, 'disk full'); END").Error; err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	if err := e.RecordDesktopRecovery(context.Background(), d); err == nil {
		t.Fatal("failed event write was acknowledged")
	}
	select {
	case ev := <-sub.C:
		t.Fatalf("undurable recovery broadcast: %+v", ev)
	default:
	}
	if err := e.RecordDesktopRecovery(context.Background(), DesktopRecoveryInput{}); err != nil {
		t.Fatal("empty page needs no fabricated turn", err)
	}
	if err := e.RecordDesktopRecovery(context.Background(), DesktopRecoveryInput{ThreadID: th.ID}); err != nil {
		t.Fatal("unsent conversation needs no fabricated turn", err)
	}
	if err := e.RecordDesktopRecovery(context.Background(), DesktopRecoveryInput{ThreadID: "missing"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("unknown conversation got metadata acknowledgement", err)
	}
}

func TestDesktopRecoveryCancellationAndInvalidTimeCannotPublishAReceipt(t *testing.T) {
	e := newTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := e.RecordDesktopRecovery(ctx, DesktopRecoveryInput{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled metadata recording continued", err)
	}
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	in := DesktopRecoveryInput{ThreadID: th.ID, TurnID: turn.ID, At: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}
	if err := e.RecordDesktopRecovery(context.Background(), in); !errors.Is(err, ErrInvalidDesktopRecovery) {
		t.Fatal("unserializable timestamp accepted", err)
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil || len(events) != 0 {
		t.Fatal("invalid metadata left a receipt", events, err)
	}
}
