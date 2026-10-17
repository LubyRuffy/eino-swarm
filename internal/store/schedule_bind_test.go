package store

import (
	"errors"
	"sync"
	"testing"
)

func TestScheduledRunBindingOnlyFillsAnExactUnfinishedClaim(t *testing.T) {
	for _, boundary := range []string{"same", "empty-turn", "empty-thread", "empty-both", "other-turn", "other-thread", "terminal", "missing-run", "missing-turn", "wrong-source", "not-scheduled", "empty-id", "read-run", "read-turn", "write"} {
		t.Run(boundary, func(t *testing.T) {
			s, scope, _ := storedGoalTurn(t)
			turn, _ := s.GetTurn(scope.TurnID)
			threadID, turnID, runID := turn.ThreadID, turn.ID, turn.ScheduleRunID
			var err error
			switch boundary {
			case "same":
				err = s.DB().Exec(`CREATE TRIGGER refuse_rebinding BEFORE UPDATE ON schedule_runs BEGIN SELECT RAISE(ABORT,'already bound'); END`).Error
			case "empty-turn", "write":
				err = s.SetRunTurn(runID, "")
			case "empty-thread":
				err = s.DB().Exec("UPDATE schedule_runs SET thread_id = '' WHERE id = ?", runID).Error
			case "empty-both":
				err = s.DB().Exec("UPDATE schedule_runs SET thread_id = '', turn_id = '' WHERE id = ?", runID).Error
			case "other-turn":
				err = s.SetRunTurn(runID, "other-owner")
			case "other-thread":
				err = s.DB().Exec("UPDATE schedule_runs SET thread_id = 'other-owner' WHERE id = ?", runID).Error
			case "terminal":
				err = s.FinishRun(runID, ScheduleRunFindings, "verified findings", true)
			case "missing-run":
				err = s.DB().Exec("DELETE FROM schedule_runs WHERE id = ?", runID).Error
			case "missing-turn":
				turnID = "missing"
			case "wrong-source":
				runID = "another-run"
			case "not-scheduled":
				err = s.DB().Exec("UPDATE turns SET schedule_continue = false WHERE id = ?", turnID).Error
			case "empty-id":
				runID = ""
			case "read-run":
				err = s.DB().Migrator().DropTable(&ScheduleRun{})
			case "read-turn":
				err = s.DB().Migrator().DropTable(&Turn{})
			}
			if err != nil {
				t.Fatal(err)
			}
			if boundary == "write" {
				if err := s.DB().Exec(`CREATE TRIGGER refuse_binding BEFORE UPDATE ON schedule_runs BEGIN SELECT RAISE(ABORT,'binding rejected'); END`).Error; err != nil {
					t.Fatal(err)
				}
			}
			before, _ := s.GetRun(turn.ScheduleRunID)
			err = s.BindRunIfUnbound(threadID, turnID, runID)
			success := boundary == "same" || boundary == "empty-turn" || boundary == "empty-thread" || boundary == "empty-both"
			if (err == nil) != success {
				t.Fatalf("boundary %s returned %v", boundary, err)
			}
			if boundary == "other-turn" || boundary == "other-thread" || boundary == "terminal" || boundary == "empty-id" || boundary == "wrong-source" || boundary == "not-scheduled" || boundary == "missing-run" {
				if !errors.Is(err, ErrScheduleRunConflict) {
					t.Fatalf("conflict lost its category: %v", err)
				}
			}
			after, _ := s.GetRun(turn.ScheduleRunID)
			if success && (after.TurnID != scope.TurnID || after.ThreadID != scope.ThreadID || after.Status != ScheduleRunRunning) {
				t.Fatalf("binding is incomplete: %+v", after)
			}
			if !success && before != nil && after != nil && (before.TurnID != after.TurnID || before.ThreadID != after.ThreadID || before.Status != after.Status || before.Summary != after.Summary) {
				t.Fatalf("refused binding altered history: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestConcurrentTurnBindingsNeverStealTheFirstCommittedClaim(t *testing.T) {
	s, scope, _ := storedGoalTurn(t)
	turn, _ := s.GetTurn(scope.TurnID)
	if err := s.SetRunTurn(turn.ScheduleRunID, ""); err != nil {
		t.Fatal(err)
	}
	other := &Turn{ThreadID: scope.ThreadID, ScheduleContinue: true, ScheduleRunID: turn.ScheduleRunID}
	if err := s.CreateTurn(other); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, id := range []string{scope.TurnID, other.ID} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			<-start
			errs <- s.BindRunIfUnbound(scope.ThreadID, id, turn.ScheduleRunID)
		}(id)
	}
	close(start)
	wg.Wait()
	close(errs)
	winners := 0
	for err := range errs {
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrScheduleRunConflict) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("expected one binding winner, got %d", winners)
	}
}
