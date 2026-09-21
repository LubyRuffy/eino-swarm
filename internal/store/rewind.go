package store

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// TruncateFromEventSeq drops the named event and everything after it: later
// turns, their model messages, their model-call records, and any queued
// follow-up. Earlier turns stay. Sequence counters are not wound back, so a
// live subscriber's Last-Event-ID still lands on new events instead of
// colliding with a seq that just vanished.
func (s *Store) TruncateFromEventSeq(threadID string, fromSeq int64) error {
	if fromSeq <= 0 {
		return ErrNotFound
	}
	if _, err := s.GetThread(threadID); err != nil {
		return err
	}
	ev, err := s.GetEvent(threadID, fromSeq)
	if err != nil {
		return err
	}
	turn, err := s.GetTurn(ev.TurnID)
	if err != nil {
		return err
	}

	err = s.db.Transaction(func(tx *gorm.DB) error {
		var later []Turn
		if err := tx.Where("thread_id = ? AND seq >= ?", threadID, turn.Seq).Find(&later).Error; err != nil {
			return err
		}
		ids := make([]string, 0, len(later))
		for _, t := range later {
			ids = append(ids, t.ID)
		}
		if err := tx.Where("thread_id = ? AND seq >= ?", threadID, fromSeq).Delete(&Event{}).Error; err != nil {
			return err
		}
		if len(ids) > 0 {
			if err := tx.Where("thread_id = ? AND turn_id IN ?", threadID, ids).Delete(&Message{}).Error; err != nil {
				return err
			}
			if err := tx.Where("thread_id = ? AND turn_id IN ?", threadID, ids).Delete(&LLMCall{}).Error; err != nil {
				return err
			}
			if err := tx.Where("thread_id = ? AND id IN ?", threadID, ids).Delete(&Turn{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("thread_id = ?", threadID).Delete(&Followup{}).Error; err != nil {
			return err
		}
		return s.clearBrokenCompact(tx, threadID)
	})
	if err != nil {
		return fmt.Errorf("store: truncate from seq %d: %w", fromSeq, err)
	}
	s.mu.Lock()
	delete(s.followupSeq, threadID)
	s.mu.Unlock()
	return s.IndexThread(threadID)
}

// GetEvent loads one timeline row by its per-conversation sequence number.
func (s *Store) GetEvent(threadID string, seq int64) (*Event, error) {
	var e Event
	err := s.db.Where("thread_id = ? AND seq = ?", threadID, seq).First(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get event: %w", err)
	}
	return &e, nil
}

func (s *Store) clearBrokenCompact(tx *gorm.DB, threadID string) error {
	var th Thread
	if err := tx.First(&th, "id = ?", threadID).Error; err != nil {
		return err
	}
	if th.CompactThroughSeq > 0 && th.CompactSummary != "" {
		var n int64
		if err := tx.Model(&Message{}).
			Where("thread_id = ? AND seq = ?", threadID, th.CompactThroughSeq).
			Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			if err := tx.Model(&Thread{}).Where("id = ?", threadID).Updates(map[string]any{
				"compact_summary":     "",
				"compact_through_seq": 0,
			}).Error; err != nil {
				return err
			}
		}
	}
	return s.clearBrokenSessionMemory(tx, threadID, &th)
}

func (s *Store) clearBrokenSessionMemory(tx *gorm.DB, threadID string, th *Thread) error {
	if th.SessionMemoryThroughSeq <= 0 || strings.TrimSpace(th.SessionMemory) == "" {
		return nil
	}
	var n int64
	if err := tx.Model(&Event{}).
		Where("thread_id = ? AND seq = ?", threadID, th.SessionMemoryThroughSeq).
		Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	return tx.Model(&Thread{}).Where("id = ?", threadID).Updates(map[string]any{
		"session_memory":             "",
		"session_memory_through_seq": 0,
		"session_memory_tokens":      0,
	}).Error
}
