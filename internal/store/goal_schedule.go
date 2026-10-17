package store

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ErrGoalScheduleConflict is recoverable: stop this turn's continuing wake
// before completing its objective, or reopen the objective before rearming it.
var ErrGoalScheduleConflict = errors.New("store: the scheduled check cannot both complete its objective and continue its wake")

// GoalTurnScope binds a tool action to its exact turn and the objective read
// immediately before the action. Goal edits remain valid within a running turn;
// a concurrent edit after that read must not complete a different objective.
type GoalTurnScope struct {
	ThreadID      string
	TurnID        string
	Goal          string
	GoalStartedAt *time.Time
}

func sameGoal(th *Thread, scope GoalTurnScope) bool {
	if th.Goal != scope.Goal || (th.GoalStartedAt == nil) != (scope.GoalStartedAt == nil) {
		return false
	}
	return th.GoalStartedAt == nil || th.GoalStartedAt.Equal(*scope.GoalStartedAt)
}

// goalTurn reads only through tx: Store has one connection. In particular a
// nested GetRun/GetThread call would wait forever for this transaction.
func goalTurn(tx *gorm.DB, scope GoalTurnScope) (*Thread, *Schedule, error) {
	var th Thread
	if err := tx.First(&th, "id = ?", scope.ThreadID).Error; err != nil {
		return nil, nil, err
	}
	if !sameGoal(&th, scope) {
		return nil, nil, fmt.Errorf("store: the standing objective changed; read the current objective and retry")
	}
	if scope.TurnID == "" {
		return &th, nil, nil // Explicit non-tool completion has no claimed wake.
	}
	var turn Turn
	if err := tx.First(&turn, "id = ? AND thread_id = ?", scope.TurnID, scope.ThreadID).Error; err != nil {
		return nil, nil, err
	}
	if !turn.ScheduleContinue {
		return &th, nil, nil
	}
	if strings.TrimSpace(th.Goal) == "" {
		return &th, nil, nil
	}
	var run ScheduleRun
	if err := tx.First(&run, "id = ?", turn.ScheduleRunID).Error; err != nil {
		return nil, nil, err
	}
	if run.ThreadID != scope.ThreadID || run.TurnID != scope.TurnID {
		return nil, nil, fmt.Errorf("store: the scheduled fire does not own this turn")
	}
	var row Schedule
	if err := tx.First(&row, "id = ?", run.ScheduleID).Error; err != nil {
		return nil, nil, err
	}
	kind := run.Kind
	if kind == "" {
		kind = row.Kind // Retarget freezes pre-column fires before changing Kind.
	}
	if kind != ScheduleThread || row.Kind != ScheduleThread || row.ThreadID != scope.ThreadID || row.CreatedBy != ScheduleCreatedManager {
		return &th, nil, nil
	}
	return &th, &row, nil
}

func goalContinuation(tx *gorm.DB, scope *GoalTurnScope, newThreadWake bool) error {
	if scope == nil {
		return nil
	}
	th, ownWake, err := goalTurn(tx, *scope)
	if err != nil {
		return err
	}
	if (ownWake == nil && !newThreadWake) || !th.GoalComplete {
		return nil
	}
	var count int64
	q := tx.Model(&Event{}).Where("thread_id = ? AND turn_id = ? AND kind = ?", scope.ThreadID, scope.TurnID, "goal_complete")
	if th.GoalStartedAt != nil {
		q = q.Where("created_at >= ?", *th.GoalStartedAt)
	}
	if err := q.Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		return ErrGoalScheduleConflict
	}
	return nil
}

func goalWakeDestination(tx *gorm.DB, scope *GoalTurnScope, row *Schedule) error {
	if scope == nil || strings.TrimSpace(scope.Goal) == "" {
		return nil
	}
	var turn Turn
	if err := tx.First(&turn, "id = ? AND thread_id = ?", scope.TurnID, scope.ThreadID).Error; err != nil {
		return err
	}
	var run ScheduleRun
	if err := tx.First(&run, "id = ? AND schedule_id = ?", turn.ScheduleRunID, row.ID).Error; err != nil {
		return err
	}
	kind := run.Kind
	if kind == "" {
		kind = row.Kind
	}
	if kind != row.Kind || (kind == ScheduleThread && row.ThreadID != scope.ThreadID) {
		return fmt.Errorf("store: the wait's destination changed; report findings without changing that wait")
	}
	return nil
}

