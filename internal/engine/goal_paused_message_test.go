package engine

import (
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestAHumanMessageDoesNotResumeABlockedGoal(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })
	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 1
	th, err := e.CreateThread("status discussion", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadGoal(th.ID, "deliver the agreed result"); err != nil {
		t.Fatal(err)
	}
	if err := e.BlockThreadGoal(th.ID, "requires an external decision"); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{"goal_auto_turns": 3}); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "explain the current state")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	waitSettled(t, e, th.ID)
	got, err := e.Store().GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	turns, _ := e.Store().ListTurns(th.ID)
	if !got.GoalBlocked || got.GoalBlockReason != "requires an external decision" || got.GoalAutoTurns != 3 || len(turns) != 1 {
		t.Fatalf("answering a message must retain the blocked goal: thread=%+v turns=%d", got, len(turns))
	}
	resumed, err := e.ResumeThreadGoal(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, resumed.ID)
	waitForTurnCount(t, e, th.ID, 3)
	waitSettled(t, e, th.ID)
	got, _ = e.Store().GetThread(th.ID)
	if got.GoalBlocked || !got.GoalCapped || got.GoalAutoTurns != 1 {
		t.Fatalf("explicit resume must clear the block and grant its own budget: %+v", got)
	}
}

func TestAHumanMessageResetsAnUnpausedGoalBudget(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })
	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 1
	th, err := e.CreateThread("active discussion", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadGoal(th.ID, "deliver the agreed result"); err != nil {
		t.Fatal(err)
	}
	// A queued human follow-up can arrive after the last automatic turn was
	// admitted but before finalization has emitted the cap event.
	if err := e.Store().UpdateThread(th.ID, map[string]any{"goal_auto_turns": 1}); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "incorporate this additional constraint")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	waitForTurnCount(t, e, th.ID, 2)
	waitSettled(t, e, th.ID)
	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 2 || !turns[1].GoalContinue {
		t.Fatalf("a human message during pursuit must renew only the consecutive auto-turn count: %+v", turns)
	}
}

