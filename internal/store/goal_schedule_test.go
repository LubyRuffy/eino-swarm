package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func storedGoalTurn(t *testing.T) (*Store, GoalTurnScope, *Schedule) {
	t.Helper()
	s := open(t)
	now := time.Now().UTC()
	th := &Thread{Goal: "satisfy the standing objective", GoalStartedAt: &now}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	row := &Schedule{Kind: ScheduleThread, ThreadID: th.ID, CreatedBy: ScheduleCreatedManager,
		Status: ScheduleDone, NextRunAt: now.Add(time.Minute)}
	if err := s.CreateSchedule(row); err != nil {
		t.Fatal(err)
	}
	run := &ScheduleRun{ScheduleID: row.ID, ThreadID: th.ID, Kind: ScheduleThread}
	if err := s.CreateRun(run); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: th.ID, ScheduleContinue: true, ScheduleRunID: run.ID}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRunTurn(run.ID, turn.ID); err != nil {
		t.Fatal(err)
	}
	return s, GoalTurnScope{ThreadID: th.ID, TurnID: turn.ID, Goal: th.Goal, GoalStartedAt: &now}, row
}

func goalEvent(scope GoalTurnScope) *Event {
	return &Event{ThreadID: scope.ThreadID, TurnID: scope.TurnID, Kind: "goal_complete", AgentID: "manager"}
}

func TestGoalCompletionAndTraceCommitTogether(t *testing.T) {
	for _, failure := range []string{"", "event", "flags"} {
		t.Run(failure, func(t *testing.T) {
			s, scope, row := storedGoalTurn(t)
			if failure != "" {
				statement := `CREATE TRIGGER reject_completion BEFORE INSERT ON events WHEN NEW.kind='goal_complete' BEGIN SELECT RAISE(ABORT,'injected event failure'); END`
				if failure == "flags" {
					statement = `CREATE TRIGGER reject_completion BEFORE UPDATE OF goal_complete ON threads BEGIN SELECT RAISE(ABORT,'injected flag failure'); END`
				}
				if err := s.DB().Exec(statement).Error; err != nil {
					t.Fatal(err)
				}
			}
			ev := goalEvent(scope)
			changed, err := s.CompleteGoalForTurn(scope, ev)
			th, _ := s.GetThread(scope.ThreadID)
			events, _ := s.ListEvents(scope.ThreadID, 0, 0)
			if failure != "" {
				if err == nil || changed || th.GoalComplete || len(events) != 0 {
					t.Fatalf("partial completion persisted: changed=%v err=%v thread=%+v events=%+v", changed, err, th, events)
				}
				return
			}
			if err != nil || !changed || !th.GoalComplete || len(events) != 1 || ev.Seq == 0 || ev.CreatedAt.IsZero() {
				t.Fatalf("completion not atomic: %v %v %+v %+v", changed, err, th, events)
			}
			if changed, err := s.CompleteGoalForTurn(scope, goalEvent(scope)); changed || err != nil {
				t.Fatalf("repeat completion: %v %v", changed, err)
			}
			for _, action := range []func() error{
				func() error { return s.CheckGoalContinuation(&scope, true) },
				func() error { return s.RearmScheduleForTurn(&scope, row.ID, 10, map[string]any{"every_s": 60}) },
				func() error { return s.CreateScheduleForTurn(&scope, &Schedule{}, 10) },
				func() error {
					return s.ReplaceThreadWakeForTurn(&scope, scope.ThreadID, row.ID, map[string]any{"prompt": "continue"})
				},
			} {
				if err := action(); !errors.Is(err, ErrGoalScheduleConflict) {
					t.Fatalf("same-turn completion was not durable authority: %v", err)
				}
			}
			if err := s.CheckGoalContinuation(&scope, false); err != nil {
				t.Fatalf("stopping after completion is valid: %v", err)
			}
		})
	}
}

func TestGoalCompletionGuardChecksCurrentGoalAndClaim(t *testing.T) {
	for _, fault := range []string{"goal-text", "goal-start", "missing-thread", "missing-turn", "missing-run", "missing-schedule", "wrong-turn", "wrong-thread", "active", "no-goal", "seed"} {
		t.Run(fault, func(t *testing.T) {
			s, scope, row := storedGoalTurn(t)
			turn, _ := s.GetTurn(scope.TurnID)
			var err error
			switch fault {
			case "goal-text":
				err = s.UpdateThread(scope.ThreadID, map[string]any{"goal": "a replacement objective"})
			case "goal-start":
				err = s.UpdateThread(scope.ThreadID, map[string]any{"goal_started_at": nil})
			case "missing-thread":
				scope.ThreadID = "missing"
			case "missing-turn":
				scope.TurnID = "missing"
			case "missing-run":
				err = s.DB().Exec("DELETE FROM schedule_runs WHERE id = ?", turn.ScheduleRunID).Error
			case "missing-schedule":
				err = s.DB().Exec("DELETE FROM schedules WHERE id = ?", row.ID).Error
			case "wrong-turn":
				err = s.SetRunTurn(turn.ScheduleRunID, "another-turn")
			case "wrong-thread":
				err = s.DB().Exec("UPDATE schedule_runs SET thread_id = 'another-thread' WHERE id = ?", turn.ScheduleRunID).Error
			case "active":
				err = s.UpdateSchedule(row.ID, map[string]any{"status": ScheduleActive, "next_run_at": time.Now().Add(-time.Minute)})
			case "no-goal":
				scope.Goal, scope.GoalStartedAt = "", nil
				err = s.UpdateThread(scope.ThreadID, map[string]any{"goal": "", "goal_started_at": nil})
			case "seed":
				err = s.DB().Migrator().DropTable(&Event{})
			}
			if err != nil {
				t.Fatal(err)
			}
			changed, err := s.CompleteGoalForTurn(scope, goalEvent(scope))
			if changed || err == nil {
				t.Fatalf("invalid completion accepted: %v %v", changed, err)
			}
			if fault == "active" && !errors.Is(err, ErrGoalScheduleConflict) {
				t.Fatalf("overdue wake lost its typed conflict: %v", err)
			}
		})
	}
}

