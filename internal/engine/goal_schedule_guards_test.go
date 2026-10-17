package engine

import (
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestNewWakeOwnershipSurvivesRestartWithoutGuessingTheLatestTurn(t *testing.T) {
	for _, origin := range []string{"scheduled", "human"} {
		t.Run(origin, func(t *testing.T) {
			e, thread, turn, _ := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleDone)
			if origin == "human" {
				turn = mustStoredTurn(t, e, thread, store.Turn{}).ID
			}
			out, err := e.scheduleWakeJSON(thread, turn, `{"prompt":"inspect remaining work","delay_s":60}`)
			if err != nil {
				t.Fatal(err)
			}
			requireGoalScheduleToolOK(t, out, true)
			rows, _ := e.Store().ListSchedules()
			found := false
			for _, row := range rows {
				if row.Status == store.ScheduleActive {
					found = true
					if row.ArmedTurnID != turn {
						t.Fatalf("new wait lost exact turn ownership: %+v", row)
					}
				}
			}
			if !found {
				t.Fatal("test did not arm a continuation")
			}
			e.Shutdown()
			_ = e.Store().Close()
			restarted := newTestEngineAt(t, e.cfg.DataDir())
			requireGoalScheduleToolOK(t, completeScheduledGoal(t, restarted, thread, turn), false)
		})
	}
}

func TestGoalToolScopeErrorsStayRecoverable(t *testing.T) {
	e, thread, turn, _ := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleDone)
	for _, tc := range []struct{ thread, turn string }{{thread, ""}, {thread, "missing"}, {"missing", turn}} {
		out, err := e.completeGoalForTurnJSON(tc.thread, tc.turn, "objective satisfied")
		if err != nil {
			t.Fatal(err)
		}
		requireGoalScheduleToolOK(t, out, false)
	}
	e.recordMu.Lock()
	e.droppedTurns[turn] = struct{}{}
	e.recordMu.Unlock()
	requireGoalScheduleToolOK(t, completeScheduledGoal(t, e, thread, turn), false)
	for _, tc := range []struct{ thread, turn string }{{thread, ""}, {"missing", turn}} {
		out, err := e.scheduleWakeJSON(tc.thread, tc.turn, `{"prompt":"inspect remaining work","delay_s":60}`)
		if err != nil {
			t.Fatal(err)
		}
		requireGoalScheduleToolOK(t, out, false)
	}
	if err := e.Store().DB().Exec("DELETE FROM threads WHERE id = ?", thread).Error; err != nil {
		t.Fatal(err)
	}
	out, err := e.reportScheduleJSON(thread, turn, `{"keep":false}`)
	if err != nil {
		t.Fatal(err)
	}
	requireGoalScheduleToolOK(t, out, false)
}

func TestStoppingAnAlreadyConsumedWakeStillAllowsGoalCompletion(t *testing.T) {
	e, thread, turn, _ := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleDone)
	out, err := e.reportScheduleJSON(thread, turn, `{"keep":false}`)
	if err != nil {
		t.Fatal(err)
	}
	requireGoalScheduleToolOK(t, out, true)
	requireGoalScheduleToolOK(t, completeScheduledGoal(t, e, thread, turn), true)
	if strings.Contains(out, "goal_schedule_conflict") {
		t.Fatalf("consumed wait was classified as continuing: %s", out)
	}
}

func TestGoalWakeReplacementRejectsMissingImpossibleOrUnwritableWaits(t *testing.T) {
	for _, fault := range []string{"missing", "impossible-cron", "write"} {
		t.Run(fault, func(t *testing.T) {
			e, thread, _, id := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleActive)
			in := ScheduleInput{Prompt: "inspect remaining work", EveryS: 60}
			if fault == "missing" {
				id = "missing"
			} else if fault == "impossible-cron" {
				in.EveryS, in.Cron = 0, "0 0 31 2 *"
			} else if err := e.Store().DB().Exec(`CREATE TRIGGER refuse_replacement BEFORE UPDATE ON schedules BEGIN SELECT RAISE(ABORT,'replacement rejected'); END`).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := e.replaceThreadWake(thread, id, in, nil); err == nil {
				t.Fatalf("%s replacement was accepted", fault)
			}
		})
	}
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if scope, err := e.wakeGoalScope(th.ID, ""); scope != nil || err != nil {
		t.Fatalf("legacy goal-free caller changed: %+v %v", scope, err)
	}
}
