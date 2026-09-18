package engine

import (
	"testing"
)

func TestPauseOpenGoalOnInterruptNoopsWithoutAGoal(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	e.pauseOpenGoalOnInterrupt(th.ID)
	if hasKind(t, e, th.ID, KindGoalCapped) {
		t.Fatal("no standing objective to pause")
	}
}

func TestPauseOpenGoalOnInterruptNoopsWhenAlreadyCapped(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{"goal_capped": true}); err != nil {
		t.Fatal(err)
	}
	e.pauseOpenGoalOnInterrupt(th.ID)
	if hasKind(t, e, th.ID, KindGoalCapped) {
		t.Fatal("a second cap event would look like the budget ran out twice")
	}
}

func TestPauseOpenGoalOnInterruptNoopsWhenBlocked(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if err := e.BlockThreadGoal(th.ID, "needs an external change"); err != nil {
		t.Fatal(err)
	}
	e.pauseOpenGoalOnInterrupt(th.ID)
	events, _ := e.Replay(th.ID, 0)
	for _, ev := range events {
		if ev.Kind == KindGoalCapped {
			t.Fatal("blocked is already waiting for resume; do not also cap")
		}
	}
}

func TestPauseOpenGoalOnInterruptMarksAPursuingGoal(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	e.pauseOpenGoalOnInterrupt(th.ID)
	got, err := e.Store().GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.GoalCapped || got.GoalBlocked || got.GoalComplete {
		t.Fatalf("want a paused open goal, got %+v", got)
	}
	if !hasKind(t, e, th.ID, KindGoalCapped) {
		t.Fatal("missing goal_capped")
	}
	events, _ := e.Replay(th.ID, 0)
	for _, ev := range events {
		if ev.Kind == KindGoalCapped && ev.Text != goalCappedReasonInterrupted {
			t.Fatalf("payload=%q", ev.Text)
		}
	}
}

func TestPauseOpenGoalOnInterruptMissingThreadIsANoop(t *testing.T) {
	e := newTestEngine(t)
	e.pauseOpenGoalOnInterrupt("th_missing")
}
