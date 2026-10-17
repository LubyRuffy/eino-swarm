package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestGoalWakeTurnOwnerCommitsWithTheMutation(t *testing.T) {
	for _, operation := range []string{"create", "replace", "rearm"} {
		for _, reject := range []bool{false, true} {
			t.Run(operation+map[bool]string{false: "/commit", true: "/rollback"}[reject], func(t *testing.T) {
				s, scope, old := storedGoalTurn(t)
				row := old
				if operation == "create" {
					row = &Schedule{Kind: ScheduleThread, ThreadID: scope.ThreadID,
						CreatedBy: ScheduleCreatedManager, NextRunAt: time.Now().Add(time.Minute)}
				} else if operation == "replace" {
					if err := s.UpdateSchedule(row.ID, map[string]any{"status": ScheduleActive}); err != nil {
						t.Fatal(err)
					}
				}
				if reject {
					sql := `CREATE TRIGGER refuse_arming BEFORE UPDATE ON schedules BEGIN SELECT RAISE(ABORT,'arming rejected'); END`
					if operation == "create" {
						sql = `CREATE TRIGGER refuse_arming BEFORE INSERT ON schedules BEGIN SELECT RAISE(ABORT,'arming rejected'); END`
					}
					if err := s.DB().Exec(sql).Error; err != nil {
						t.Fatal(err)
					}
				}
				var err error
				switch operation {
				case "create":
					err = s.CreateScheduleForTurn(&scope, row, 5)
				case "replace":
					err = s.ReplaceThreadWakeForTurn(&scope, scope.ThreadID, row.ID, map[string]any{"prompt": "inspect remaining work"})
				case "rearm":
					err = s.RearmScheduleForTurn(&scope, row.ID, 5, map[string]any{"every_s": 60})
				}
				if reject {
					if err == nil {
						t.Fatal("injected mutation failure was accepted")
					}
					if operation != "create" {
						got, _ := s.GetSchedule(row.ID)
						if got.ArmedTurnID != "" {
							t.Fatalf("owner survived a rolled-back mutation: %+v", got)
						}
					}
					return
				}
				got, readErr := s.GetSchedule(row.ID)
				if err != nil || readErr != nil || got.ArmedTurnID != scope.TurnID {
					t.Fatalf("mutation lost exact owner: %+v %v %v", got, err, readErr)
				}
				wire, _ := json.Marshal(got)
				if strings.Contains(string(wire), "armed_turn_id") || strings.Contains(string(wire), scope.TurnID) {
					t.Fatalf("internal owner leaked onto the schedule API: %s", wire)
				}
				if _, err := s.CompleteGoalForTurn(scope, goalEvent(scope)); !errors.Is(err, ErrGoalScheduleConflict) {
					t.Fatalf("armed continuation lost completion exclusion: %v", err)
				}
			})
		}
	}
}

