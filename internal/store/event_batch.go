package store

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// AppendEventsAtomic commits related nonterminal receipts together. Completion
// events use AppendEvent, which also updates their attention watermark.
func (s *Store) AppendEventsAtomic(events []*Event) error {
	if len(events) == 0 {
		return fmt.Errorf("store: atomic receipts require at least one event")
	}
	for _, event := range events {
		if event == nil || primaryTerminal(event) || event.ThreadID == "" || event.TurnID == "" || event.ThreadID != events[0].ThreadID || event.TurnID != events[0].TurnID {
			return fmt.Errorf("store: atomic receipts require nonterminal events from one thread and turn")
		}
	}
	for _, event := range events {
		seq, err := s.nextEventSeq(event.ThreadID)
		if err != nil {
			return fmt.Errorf("store: seed atomic event sequence: %w", err)
		}
		event.Seq, event.CreatedAt = seq, time.Now().UTC()
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		for _, event := range events {
			if err := tx.Create(event).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("store: append atomic receipts: %w", err)
	}
	return nil
}
