package store

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ErrScheduleRunConflict means this turn cannot use the claimed fire. Cleanup
// must preserve a different owner's claim and an already-terminal run.
var ErrScheduleRunConflict = errors.New("store: scheduled fire is not runnable by this turn")

// BindRunIfUnbound is the shared pre-runtime gate for starting and restoring
// a scheduled turn. Its persisted source and claim are checked in one tx.
func (s *Store) BindRunIfUnbound(threadID, turnID, runID string) error {
	if runID == "" {
		return fmt.Errorf("%w: the turn has no persisted fire", ErrScheduleRunConflict)
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var turn Turn
		if err := tx.First(&turn, "id = ?", turnID).Error; err != nil {
			return fmt.Errorf("store: load scheduled turn: %w", err)
		}
		if turn.ThreadID != threadID || !turn.ScheduleContinue || turn.ScheduleRunID != runID {
			return fmt.Errorf("%w: the persisted turn does not name this fire", ErrScheduleRunConflict)
		}
		var run ScheduleRun
		if err := tx.First(&run, "id = ?", runID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: the claimed fire is missing", ErrScheduleRunConflict)
			}
			return fmt.Errorf("store: load claimed fire: %w", err)
		}
		if run.Status != ScheduleRunRunning || (run.ThreadID != "" && run.ThreadID != threadID) || (run.TurnID != "" && run.TurnID != turnID) {
			return fmt.Errorf("%w: the fire finished or belongs to another turn", ErrScheduleRunConflict)
		}
		if run.ThreadID == threadID && run.TurnID == turnID {
			return nil
		}
		if err := tx.Model(&ScheduleRun{}).Where("id = ?", runID).Updates(map[string]any{
			"thread_id": threadID, "turn_id": turnID, "updated_at": time.Now().UTC(),
		}).Error; err != nil {
			return fmt.Errorf("store: bind claimed fire: %w", err)
		}
		return nil
	})
}
