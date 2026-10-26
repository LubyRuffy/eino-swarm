package engine

import (
	"fmt"
	"testing"
)

// A failed response to a human question cannot grant another objective slice
// or replace an existing pause, even when its retry evidence was not stored.
func TestPausedGoalHumanAnswerScheduleFailurePreservesItsPause(t *testing.T) {
	for _, state := range []string{"capped", "blocked", "idle"} {
		for _, loseTerminal := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/terminal_missing=%v", state, loseTerminal), func(t *testing.T) {
				e, manager, checks := newScheduleAnswerFixture(t, "no-call", false)
				e.cfg.Swarm.GoalMaxAutoTurns = 12
				thread, err := e.CreateThread("paused schedule answer", "", "")
				if err != nil {
					t.Fatal(err)
				}
				if err := e.SetThreadGoal(thread.ID, "deliver the agreed result"); err != nil {
					t.Fatal(err)
				}
				if err := e.store.UpdateThread(thread.ID, map[string]any{
					"goal_capped": state == "capped", "goal_blocked": state == "blocked", "goal_idle": state == "idle",
					"goal_block_reason": "retained pause details", "goal_auto_turns": 7,
				}); err != nil {
					t.Fatal(err)
				}
				if loseTerminal {
					if err := e.store.DB().Exec(`CREATE TRIGGER refuse_paused_answer_terminal BEFORE INSERT ON events WHEN NEW.kind = 'error' BEGIN SELECT RAISE(ABORT, 'terminal receipt unavailable'); END`).Error; err != nil {
						t.Fatal(err)
					}
				}
				turn, err := e.StartTurn(thread.ID, "Explain the recorded status.")
				if err != nil {
					t.Fatal(err)
				}
				finished := waitForTurn(t, e, turn.ID)
				waitSettled(t, e, thread.ID)
				if !isStoredScheduleClaimMismatch(finished) || finished.Final != "" {
					t.Fatalf("the rejected answer lost its actual terminal error: %+v", finished)
				}
				got, err := e.store.GetThread(thread.ID)
				if err != nil {
					t.Fatal(err)
				}
				if got.GoalCapped != (state == "capped") || got.GoalBlocked != (state == "blocked") || got.GoalIdle != (state == "idle") || got.GoalBlockReason != "retained pause details" || got.GoalAutoTurns != 7 {
					t.Fatalf("a failed human answer changed the goal pause: %+v", got)
				}
				turns, err := e.store.ListTurns(thread.ID)
				if err != nil {
					t.Fatal(err)
				}
				if len(turns) != 1 || manager.Load() != 2 || checks.Load() != 2 {
					t.Fatalf("paused human failure started more objective work: turns=%d manager=%d checks=%d", len(turns), manager.Load(), checks.Load())
				}
				if e.allowFailedScheduleClaimGoalRetry(finished) {
					t.Fatal("a paused human answer was granted a goal retry allowance")
				}
				// Snapshot-based restored callers must apply the same boundary.
				if e.allowScheduleClaimGoalRetry(got, finished) {
					t.Fatal("a paused snapshot was granted a goal retry allowance")
				}
			})
		}
	}
}