func TestGoalContinuationKeepsIndependentAndNewObjectivesCompatible(t *testing.T) {
	for _, boundary := range []string{"public", "human-turn", "standalone-claim", "legacy-thread", "human-wake", "retarget-thread", "retarget-standalone", "new-goal", "reopened", "older-turn", "no-start-time"} {
		t.Run(boundary, func(t *testing.T) {
			s, scope, row := storedGoalTurn(t)
			turn, _ := s.GetTurn(scope.TurnID)
			var err error
			switch boundary {
			case "public":
				scope.TurnID = ""
			case "human-turn":
				err = s.DB().Exec("UPDATE turns SET schedule_continue = false WHERE id = ?", scope.TurnID).Error
			case "standalone-claim":
				err = s.DB().Exec("UPDATE schedule_runs SET kind = ? WHERE id = ?", ScheduleStandalone, turn.ScheduleRunID).Error
			case "legacy-thread":
				err = s.DB().Exec("UPDATE schedule_runs SET kind = '' WHERE id = ?", turn.ScheduleRunID).Error
			case "human-wake":
				err = s.UpdateSchedule(row.ID, map[string]any{"created_by": ScheduleCreatedHuman})
			case "retarget-thread":
				err = s.UpdateSchedule(row.ID, map[string]any{"thread_id": "another-thread"})
			case "retarget-standalone":
				err = s.UpdateSchedule(row.ID, map[string]any{"kind": ScheduleStandalone, "thread_id": ""})
			case "no-start-time":
				scope.GoalStartedAt = nil
				err = s.UpdateThread(scope.ThreadID, map[string]any{"goal_started_at": nil})
			}
			if err != nil {
				t.Fatal(err)
			}
			if changed, err := s.CompleteGoalForTurn(scope, goalEvent(scope)); err != nil || !changed {
				t.Fatalf("compatible completion failed: %v %v", changed, err)
			}
			switch boundary {
			case "new-goal":
				now := time.Now().UTC().Add(time.Second)
				scope.GoalStartedAt = &now
				err = s.UpdateThread(scope.ThreadID, map[string]any{"goal_started_at": now})
			case "reopened":
				err = s.UpdateThread(scope.ThreadID, map[string]any{"goal_complete": false})
			case "older-turn":
				later := &Turn{ThreadID: scope.ThreadID, ScheduleContinue: true, ScheduleRunID: turn.ScheduleRunID}
				if err = s.CreateTurn(later); err == nil {
					scope.TurnID = later.ID
					err = s.SetRunTurn(turn.ScheduleRunID, later.ID)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			err = s.CheckGoalContinuation(&scope, true)
			wantConflict := boundary == "legacy-thread" || boundary == "no-start-time"
			if wantConflict != errors.Is(err, ErrGoalScheduleConflict) || (err != nil && !wantConflict) {
				t.Fatalf("boundary %s changed: %v", boundary, err)
			}
		})
	}
}

func TestGoalContinuationMutationsRetainCapsStopsAndStoreErrors(t *testing.T) {
	for _, action := range []string{"rearm", "create", "replace", "check"} {
		for _, fault := range []string{"", "missing", "cancelled", "cap", "write", "read"} {
			t.Run(action+"/"+fault, func(t *testing.T) {
				s, scope, row := storedGoalTurn(t)
				id, cap := row.ID, 5
				if action == "replace" {
					if err := s.UpdateSchedule(id, map[string]any{"status": ScheduleActive}); err != nil {
						t.Fatal(err)
					}
				}
				var err error
				switch fault {
				case "missing":
					id = "missing"
				case "cancelled":
					err = s.UpdateSchedule(row.ID, map[string]any{"status": ScheduleCancelled})
				case "cap":
					cap = 0
				case "write":
					err = s.DB().Exec(`CREATE TRIGGER fail_update BEFORE UPDATE ON schedules BEGIN SELECT RAISE(ABORT,'injected'); END`).Error
					if action == "create" {
						err = s.DB().Exec(`CREATE TRIGGER fail_create BEFORE INSERT ON schedules BEGIN SELECT RAISE(ABORT,'injected'); END`).Error
					}
				case "read":
					err = s.DB().Migrator().DropTable(&Schedule{})
				}
				if err != nil {
					t.Fatal(err)
				}
				patch := map[string]any{"prompt": "inspect remaining work"}
				switch action {
				case "rearm":
					err = s.RearmScheduleForTurn(nil, id, cap, patch)
				case "create":
					err = s.CreateScheduleForTurn(nil, &Schedule{ThreadID: scope.ThreadID}, cap)
				case "replace":
					err = s.ReplaceThreadWakeForTurn(nil, scope.ThreadID, id, patch)
				case "check":
					err = s.CheckGoalContinuation(&scope, true)
				}
				wantError := fault == "read" || (action != "check" && fault == "write") ||
					((action == "rearm" || action == "replace") && (fault == "missing" || fault == "cancelled")) ||
					((action == "create" || action == "rearm") && fault == "cap")
				if (err != nil) != wantError {
					t.Fatalf("error=%v, want error=%v", err, wantError)
				}
				if fault == "write" && err != nil && !strings.Contains(err.Error(), "injected") {
					t.Fatalf("storage error lost cause: %v", err)
				}
			})
		}
	}
}
