package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestPreemptIdleReportsIdle(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Preempt(th.ID); !errors.Is(err, ErrIdle) {
		t.Fatalf("idle preempt: %v", err)
	}
	if err := e.RetractSteer(th.ID, 1); !errors.Is(err, ErrIdle) {
		t.Fatalf("idle retract: %v", err)
	}
}

func TestRetractUnreadSteerDropsItFromReplay(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	release := holdTurn(t, e, th.ID)
	defer release()

	nudge := "change course now"
	if err := e.Steer(th.ID, nudge); err != nil {
		t.Fatalf("Steer: %v", err)
	}
	seq := steerSeq(t, e, th.ID, nudge)
	if err := e.RetractSteer(th.ID, seq); err != nil {
		t.Fatalf("RetractSteer: %v", err)
	}

	dump := replayDump(t, e, th.ID)
	if strings.Contains(dump, nudge) {
		t.Fatalf("retracted steering still in replay:\n%s", dump)
	}
	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var retracted bool
	for _, ev := range events {
		if ev.Kind == KindSteerRetracted {
			var p steerRetractPayload
			if json.Unmarshal([]byte(ev.Text), &p) == nil && p.Seq == seq {
				retracted = true
			}
		}
	}
	if !retracted {
		t.Fatal("missing steer_retracted on the timeline")
	}
	if err := e.RetractSteer(th.ID, seq); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second retract: %v", err)
	}
}

func TestPreemptKeepsTheTurnRunning(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	release := holdTurn(t, e, th.ID)
	defer release()

	if err := e.Preempt(th.ID); !errors.Is(err, ErrNoPendingSteer) {
		t.Fatalf("empty inbox: %v", err)
	}
	if err := e.Steer(th.ID, "prefer the shorter path"); err != nil {
		t.Fatalf("Steer: %v", err)
	}
	if err := e.Preempt(th.ID); err != nil {
		t.Fatalf("Preempt: %v", err)
	}
	st := e.Status(th.ID)
	if !st.Running {
		t.Fatal("preempt cancelled the turn; Stop is the only cancel")
	}
	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, ev := range events {
		if ev.Kind == KindSteerPreempted {
			saw = true
		}
	}
	if !saw {
		t.Fatal("missing steer_preempted on the timeline")
	}
}

func TestRetractUnknownSteerIsNotFound(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	release := holdTurn(t, e, th.ID)
	defer release()
	if err := e.Steer(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if err := e.RetractSteer(th.ID, 1<<20); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown seq: %v", err)
	}
}

func holdTurn(t *testing.T, e *Engine, threadID string) func() {
	t.Helper()
	turn := &store.Turn{ThreadID: threadID, UserText: "held open"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	reg := swarm.NewRegistry()
	rt := e.runtimeFor(threadID)
	_, cancel := context.WithCancel(context.Background())
	idle := make(chan struct{})
	if !rt.occupy(reg, cancel, turn.ID, idle) {
		t.Fatal("occupy")
	}
	return func() {
		rt.release(cancel, idle, turn.ID)
		reg.Close()
	}
}

func steerSeq(t *testing.T, e *Engine, threadID, text string) int64 {
	t.Helper()
	events, err := e.Replay(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind == KindSteer && events[i].Text == text {
			return events[i].Seq
		}
	}
	t.Fatalf("no steer event for %q", text)
	return 0
}

func TestSkipRetractedSteerMatchesALegacyUntaggedRow(t *testing.T) {
	retracted := map[int64]struct{}{9: {}}
	captions := map[string]struct{}{"[steer] never mind": {}}
	if !skipRetractedSteer(store.Message{Role: "user", Content: "[steer] never mind", EventSeq: 0}, retracted, captions) {
		t.Fatal("a pre-upgrade [steer] row must still drop after retract")
	}
	if skipRetractedSteer(store.Message{Role: "user", Content: "[steer] keep me", EventSeq: 0}, retracted, captions) {
		t.Fatal("an unretracted caption must stay")
	}
}
