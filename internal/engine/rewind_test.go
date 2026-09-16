package engine

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestRewindFromAnEarlierMessageDropsWhatCameAfter(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")

	first, err := e.StartTurn(th.ID, "the first request")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	second, err := e.StartTurn(th.ID, "the second request")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, second.ID)

	seq := userMessageSeq(t, e, th.ID, first.ID)
	sub := e.Subscribe(th.ID)
	defer sub.Close()

	again, err := e.StartTurnInput(th.ID, UserInput{
		Text: "the edited request", FromEventSeq: seq,
	})
	if err != nil {
		t.Fatalf("StartTurnInput rewind: %v", err)
	}
	waitForTurn(t, e, again.ID)

	if saw := waitForKind(t, sub.C, KindRewound, 5*time.Second); saw.Text != strconv.FormatInt(seq, 10) {
		t.Fatalf("rewound event text=%q want seq %d", saw.Text, seq)
	}

	dump := replayDump(t, e, th.ID)
	if !strings.Contains(dump, "the edited request") {
		t.Fatalf("rewound turn missing the new request:\n%s", dump)
	}
	if strings.Contains(dump, "the first request") || strings.Contains(dump, "the second request") {
		t.Fatalf("deleted turns still in model replay:\n%s", dump)
	}

	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 1 || turns[0].ID != again.ID {
		t.Fatalf("want only the replacement turn, got %+v", turns)
	}
	events, _ := e.Store().ListEvents(th.ID, 0, 0)
	for _, ev := range events {
		if ev.Kind == KindRewound {
			t.Fatal("rewound must not be stored; a reload already has the truncated log")
		}
		if ev.TurnID == first.ID || ev.TurnID == second.ID {
			t.Fatalf("a dropped turn still has events: %+v", ev)
		}
	}
}

func TestRewindRejectsANonUserEvent(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurn(th.ID, "the first request")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	events, _ := e.Store().ListEvents(th.ID, 0, 0)
	var other int64
	for _, ev := range events {
		if ev.Kind != KindUser {
			other = ev.Seq
			break
		}
	}
	if other == 0 {
		t.Fatal("need a non-user event to refuse")
	}
	if _, err := e.StartTurnInput(th.ID, UserInput{
		Text: "edited", FromEventSeq: other,
	}); !errors.Is(err, ErrNotRewindable) {
		t.Fatalf("want ErrNotRewindable, got %v", err)
	}
}