func TestPausedStandingGoalRequiresAnOpenPausedObjective(t *testing.T) {
	for _, tc := range []struct {
		name string
		th   *store.Thread
		want bool
	}{
		{"missing", nil, false},
		{"empty", &store.Thread{GoalCapped: true}, false},
		{"completed", &store.Thread{Goal: "deliver", GoalComplete: true, GoalCapped: true}, false},
		{"pursuing", &store.Thread{Goal: "deliver"}, false},
		{"capped", &store.Thread{Goal: "deliver", GoalCapped: true}, true},
		{"blocked", &store.Thread{Goal: "deliver", GoalBlocked: true}, true},
		{"idle", &store.Thread{Goal: "deliver", GoalIdle: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := pausedStandingGoal(tc.th); got != tc.want {
				t.Fatalf("paused=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestAFailedAnswerDoesNotChangeTheGoalPause(t *testing.T) {
	provider.SetMockFailure(errors.New("provider refused this answer"))
	t.Cleanup(func() { provider.SetMockFailure(nil) })
	for _, field := range []string{"goal_capped", "goal_blocked", "goal_idle"} {
		t.Run(field, func(t *testing.T) {
			e := newTestEngine(t)
			th, err := e.CreateThread("paused answer failure", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := e.SetThreadGoal(th.ID, "deliver the agreed result"); err != nil {
				t.Fatal(err)
			}
			if err := e.Store().UpdateThread(th.ID, map[string]any{field: true, "goal_auto_turns": 3}); err != nil {
				t.Fatal(err)
			}
			before, _ := e.Store().GetThread(th.ID)
			turn, err := e.StartTurn(th.ID, "explain the recorded state")
			if err != nil {
				t.Fatal(err)
			}
			if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnError {
				t.Fatalf("the answer must retain the actual provider failure: %+v", got)
			}
			waitSettled(t, e, th.ID)
			after, _ := e.Store().GetThread(th.ID)
			if after.GoalCapped != before.GoalCapped || after.GoalBlocked != before.GoalBlocked || after.GoalIdle != before.GoalIdle || after.GoalAutoTurns != 3 {
				t.Fatalf("an answer failure must not replace the original pause: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestPausedGoalsCannotOwnNewBackgroundWorkers(t *testing.T) {
	for _, field := range []string{"goal_capped", "goal_blocked", "goal_idle"} {
		t.Run(field, func(t *testing.T) {
			e := newTestEngine(t)
			th, err := e.CreateThread("paused ownership", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := e.SetThreadGoal(th.ID, "deliver the agreed result"); err != nil {
				t.Fatal(err)
			}
			if err := e.Store().UpdateThread(th.ID, map[string]any{field: true}); err != nil {
				t.Fatal(err)
			}
			rt := e.runtimeFor(th.ID)
			if rt.shouldPark(store.TurnDone) {
				t.Fatal("a paused goal cannot authorize new background investigation")
			}
			if _, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID, Title: "Independent follow-up", Prompt: scheduleWaitPrompt, EveryS: 60, CreatedBy: store.ScheduleCreatedHuman}); err != nil {
				t.Fatal(err)
			}
			if !rt.shouldPark(store.TurnDone) {
				t.Fatal("an independently armed wake must retain its own worker authorization")
			}
		})
	}
}

func TestStoppingAPausedGoalAnswerPreservesItsPause(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })
	for _, field := range []string{"goal_capped", "goal_blocked", "goal_idle"} {
		t.Run(field, func(t *testing.T) {
			e := newTestEngine(t)
			th, err := e.CreateThread("paused answer stop", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := e.SetThreadGoal(th.ID, "deliver the agreed result"); err != nil {
				t.Fatal(err)
			}
			if err := e.Store().UpdateThread(th.ID, map[string]any{field: true, "goal_auto_turns": 3}); err != nil {
				t.Fatal(err)
			}
			before, _ := e.Store().GetThread(th.ID)
			turn, err := e.StartTurn(th.ID, "explain the recorded state")
			if err != nil {
				t.Fatal(err)
			}
			if err := e.Interrupt(th.ID); err != nil {
				t.Fatal(err)
			}
			if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnCancelled {
				t.Fatalf("Stop must still cancel this answer: %+v", got)
			}
			waitSettled(t, e, th.ID)
			after, _ := e.Store().GetThread(th.ID)
			if after.GoalCapped != before.GoalCapped || after.GoalBlocked != before.GoalBlocked || after.GoalIdle != before.GoalIdle || after.GoalAutoTurns != 3 {
				t.Fatalf("Stop must retain the pause that preceded this answer: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestAHumanMessageWarnsWhenItsGoalBudgetCannotBeReset(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })
	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 1
	logs := &followupWarningLog{}
	e.log = slog.New(slog.NewJSONHandler(logs, nil))
	th, err := e.CreateThread("budget storage failure", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadGoal(th.ID, "deliver the agreed result"); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{"goal_auto_turns": 3}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().DB().Exec(`CREATE TRIGGER refuse_goal_budget_reset BEFORE UPDATE OF goal_auto_turns ON threads WHEN NEW.goal_auto_turns = 0 BEGIN SELECT RAISE(ABORT,'budget storage unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	const request = "private additional constraint"
	turn, err := e.StartTurn(th.ID, request)
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	waitSettled(t, e, th.ID)
	got, err := e.Store().GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.GoalAutoTurns != 3 || !got.GoalCapped {
		t.Fatalf("a failed storage write cannot grant a fresh auto-turn budget: %+v", got)
	}
	for _, want := range []string{"WARN", "could not reset the goal auto-continue budget", th.ID, "budget storage unavailable"} {
		if text := logs.snapshot(); !strings.Contains(text, want) {
			t.Fatalf("missing storage warning %q in %s", want, text)
		}
	}
	if strings.Contains(logs.snapshot(), request) {
		t.Fatal("budget storage diagnostics must not copy private human input")
	}
}