// CompleteGoalForTurn commits the flag and its same-turn evidence together.
// A rejected event insert rolls back the flag; the caller broadcasts only after
// success. Sequence allocation is outside tx and may leave a safe, unused gap.
func (s *Store) CompleteGoalForTurn(scope GoalTurnScope, ev *Event) (bool, error) {
	seq, err := s.nextEventSeq(scope.ThreadID)
	if err != nil {
		return false, fmt.Errorf("store: seed goal completion sequence: %w", err)
	}
	ev.Seq, ev.CreatedAt = seq, time.Now().UTC()
	changed := false
	err = s.db.Transaction(func(tx *gorm.DB) error {
		th, wake, err := goalTurn(tx, scope)
		if err != nil {
			return err
		}
		if strings.TrimSpace(th.Goal) == "" {
			return fmt.Errorf("store: there is no standing objective to complete")
		}
		if th.GoalComplete {
			return nil
		}
		// Overdue still means pending: a long check can outlive its cadence.
		if wake != nil && wake.Status == ScheduleActive && !wake.NextRunAt.IsZero() {
			return ErrGoalScheduleConflict
		}
		if scope.TurnID != "" {
			var n int64
			if err := tx.Model(&Schedule{}).Where("armed_turn_id = ? AND kind = ? AND thread_id = ? AND created_by = ? AND status = ? AND next_run_at != ?",
				scope.TurnID, ScheduleThread, scope.ThreadID, ScheduleCreatedManager, ScheduleActive, time.Time{}).Count(&n).Error; err != nil {
				return err
			}
			if n != 0 {
				return ErrGoalScheduleConflict
			}
		}
		if err := tx.Model(&Thread{}).Where("id = ?", th.ID).Updates(map[string]any{
			"goal_complete": true, "goal_capped": false, "goal_blocked": false,
			"goal_block_reason": "", "goal_idle": false, "updated_at": time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(ev).Error; err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("store: complete standing objective: %w", err)
	}
	return changed, nil
}

// CheckGoalContinuation handles an explicit keep:true without a cadence edit.
func (s *Store) CheckGoalContinuation(scope *GoalTurnScope, continuing bool) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if scope != nil && !continuing {
			_, _, err := goalTurn(tx, *scope)
			return err
		}
		return goalContinuation(tx, scope, false)
	})
}

// RearmScheduleForTurn checks the terminal decision and recadences the row in
// one transaction. A goal-scoped check cannot mutate a human-retargeted
// destination. Done one-shots still share the ordinary active ceiling.
func (s *Store) RearmScheduleForTurn(scope *GoalTurnScope, id string, cap int, fields map[string]any) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := goalContinuation(tx, scope, false); err != nil {
			return err
		}
		var row Schedule
		if err := tx.First(&row, "id = ?", id).Error; err != nil {
			return err
		}
		if err := goalWakeDestination(tx, scope, &row); err != nil {
			return err
		}
		if row.Status != ScheduleActive && row.Status != ScheduleDone {
			return fmt.Errorf("store: that wait is no longer active")
		}
		if row.Status == ScheduleDone {
			n, err := countActive(tx)
			if err != nil {
				return err
			}
			if n >= cap {
				return ErrScheduleCap
			}
		}
		fields["status"], fields["updated_at"] = ScheduleActive, time.Now().UTC()
		if scope != nil {
			fields["armed_turn_id"] = scope.TurnID
		}
		return tx.Model(&Schedule{}).Where("id = ?", id).Updates(fields).Error
	})
}

// CreateScheduleForTurn is the manager wake path. The ordinary public create
// remains independent of a conversation's standing objective.
func (s *Store) CreateScheduleForTurn(scope *GoalTurnScope, row *Schedule, cap int) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := goalContinuation(tx, scope, row.Kind == ScheduleThread && row.CreatedBy == ScheduleCreatedManager); err != nil {
			return err
		}
		n, err := countActive(tx)
		if err != nil {
			return err
		}
		if n >= cap {
			return ErrScheduleCap
		}
		if scope != nil {
			row.ArmedTurnID = scope.TurnID
		}
		return insertSchedule(tx, row)
	})
}

// ReplaceThreadWakeForTurn validates the current destination again: the
// engine's earlier lookup is not authority after a human retarget or stop.
func (s *Store) ReplaceThreadWakeForTurn(scope *GoalTurnScope, threadID, id string, fields map[string]any) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var row Schedule
		if err := tx.First(&row, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if row.Kind != ScheduleThread || row.ThreadID != threadID {
			return fmt.Errorf("store: that wait is not a wake on this conversation")
		}
		if err := goalContinuation(tx, scope, row.CreatedBy == ScheduleCreatedManager); err != nil {
			return err
		}
		if row.Status != ScheduleActive && row.Status != SchedulePaused {
			return fmt.Errorf("store: that wait is no longer active")
		}
		fields["updated_at"] = time.Now().UTC()
		if scope != nil {
			fields["armed_turn_id"] = scope.TurnID
		}
		return tx.Model(&Schedule{}).Where("id = ?", id).Updates(fields).Error
	})
}

// CancelScheduleForTurn is report_schedule keep:false. Its destination check
// and status write share a transaction with human retargeting.
func (s *Store) CancelScheduleForTurn(scope *GoalTurnScope, id string) (*Schedule, error) {
	var changed *Schedule
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if scope != nil {
			if _, _, err := goalTurn(tx, *scope); err != nil {
				return err
			}
		}
		var row Schedule
		if err := tx.First(&row, "id = ?", id).Error; err != nil {
			return err
		}
		if err := goalWakeDestination(tx, scope, &row); err != nil {
			return err
		}
		if row.Status != ScheduleActive && row.Status != SchedulePaused {
			return nil
		}
		if err := tx.Model(&Schedule{}).Where("id = ?", id).Updates(map[string]any{
			"status": ScheduleCancelled, "updated_at": time.Now().UTC(),
		}).Error; err != nil {
			return err
		}
		row.Status = ScheduleCancelled
		changed = &row
		return nil
	})
	return changed, err
}