func TestRewindMissingSeqIsNotFound(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if _, err := e.StartTurnInput(th.ID, UserInput{
		Text: "edited", FromEventSeq: 99,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestRewindInterruptsARunningTurn(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sub := e.Subscribe(th.ID)
	defer sub.Close()

	turn, err := e.StartTurn(th.ID, "the original request")
	if err != nil {
		t.Fatal(err)
	}
	user := waitForKind(t, sub.C, KindUser, 5*time.Second)
	again, err := e.StartTurnInput(th.ID, UserInput{
		Text: "the replacement request", FromEventSeq: user.Seq,
	})
	if err != nil {
		t.Fatalf("rewind while running: %v", err)
	}
	waitForTurn(t, e, again.ID)

	if _, err := e.Store().GetTurn(turn.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the interrupted turn should have been dropped, got %v", err)
	}
	dump := replayDump(t, e, th.ID)
	if strings.Contains(dump, "the original request") {
		t.Fatalf("interrupted request still in replay:\n%s", dump)
	}
	if !strings.Contains(dump, "the replacement request") {
		t.Fatalf("replacement missing from replay:\n%s", dump)
	}
}

func TestRewindEmptyTextDoesNotWipeTheLog(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	first, err := e.StartTurn(th.ID, "the first request")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	seq := userMessageSeq(t, e, th.ID, first.ID)
	if _, err := e.StartTurnInput(th.ID, UserInput{
		Text: "  ", FromEventSeq: seq,
	}); err == nil {
		t.Fatal("an empty replacement must be refused")
	}
	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 1 || turns[0].ID != first.ID {
		t.Fatalf("refusing an empty edit must not wipe the conversation, got %+v", turns)
	}
}

func TestRewindRejectsAGoalContinuation(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurn(th.ID, "the first request")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	seq := userMessageSeq(t, e, th.ID, turn.ID)
	if _, err := e.StartTurnInput(th.ID, UserInput{
		Text: "edited", FromEventSeq: seq, ContinueGoal: true,
	}); err == nil {
		t.Fatal("a standing-objective continuation must not rewind")
	}
	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 1 || turns[0].ID != turn.ID {
		t.Fatalf("a refused rewind must leave the original turn, got %+v", turns)
	}
}

func TestRewindReusesTheOriginalImages(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurnInput(th.ID, UserInput{
		Text:   "look",
		Images: []ImageInput{{Name: "clip.png", MIME: "image/png", Data: tinyPNG}},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	seq := userMessageSeq(t, e, th.ID, turn.ID)
	orig := userImages(t, e, turn.ID)

	again, err := e.StartTurnInput(th.ID, UserInput{
		Text: "look again", FromEventSeq: seq,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, again.ID)
	got := userImages(t, e, again.ID)
	if len(got) != 1 || len(orig) != 1 || got[0].ID != orig[0].ID {
		t.Fatalf("want the original image handle reused, orig=%+v got=%+v", orig, got)
	}
}

func TestRewindAllowsAnImageOnlyReplacement(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurnInput(th.ID, UserInput{
		Text:   "look",
		Images: []ImageInput{{Name: "clip.png", MIME: "image/png", Data: tinyPNG}},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	seq := userMessageSeq(t, e, th.ID, turn.ID)
	again, err := e.StartTurnInput(th.ID, UserInput{
		Text: "  ", FromEventSeq: seq,
	})
	if err != nil {
		t.Fatalf("blank text with the original image must still send: %v", err)
	}
	waitForTurn(t, e, again.ID)
	if n := len(userImages(t, e, again.ID)); n != 1 {
		t.Fatalf("the replacement lost the image, n=%d", n)
	}
}

func TestADroppedTurnCannotRecordAfterRewind(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	first, err := e.StartTurn(th.ID, "the first request")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	seq := userMessageSeq(t, e, th.ID, first.ID)
	again, err := e.StartTurnInput(th.ID, UserInput{
		Text: "the edited request", FromEventSeq: seq,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, again.ID)
	e.record(store.Event{ThreadID: th.ID, TurnID: first.ID, Kind: KindTitle, Text: "ghost"})
	events, err := e.Store().ListEvents(th.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.TurnID == first.ID || ev.Text == "ghost" {
			t.Fatalf("a dropped turn recorded after rewind: %+v", ev)
		}
	}
}

func TestApplyRewindGivesUpIfTheTurnWillNotStop(t *testing.T) {
	orig := rewindWait
	rewindWait = 40 * time.Millisecond
	t.Cleanup(func() { rewindWait = orig })

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	rt := e.runtimeFor(th.ID)
	idle := make(chan struct{})
	_, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if !rt.occupy(nil, cancel, "tn_stuck", idle) {
		t.Fatal("occupy")
	}
	t.Cleanup(func() { rt.release(cancel, idle, "tn_stuck") })
	if err := e.applyRewind(th.ID, 1); err == nil || !strings.Contains(err.Error(), "did not stop") {
		t.Fatalf("want a timeout, got %v", err)
	}
}

func TestApplyRewindFailsIfTheCutVanished(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurn(th.ID, "the first request")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	seq := userMessageSeq(t, e, th.ID, turn.ID)
	if err := e.Store().TruncateFromEventSeq(th.ID, seq); err != nil {
		t.Fatal(err)
	}
	if err := e.applyRewind(th.ID, seq); err == nil {
		t.Fatal("truncating a vanished cut must fail")
	}
}

func TestWaitTurnClosedHandlesAMissingTurn(t *testing.T) {
	e := newTestEngine(t)
	e.waitTurnClosed("", 10*time.Millisecond)
	e.waitTurnClosed("tn_missing", 40*time.Millisecond)
}

func TestWaitTurnClosedTimesOutOnAStuckRow(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, UserText: "stuck"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.waitTurnClosed(turn.ID, 25*time.Millisecond)
}

func TestLoadImageInputsSkipsMissingFiles(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if got := e.loadImageInputs(th.ID, nil); got != nil {
		t.Fatalf("empty refs: %+v", got)
	}
	got := e.loadImageInputs(th.ID, []store.ImageRef{{ID: "img_dead", MIME: "image/png"}})
	if len(got) != 0 {
		t.Fatalf("a missing paste must not invent pixels: %+v", got)
	}
}

func userImages(t *testing.T, e *Engine, turnID string) []store.ImageRef {
	t.Helper()
	events, err := e.Store().ListTurnEvents(turnID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == KindUser {
			return ev.Images
		}
	}
	t.Fatal("no user_message on the turn")
	return nil
}

func userMessageSeq(t *testing.T, e *Engine, threadID, turnID string) int64 {
	t.Helper()
	events, err := e.Store().ListTurnEvents(turnID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == KindUser {
			return ev.Seq
		}
	}
	t.Fatal("no user_message on the turn")
	return 0
}

func waitForKind(t *testing.T, ch <-chan store.Event, kind string, d time.Duration) store.Event {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case ev := <-ch:
			if ev.Kind == kind {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", kind)
			return store.Event{}
		}
	}
}
