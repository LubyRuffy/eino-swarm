package store

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// CapGoalAfterTurn commits an automatic-pursuit pause with its diagnostic.
// A human reset, changed objective or newer turn makes the old decision stale.
// The caller orders event publication with its other writers and broadcasts
// only a successful change. Failed inserts roll back the capped flag.
func (s *Store) CapGoalAfterTurn(scope GoalTurnScope, autoTurns int, ev *Event) (bool, error) {
	seq, err := s.nextEventSeq(scope.ThreadID)
	if err != nil {
		return false, fmt.Errorf("store: seed goal cap sequence: %w", err)
	}
	ev.ThreadID, ev.TurnID = scope.ThreadID, scope.TurnID
	ev.Seq, ev.CreatedAt = seq, time.Now().UTC()
	changed := false
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var th Thread
		if err := tx.First(&th, "id = ?", scope.ThreadID).Error; err != nil {
			return err
		}
		if !sameGoal(&th, scope) || th.Goal == "" || th.GoalComplete || th.GoalBlocked || th.GoalCapped || th.GoalIdle || th.GoalAutoTurns != autoTurns {
			return nil
		}
		var last Turn
		if err := tx.Where("thread_id = ?", scope.ThreadID).Order("seq desc").First(&last).Error; err != nil {
			return err
		}
		if last.ID != scope.TurnID {
			return nil
		}
		if err := tx.Model(&Thread{}).Where("id = ?", scope.ThreadID).Update("goal_capped", true).Error; err != nil {
			return err
		}
		if err := tx.Create(ev).Error; err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("store: cap standing objective after turn: %w", err)
	}
	return changed, nil
}
