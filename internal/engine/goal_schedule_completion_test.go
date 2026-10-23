package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/tool"
)

func goalScheduledTurn(t *testing.T, createdBy, kind, status string) (*Engine, string, string, string) {
	t.Helper()
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadGoal(th.ID, "finish the complete objective"); err != nil {
		t.Fatal(err)
	}
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID, OriginThreadID: th.ID,
		Title: "periodic check", Prompt: "inspect the remaining work", EveryS: 60, CreatedBy: createdBy,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{"status": status}); err != nil {
		t.Fatal(err)
	}
	run := &store.ScheduleRun{ScheduleID: sch.ID, ThreadID: th.ID, Kind: kind, Status: store.ScheduleRunRunning}
	if err := e.Store().CreateRun(run); err != nil {
		t.Fatal(err)
	}
	turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true, ScheduleRunID: run.ID})
	if err := e.Store().SetRunTurn(run.ID, turn.ID); err != nil {
		t.Fatal(err)
	}
	return e, th.ID, turn.ID, sch.ID
}

func completeScheduledGoal(t *testing.T, e *Engine, thread, turn string) string {
	t.Helper()
	tl := CompleteGoalTool(func(summary string) (string, error) {
		return e.completeGoalForTurnJSON(thread, turn, summary)
	}).(tool.InvokableTool)
	out, err := tl.InvokableRun(context.Background(), `{"summary":"objective satisfied"}`)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func requireGoalScheduleToolOK(t *testing.T, out string, want bool) {
	t.Helper()
	var body struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(out), &body); err != nil || body.OK != want {
		t.Fatalf("tool result = %s, parse error = %v; want ok=%v", out, err, want)
	}
}

func TestCompleteGoalRejectsAnArmedWakeFromItsScheduledTurn(t *testing.T) {
	e, thread, turn, schedule := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleActive)
	out, err := e.reportOwnedTestScheduleJSON(thread, turn, `{"findings":"partial progress","keep":true,"next_in_s":60}`)
	if err != nil {
		t.Fatal(err)
	}
	requireGoalScheduleToolOK(t, out, true)
	requireGoalScheduleToolOK(t, completeScheduledGoal(t, e, thread, turn), false)
	th, _ := e.Store().GetThread(thread)
	row, _ := e.Store().GetSchedule(schedule)
	if th.GoalComplete || row.Status != store.ScheduleActive || hasKind(t, e, thread, KindGoalComplete) {
		t.Fatalf("a refused completion changed goal/wait: goal=%+v wait=%+v", th, row)
	}
}

func TestCompletedGoalCannotRearmTheSameScheduledTurn(t *testing.T) {
	e, thread, turn, schedule := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleDone)
	requireGoalScheduleToolOK(t, completeScheduledGoal(t, e, thread, turn), true)
	out, err := e.reportOwnedTestScheduleJSON(thread, turn, `{"findings":"done","keep":true,"next_in_s":60}`)
	if err != nil {
		t.Fatal(err)
	}
	requireGoalScheduleToolOK(t, out, false)
	row, _ := e.Store().GetSchedule(schedule)
	if row.Status != store.ScheduleDone {
		t.Fatalf("late rearm resurrected the completed goal's wake: %+v", row)
	}
}

func TestCompletedGoalCannotScheduleAnotherWakeInTheSameTurn(t *testing.T) {
	e, thread, turn, _ := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleDone)
	requireGoalScheduleToolOK(t, completeScheduledGoal(t, e, thread, turn), true)
	out, err := e.scheduleWakeJSON(thread, turn, `{"prompt":"inspect remaining work","delay_s":60}`)
	if err != nil {
		t.Fatal(err)
	}
	requireGoalScheduleToolOK(t, out, false)
	rows, err := e.Store().ListSchedules()
	if err != nil || len(rows) != 1 || rows[0].Status != store.ScheduleDone {
		t.Fatalf("schedule_wake bypassed the same-turn terminal goal decision: %+v %v", rows, err)
	}
}

func TestScheduledTurnCannotCompleteAfterArmingAReplacementWake(t *testing.T) {
	e, thread, turn, oldID := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleDone)
	out, err := e.scheduleWakeJSON(thread, turn, `{"prompt":"inspect remaining work","delay_s":60}`)
	if err != nil {
		t.Fatal(err)
	}
	requireGoalScheduleToolOK(t, out, true)
	if strings.Contains(out, oldID) {
		t.Fatalf("one-shot must have created a different continuing wake: %s", out)
	}
	requireGoalScheduleToolOK(t, completeScheduledGoal(t, e, thread, turn), false)
	th, _ := e.Store().GetThread(thread)
	if th.GoalComplete || hasKind(t, e, thread, KindGoalComplete) {
		t.Fatal("new wait bypassed the scheduled turn's completion decision")
	}
}

