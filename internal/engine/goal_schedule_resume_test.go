package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestRestartBindsTheExactPersistedScheduledTurnBeforeItsFirstTool(t *testing.T) {
	provider.SetMockScheduleFindings(true)
	t.Cleanup(func() { provider.SetMockScheduleFindings(false) })
	e, thread, turnID, _ := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleActive)
	turn, _ := e.Store().GetTurn(turnID)
	if err := e.Store().SetRunTurn(turn.ScheduleRunID, ""); err != nil {
		t.Fatal(err)
	}
	e.Shutdown()
	_ = e.Store().Close()
	restarted := newTestEngineAt(t, e.cfg.DataDir())
	sub := restarted.Subscribe(thread)
	defer sub.Close()
	n, err := restarted.ResumeOrphanedTurns()
	if err != nil || n != 1 {
		t.Fatalf("resume failed: n=%d err=%v", n, err)
	}
	result := waitLive(t, sub, "tool_result", 5*time.Second)
	run, err := restarted.Store().GetRun(turn.ScheduleRunID)
	if err != nil || run.TurnID != turnID || run.ThreadID != thread || !strings.Contains(result.Text, `"ok":true`) {
		t.Fatalf("persisted crash window lost exact first-tool binding: run=%+v err=%v result=%+v", run, err, result)
	}
	waitForTurn(t, restarted, turnID)
}

func TestRestartNeverStealsAnotherTurnsScheduleClaim(t *testing.T) {
	for _, mismatch := range []string{"turn", "thread"} {
		t.Run(mismatch, func(t *testing.T) {
			e, thread, turnID, _ := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleActive)
			turn, _ := e.Store().GetTurn(turnID)
			field := "turn_id"
			if mismatch == "thread" {
				field = "thread_id"
			}
			if err := e.Store().DB().Model(&store.ScheduleRun{}).Where("id = ?", turn.ScheduleRunID).Update(field, "another-owner").Error; err != nil {
				t.Fatal(err)
			}
			before, _ := e.Store().GetRun(turn.ScheduleRunID)
			e.Shutdown()
			_ = e.Store().Close()
			restarted := newTestEngineAt(t, e.cfg.DataDir())
			n, err := restarted.ResumeOrphanedTurns()
			if err != nil || n != 0 || restarted.Status(thread).Running {
				t.Fatalf("conflicting claim resumed: n=%d err=%v", n, err)
			}
			after, _ := restarted.Store().GetRun(turn.ScheduleRunID)
			if after.TurnID != before.TurnID || after.ThreadID != before.ThreadID || after.Status != before.Status {
				t.Fatalf("resume altered another owner's claim: before=%+v after=%+v", before, after)
			}
			stored, _ := restarted.Store().GetTurn(turnID)
			if stored.Status != store.TurnError {
				t.Fatalf("failed resume left a running orphan: %+v", stored)
			}
		})
	}
}

func TestRestartDoesNotRepeatAFinishedScheduleWhoseTurnWriteFailed(t *testing.T) {
	provider.SetMockScheduleFindings(true)
	t.Cleanup(func() { provider.SetMockScheduleFindings(false) })
	e, thread, _, schedule := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleActive)
	if err := e.Store().DB().Exec("UPDATE schedule_runs SET status = ? WHERE schedule_id = ?", store.ScheduleRunFindings, schedule).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.Store().DB().Exec(`CREATE TRIGGER refuse_finished_turn BEFORE UPDATE OF status ON turns WHEN NEW.status='done' BEGIN SELECT RAISE(ABORT,'finish write rejected'); END`).Error; err != nil {
		t.Fatal(err)
	}
	turn, err := e.RunScheduleNow(schedule)
	if err != nil {
		t.Fatal(err)
	}
	before := waitForScheduleRun(t, e, turn.ScheduleRunID)
	if before.Status == store.ScheduleRunRunning {
		t.Fatal("fixture did not finish the scheduled run")
	}
	stored, _ := e.Store().GetTurn(turn.ID)
	if stored.Status != store.TurnRunning {
		t.Fatalf("fixture did not leave the interrupted turn write: %+v", stored)
	}
	if err := e.Store().DB().Exec("DROP TRIGGER refuse_finished_turn").Error; err != nil {
		t.Fatal(err)
	}
	e.Shutdown()
	_ = e.Store().Close()
	restarted := newTestEngineAt(t, e.cfg.DataDir())
	n, err := restarted.ResumeOrphanedTurns()
	if err != nil || n != 0 || restarted.Status(thread).Running {
		t.Fatalf("completed scheduled check was repeated: n=%d err=%v", n, err)
	}
	after, _ := restarted.Store().GetRun(turn.ScheduleRunID)
	if before.Status != after.Status || before.Summary != after.Summary || !before.EndedAt.Equal(*after.EndedAt) {
		t.Fatalf("completed run history changed: before=%+v after=%+v", before, after)
	}
}

func TestGoalScheduledResumeNeedsAPersistedClaimIdentity(t *testing.T) {
	e, thread, turn, _ := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleActive)
	if err := e.Store().DB().Model(&store.Turn{}).Where("id = ?", turn).Update("schedule_run_id", "").Error; err != nil {
		t.Fatal(err)
	}
	n, err := e.ResumeOrphanedTurns()
	if err != nil || n != 0 || e.Status(thread).Running {
		t.Fatalf("goal schedule resumed without a persisted claim: n=%d err=%v", n, err)
	}
}

func TestClaimedFireDoesNotOverwriteAClaimThatChangedOwnerBeforeStart(t *testing.T) {
	e, thread, _, schedule := goalScheduledTurn(t, store.ScheduleCreatedManager, store.ScheduleThread, store.ScheduleActive)
	if err := e.Store().DB().Exec("UPDATE schedule_runs SET status = ? WHERE schedule_id = ?", store.ScheduleRunFindings, schedule).Error; err != nil {
		t.Fatal(err)
	}
	claim, err := e.claimRunNow(schedule)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().SetRunTurn(claim.run.ID, "another-owner"); err != nil {
		t.Fatal(err)
	}
	before, _ := e.Store().GetRun(claim.run.ID)
	turn, err := e.startClaimedFire(*claim)
	if err == nil || turn != nil || e.Status(thread).Running {
		t.Fatalf("conflicting runtime started: %+v %v", turn, err)
	}
	after, _ := e.Store().GetRun(claim.run.ID)
	if before.TurnID != after.TurnID || before.ThreadID != after.ThreadID || before.Status != after.Status {
		t.Fatalf("startup cleanup overwrote another claim: before=%+v after=%+v", before, after)
	}
}