func TestCompletionOnlyOwnsTheCurrentTurnsManagerThreadWakes(t *testing.T) {
	for _, boundary := range []string{"current", "other-turn", "human", "standalone", "other-thread", "zero-next", "paused", "human-turn", "retargeted-original"} {
		t.Run(boundary, func(t *testing.T) {
			s, scope, old := storedGoalTurn(t)
			row := &Schedule{Kind: ScheduleThread, ThreadID: scope.ThreadID, CreatedBy: ScheduleCreatedManager,
				ArmedTurnID: scope.TurnID, Status: ScheduleActive, NextRunAt: time.Now().Add(time.Minute)}
			switch boundary {
			case "other-turn":
				row.ArmedTurnID = "another-turn"
			case "human":
				row.CreatedBy = ScheduleCreatedHuman
			case "standalone":
				row.Kind, row.ThreadID = ScheduleStandalone, ""
			case "other-thread":
				row.ThreadID = "another-thread"
			case "zero-next":
				row.NextRunAt = time.Time{}
			case "paused":
				row.Status = SchedulePaused
			case "human-turn":
				if err := s.DB().Exec("UPDATE turns SET schedule_continue = false WHERE id = ?", scope.TurnID).Error; err != nil {
					t.Fatal(err)
				}
			case "retargeted-original":
				if err := s.UpdateSchedule(old.ID, map[string]any{"kind": ScheduleStandalone, "thread_id": ""}); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.CreateSchedule(row); err != nil {
				t.Fatal(err)
			}
			changed, err := s.CompleteGoalForTurn(scope, goalEvent(scope))
			conflict := boundary == "current" || boundary == "human-turn" || boundary == "retargeted-original"
			if errors.Is(err, ErrGoalScheduleConflict) != conflict || changed == conflict || (err != nil && !conflict) {
				t.Fatalf("boundary %s: changed=%v err=%v", boundary, changed, err)
			}
		})
	}
}

func TestSameTurnTerminalGoalAlsoRejectsNewWakeFromAManualTurn(t *testing.T) {
	s, scope, _ := storedGoalTurn(t)
	if err := s.DB().Exec("UPDATE turns SET schedule_continue = false WHERE id = ?", scope.TurnID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteGoalForTurn(scope, goalEvent(scope)); err != nil {
		t.Fatal(err)
	}
	row := &Schedule{Kind: ScheduleThread, ThreadID: scope.ThreadID, CreatedBy: ScheduleCreatedManager}
	if err := s.CreateScheduleForTurn(&scope, row, 5); !errors.Is(err, ErrGoalScheduleConflict) {
		t.Fatalf("new wake bypassed manual turn completion: %v", err)
	}
}

func TestScopedWakeCancellationIsAtomicAndIdempotent(t *testing.T) {
	for _, fault := range []string{"", "done", "paused", "missing", "write", "wrong-scope", "retarget", "legacy-kind"} {
		t.Run(fault, func(t *testing.T) {
			s, scope, row := storedGoalTurn(t)
			if err := s.UpdateSchedule(row.ID, map[string]any{"status": ScheduleActive}); err != nil {
				t.Fatal(err)
			}
			var err error
			switch fault {
			case "done", "paused":
				err = s.UpdateSchedule(row.ID, map[string]any{"status": fault})
			case "missing":
				row.ID = "missing"
			case "write":
				err = s.DB().Exec(`CREATE TRIGGER refuse_cancel BEFORE UPDATE ON schedules BEGIN SELECT RAISE(ABORT,'cancel rejected'); END`).Error
			case "wrong-scope":
				scope.TurnID = "missing"
			case "retarget":
				err = s.UpdateSchedule(row.ID, map[string]any{"thread_id": "another-thread"})
			case "legacy-kind":
				turn, _ := s.GetTurn(scope.TurnID)
				err = s.DB().Exec("UPDATE schedule_runs SET kind = '' WHERE id = ?", turn.ScheduleRunID).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			changed, err := s.CancelScheduleForTurn(&scope, row.ID)
			wantError := fault == "missing" || fault == "write" || fault == "wrong-scope" || fault == "retarget"
			if (err != nil) != wantError {
				t.Fatalf("cancel error=%v want=%v", err, wantError)
			}
			if !wantError && fault != "done" && (changed == nil || changed.Status != ScheduleCancelled) {
				t.Fatalf("cancel did not persist: %+v", changed)
			}
		})
	}
}

func TestGoalDestinationCheckSurfacesMissingTurnOrClaim(t *testing.T) {
	s, scope, row := storedGoalTurn(t)
	for _, fault := range []string{"turn", "run"} {
		copyScope := scope
		copyRow := *row
		if fault == "turn" {
			copyScope.TurnID = "missing"
		} else {
			copyRow.ID = "another-row"
		}
		err := s.DB().Transaction(func(tx *gorm.DB) error { return goalWakeDestination(tx, &copyScope, &copyRow) })
		if err == nil {
			t.Fatalf("missing %s accepted by destination guard", fault)
		}
	}
	other := &Schedule{Kind: ScheduleThread, ThreadID: scope.ThreadID, CreatedBy: ScheduleCreatedManager, Status: ScheduleDone}
	if err := s.CreateSchedule(other); err != nil {
		t.Fatal(err)
	}
	if err := s.RearmScheduleForTurn(&scope, other.ID, 5, map[string]any{"every_s": 60}); err == nil {
		t.Fatal("a scheduled report recadenced a row that was never its claimed fire")
	}
}