func TestCompleteGoalLeavesIndependentWaitsAlone(t *testing.T) {
	for _, tc := range []struct{ name, creator, kind string }{
		{"human", store.ScheduleCreatedHuman, store.ScheduleThread},
		{"standalone-claimed", store.ScheduleCreatedManager, store.ScheduleStandalone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, thread, turn, schedule := goalScheduledTurn(t, tc.creator, tc.kind, store.ScheduleActive)
			requireGoalScheduleToolOK(t, completeScheduledGoal(t, e, thread, turn), true)
			th, _ := e.Store().GetThread(thread)
			row, _ := e.Store().GetSchedule(schedule)
			if !th.GoalComplete || row.Status != store.ScheduleActive {
				t.Fatalf("independent wait was cancelled or prevented completion: goal=%+v wait=%+v", th, row)
			}
		})
	}
}

func TestGoalCompletionRollsBackWhenItsTraceCannotBeStored(t *testing.T) {
	e, thread, turn, _ := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleDone)
	if err := e.Store().DB().Exec(`CREATE TRIGGER refuse_goal_completion BEFORE INSERT ON events
		WHEN NEW.kind = 'goal_complete' BEGIN SELECT RAISE(ABORT, 'completion trace rejected'); END`).Error; err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(thread)
	defer sub.Close()
	requireGoalScheduleToolOK(t, completeScheduledGoal(t, e, thread, turn), false)
	th, err := e.Store().GetThread(thread)
	if err != nil || th.GoalComplete || hasKind(t, e, thread, KindGoalComplete) {
		t.Fatalf("failed Trace insert changed the goal: %+v, %v", th, err)
	}
	select {
	case ev := <-sub.C:
		t.Fatalf("uncommitted goal completion was broadcast: %+v", ev)
	default:
	}
}

func TestOverdueScheduledCheckMustStillStopItsWakeBeforeCompleting(t *testing.T) {
	e, thread, turn, schedule := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleActive)
	if err := e.Store().UpdateSchedule(schedule, map[string]any{"next_run_at": time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	out := completeScheduledGoal(t, e, thread, turn)
	requireGoalScheduleToolOK(t, out, false)
	if !strings.Contains(out, `"code":"goal_schedule_conflict"`) {
		t.Fatalf("conflict lost its recoverable category: %s", out)
	}
	out, err := e.reportOwnedTestScheduleJSON(thread, turn, `{"keep":false,"findings":"objective satisfied"}`)
	if err != nil {
		t.Fatal(err)
	}
	requireGoalScheduleToolOK(t, out, true)
	requireGoalScheduleToolOK(t, completeScheduledGoal(t, e, thread, turn), true)
	row, _ := e.Store().GetSchedule(schedule)
	if row.Status != store.ScheduleCancelled {
		t.Fatalf("completion revived the stopped wake: %+v", row)
	}
}

func TestSameTurnCompletionCannotBeRearmedAfterEngineRestart(t *testing.T) {
	e, thread, turn, _ := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleDone)
	requireGoalScheduleToolOK(t, completeScheduledGoal(t, e, thread, turn), true)
	e.Shutdown()
	if err := e.Store().Close(); err != nil {
		t.Fatal(err)
	}
	restarted := newTestEngineAt(t, e.cfg.DataDir())
	for _, args := range []string{`{"keep":true}`, `{"next_in_s":60}`} {
		out, err := restarted.reportOwnedTestScheduleJSON(thread, turn, args)
		if err != nil {
			t.Fatal(err)
		}
		requireGoalScheduleToolOK(t, out, false)
	}
}

func TestGoalToolUsesItsExactTurnRatherThanTheNewestStoredTurn(t *testing.T) {
	e, thread, turn, _ := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleActive)
	newer := mustStoredTurn(t, e, thread, store.Turn{})
	requireGoalScheduleToolOK(t, completeScheduledGoal(t, e, thread, turn), false)
	requireGoalScheduleToolOK(t, completeScheduledGoal(t, e, thread, newer.ID), true)
	events, _ := e.Replay(thread, 0)
	for _, ev := range events {
		if ev.Kind == KindGoalComplete && ev.TurnID != newer.ID {
			t.Fatalf("completion attributed to another turn: %+v", ev)
		}
	}
}

func TestRetargetedWakeDoesNotCompleteOrCancelAnotherObjective(t *testing.T) {
	for _, kind := range []string{store.ScheduleThread, store.ScheduleStandalone} {
		t.Run(kind, func(t *testing.T) {
			e, thread, turn, schedule := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleActive)
			other, _ := e.CreateThread("", "", "")
			if _, err := e.PatchScheduleFields(schedule, ScheduleFields{Kind: &kind, ThreadID: &other.ID}); err != nil {
				t.Fatal(err)
			}
			requireGoalScheduleToolOK(t, completeScheduledGoal(t, e, thread, turn), true)
			row, _ := e.Store().GetSchedule(schedule)
			if row.Status != store.ScheduleActive || row.Kind != kind {
				t.Fatalf("unrelated destination was stopped: %+v", row)
			}
		})
	}
}

