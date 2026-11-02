package store

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// CancelScheduleByHuman atomically records both the cancelled thread wait and
// the human's withdrawal of permission to park that conversation. The caller
// orders this event with its other writers and broadcasts only a new human
// action. A consumed one-shot or a manager cancellation is not a human receipt.
func (s *Store) CancelScheduleByHuman(id string, event *Event) (*Schedule, error) {
	if event == nil || event.ThreadID == "" || event.Kind != "schedule_cancelled" || event.Role != "human" || event.Text != id || event.SourceThreadID != "" || event.SourceTurnID != "" {
		return nil, fmt.Errorf("store: invalid human schedule cancellation receipt")
	}
	// Seed outside the transaction: a cold sequence lookup uses the store's
	// single connection and cannot run while that connection owns a transaction.
	seq, err := s.nextEventSeq(event.ThreadID)
	if err != nil {
		return nil, fmt.Errorf("store: seed human cancellation sequence: %w", err)
	}
	event.Seq, event.CreatedAt = seq, time.Now().UTC()
	var changed *Schedule
	err = s.db.Transaction(func(tx *gorm.DB) error {
		var row Schedule
		if err := tx.First(&row, "id = ?", id).Error; err != nil {
			return err
		}
		if row.Kind != ScheduleThread || row.ThreadID != event.ThreadID {
			return fmt.Errorf("store: wait destination changed before human cancellation")
		}
		var prior int64
		if err := tx.Model(&Event{}).Where("thread_id = ? AND kind = ? AND role = ? AND text = ? AND COALESCE(source_thread_id, '') = '' AND COALESCE(source_turn_id, '') = ''",
			row.ThreadID, "schedule_cancelled", "human", id).Count(&prior).Error; err != nil {
			return err
		}
		// The same DELETE may arrive again after a later explicit allowance.
		// Preserve that allowance by recognizing the original action, rather
		// than deriving consent from the schedule's current terminal status.
		if prior > 0 {
			return nil
		}
		var thread Thread
		if err := tx.Select("id").First(&thread, "id = ?", row.ThreadID).Error; err != nil {
			return err
		}
		if err := tx.Model(&Schedule{}).Where("id = ?", id).Updates(map[string]any{
			"status": ScheduleCancelled, "updated_at": event.CreatedAt,
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(event).Error; err != nil {
			return err
		}
		row.Status, row.UpdatedAt = ScheduleCancelled, event.CreatedAt
		changed = &row
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: cancel wait by human: %w", err)
	}
	return changed, nil
}
