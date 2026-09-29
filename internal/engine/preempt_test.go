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

func TestReviseUnreadSteerReplacesWhatTheModelWillRead(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	release := holdTurn(t, e, th.ID)
	defer release()

	if err := e.Steer(th.ID, "same words"); err != nil {
		t.Fatal(err)
	}
	if err := e.Steer(th.ID, "same words"); err != nil {
		t.Fatal(err)
	}
	first, second := steerSeqs(t, e, th.ID, "same words")
	if err := e.ReviseUnreadSteer(th.ID, first, "same words"); err != nil {
		t.Fatal(err)
	}
	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == KindSteerRevised {
			t.Fatal("an unchanged caption must not write another timeline row")
		}
	}
	if err := e.ReviseUnreadSteer(th.ID, first, "  first edited  "); err != nil {
		t.Fatalf("ReviseUnreadSteer: %v", err)
	}

	rt := e.runtimeFor(th.ID)
	rt.mu.Lock()
	reg := rt.reg
	rt.mu.Unlock()
	pending := reg.TakePendingSteerMessages()
	if len(pending) != 2 {
		t.Fatalf("inbox len=%d", len(pending))
	}
	if userMessageText(pending[0]) != "[steer] first edited" || swarm.SteerSeq(pending[0]) != first {
		t.Fatalf("first slot = %q seq %d", userMessageText(pending[0]), swarm.SteerSeq(pending[0]))
	}
	if userMessageText(pending[1]) != "[steer] same words" || swarm.SteerSeq(pending[1]) != second {
		t.Fatalf("second slot = %q seq %d", userMessageText(pending[1]), swarm.SteerSeq(pending[1]))
	}

	rows, err := e.Store().ListMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawEdited, sawOther bool
	for _, row := range rows {
		switch row.EventSeq {
		case first:
			sawEdited = row.Content == "[steer] first edited"
		case second:
			sawOther = row.Content == "[steer] same words"
		}
	}
	if !sawEdited || !sawOther {
		t.Fatalf("replay rows did not keep the unedited steer: %+v", rows)
	}

	events, err = e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var revised, originalKept bool
	for _, ev := range events {
		if ev.Kind == KindSteer && ev.Seq == first && ev.Text == "same words" {
			originalKept = true
		}
		if ev.Kind == KindSteerRevised {
			seq, text, ok := parseSteerRevision(ev.Text)
			if ok && seq == first && text == "first edited" {
				revised = true
			}
		}
	}
	if !revised || !originalKept {
		t.Fatal("the log must keep the original steer and record the revision")
	}
	if err := e.ReviseUnreadSteer(th.ID, first, "too late"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("consumed steer: %v", err)
	}
}

func TestReviseUnreadSteerKeepsPastedImages(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	release := holdTurn(t, e, th.ID)
	defer release()
	if err := e.SteerInput(th.ID, UserInput{
		Text:   "look here",
		Images: []ImageInput{{Name: "clip.png", MIME: "image/png", Data: tinyPNG}},
	}); err != nil {
		t.Fatal(err)
	}
	seq := steerSeq(t, e, th.ID, "look here")
	if err := e.ReviseUnreadSteer(th.ID, seq, "look closer"); err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(th.ID)
	rt.mu.Lock()
	reg := rt.reg
	rt.mu.Unlock()
	pending := reg.TakePendingSteerMessages()
	if len(pending) != 1 {
		t.Fatalf("inbox len=%d", len(pending))
	}
	if userMessageText(pending[0]) != "[steer] look closer" {
		t.Fatalf("caption = %q", userMessageText(pending[0]))
	}
	if len(imagesFromMessage(pending[0])) != 1 {
		t.Fatal("revise dropped the pasted image the model was going to see")
	}
	rows, err := e.Store().ListMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.EventSeq != seq {
			continue
		}
		if row.Content != "[steer] look closer" || len(row.Images) != 1 {
			t.Fatalf("stored steer = %+v", row)
		}
		return
	}
	t.Fatal("stored steer row missing")
}

func TestReviseUnreadSteerRefusesIdleEmptyAndGone(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.ReviseUnreadSteer(th.ID, 1, "later"); !errors.Is(err, ErrIdle) {
		t.Fatalf("idle revise: %v", err)
	}
	release := holdTurn(t, e, th.ID)
	defer release()
	if err := e.ReviseUnreadSteer(th.ID, 0, "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("bad seq: %v", err)
	}
	if err := e.Steer(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	seq := steerSeq(t, e, th.ID, "keep going")
	if err := e.ReviseUnreadSteer(th.ID, seq, "   "); err == nil {
		t.Fatal("an empty steer has nothing to inject")
	}
	if err := e.ReviseUnreadSteer(th.ID, 1<<20, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown seq: %v", err)
	}
	if err := e.RetractSteer(th.ID, seq); err != nil {
		t.Fatal(err)
	}
	if err := e.ReviseUnreadSteer(th.ID, seq, "after retract"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("retracted steer: %v", err)
	}
}

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

func steerSeqs(t *testing.T, e *Engine, threadID, text string) (int64, int64) {
	t.Helper()
	events, err := e.Replay(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var seqs []int64
	for _, ev := range events {
		if ev.Kind == KindSteer && ev.Text == text {
			seqs = append(seqs, ev.Seq)
		}
	}
	if len(seqs) < 2 {
		t.Fatalf("want two steers %q, got %v", text, seqs)
	}
	return seqs[0], seqs[1]
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