func TestOldScheduledTurnCannotRecadenceAHumanRetargetedWake(t *testing.T) {
	for _, kind := range []string{store.ScheduleThread, store.ScheduleStandalone} {
		t.Run(kind, func(t *testing.T) {
			e, thread, turn, schedule := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleActive)
			other, _ := e.CreateThread("", "", "")
			if _, err := e.PatchScheduleFields(schedule, ScheduleFields{Kind: &kind, ThreadID: &other.ID}); err != nil {
				t.Fatal(err)
			}
			before, _ := e.Store().GetSchedule(schedule)
			out, err := e.reportOwnedTestScheduleJSON(thread, turn, `{"next_in_s":120}`)
			if err != nil {
				t.Fatal(err)
			}
			requireGoalScheduleToolOK(t, out, false)
			after, _ := e.Store().GetSchedule(schedule)
			if before.EveryS != after.EveryS || !before.NextRunAt.Equal(after.NextRunAt) {
				t.Fatalf("old fire rewrote the human's new destination: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestOldScheduledTurnCannotCancelAHumanRetargetedWake(t *testing.T) {
	e, thread, turn, schedule := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleActive)
	other, _ := e.CreateThread("", "", "")
	kind := store.ScheduleThread
	if _, err := e.PatchScheduleFields(schedule, ScheduleFields{Kind: &kind, ThreadID: &other.ID}); err != nil {
		t.Fatal(err)
	}
	out, err := e.reportOwnedTestScheduleJSON(thread, turn, `{"keep":false}`)
	if err != nil {
		t.Fatal(err)
	}
	requireGoalScheduleToolOK(t, out, false)
	row, _ := e.Store().GetSchedule(schedule)
	if row.Status != store.ScheduleActive {
		t.Fatalf("old fire cancelled the human's new destination: %+v", row)
	}
}

func TestGoalToolsFailClosedOnMissingOrMisboundScheduledFire(t *testing.T) {
	for _, fault := range []string{"missing-run", "missing-row", "wrong-turn", "wrong-thread"} {
		t.Run(fault, func(t *testing.T) {
			e, thread, turn, schedule := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleDone)
			stored, _ := e.Store().GetTurn(turn)
			var err error
			switch fault {
			case "missing-run":
				err = e.Store().DB().Exec("DELETE FROM schedule_runs WHERE id = ?", stored.ScheduleRunID).Error
			case "missing-row":
				err = e.Store().DB().Exec("DELETE FROM schedules WHERE id = ?", schedule).Error
			case "wrong-turn":
				err = e.Store().SetRunTurn(stored.ScheduleRunID, "another-turn")
			case "wrong-thread":
				err = e.Store().DB().Exec("UPDATE schedule_runs SET thread_id = ? WHERE id = ?", "another-thread", stored.ScheduleRunID).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			requireGoalScheduleToolOK(t, completeScheduledGoal(t, e, thread, turn), false)
			for _, args := range []string{`{"next_in_s":60}`, `{"keep":false}`} {
				out, err := e.reportOwnedTestScheduleJSON(thread, turn, args)
				if err != nil {
					t.Fatal(err)
				}
				requireGoalScheduleToolOK(t, out, false)
			}
		})
	}
}

func TestCurrentTurnCanUseAnEditedOrExplicitlyReopenedObjective(t *testing.T) {
	e, thread, turn, schedule := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleDone)
	if err := e.EditThreadGoal(thread, "updated standing objective"); err != nil {
		t.Fatal(err)
	}
	requireGoalScheduleToolOK(t, completeScheduledGoal(t, e, thread, turn), true)
	if err := e.ReopenThreadGoal(thread, "further work remains"); err != nil {
		t.Fatal(err)
	}
	out, err := e.reportOwnedTestScheduleJSON(thread, turn, `{"next_in_s":60}`)
	if err != nil {
		t.Fatal(err)
	}
	requireGoalScheduleToolOK(t, out, true)
	row, _ := e.Store().GetSchedule(schedule)
	if row.Status != store.ScheduleActive {
		t.Fatalf("explicit recovery was blocked: %+v", row)
	}
}

func TestParallelScheduledCompletionAndRearmCannotBothCommit(t *testing.T) {
	for i := 0; i < 12; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			e, thread, turn, schedule := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleDone)
			start := make(chan struct{})
			var wg sync.WaitGroup
			var completion, rearm string
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				completion, _ = e.completeGoalForTurnJSON(thread, turn, "objective satisfied")
			}()
			go func() {
				defer wg.Done()
				<-start
				rearm, _ = e.reportOwnedTestScheduleJSON(thread, turn, `{"next_in_s":60}`)
			}()
			close(start)
			wg.Wait()
			th, _ := e.Store().GetThread(thread)
			row, _ := e.Store().GetSchedule(schedule)
			if th.GoalComplete && row.Status == store.ScheduleActive {
				t.Fatalf("both decisions committed: %s %s", completion, rearm)
			}
			if !strings.Contains(completion+rearm, `"code":"goal_schedule_conflict"`) {
				t.Fatalf("loser did not receive recoverable feedback: %s %s", completion, rearm)
			}
		})
	}
}
