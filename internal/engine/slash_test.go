package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

func TestStartTurnSlashGoalIsNotAUserTask(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurn(th.ID, "/goal keep going")
	if err != nil {
		t.Fatal(err)
	}
	if turn.UserText != "keep going" {
		t.Fatalf("the slash line must not be the user message, got %q", turn.UserText)
	}
	got, err := e.Store().GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Goal != "keep going" {
		t.Fatalf("goal=%q", got.Goal)
	}
	waitSettled(t, e, th.ID)
}

func TestStartTurnSlashGoalGluedWithoutASpace(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurn(th.ID, "/goal持续推进")
	if err != nil {
		t.Fatal(err)
	}
	if turn.UserText != "持续推进" {
		t.Fatalf("glued CJK must be the objective, got %q", turn.UserText)
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.Goal != "持续推进" {
		t.Fatalf("goal=%q", got.Goal)
	}
	waitSettled(t, e, th.ID)
}

func TestStartTurnFullwidthSlashGoal(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if _, err := e.StartTurn(th.ID, "／goal keep going"); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.Goal != "keep going" {
		t.Fatalf("goal=%q", got.Goal)
	}
	waitSettled(t, e, th.ID)
}

func TestStartTurnSlashGoalsStaysATask(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurn(th.ID, "/goals keep going")
	if err != nil {
		t.Fatal(err)
	}
	if turn.UserText != "/goals keep going" {
		t.Fatalf("user_text=%q", turn.UserText)
	}
	got, _ := e.Store().GetThread(th.ID)
	if strings.TrimSpace(got.Goal) != "" {
		t.Fatalf("goal=%q", got.Goal)
	}
	waitSettled(t, e, th.ID)
}

func TestStartTurnSlashGoalNeedsAnObjective(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if _, err := e.StartTurn(th.ID, "/goal"); err == nil || !strings.Contains(err.Error(), "objective") {
		t.Fatalf("err=%v", err)
	}
}

func TestStartTurnSlashGoalSteersWhenBusy(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	first, err := e.StartTurn(th.ID, "start the work")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if e.Status(th.ID).Running {
			got, err := e.StartTurn(th.ID, "/goal keep going")
			if err != nil {
				t.Fatal(err)
			}
			if got == nil || got.ID != first.ID {
				t.Fatalf("a live /goal must stay on this turn, got %+v", got)
			}
			if err := e.CompleteThreadGoal(th.ID, ""); err != nil {
				t.Fatal(err)
			}
			waitSettled(t, e, th.ID)
			if !hasKind(t, e, th.ID, KindGoal) {
				t.Fatal("missing goal event")
			}
			if !hasKind(t, e, th.ID, KindSteer) {
				t.Fatal("a live /goal send must steer so this turn sees the objective")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the turn finished before /goal could land")
}
