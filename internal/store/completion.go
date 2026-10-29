package store

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

var ErrInvalidCompletionSeq = errors.New("store: invalid completion sequence")

func primaryTerminal(e *Event) bool {
	return (e.Kind == "done" || e.Kind == "error") &&
		(e.AgentID == "" || e.AgentID == "manager") && (e.Role == "" || e.Role == "manager")
}

func (s *Store) nextEventSeq(threadID string) (int64, error) {
	s.mu.Lock()
	cur, ok := s.evtSeq[threadID]
	s.mu.Unlock()
	if !ok {
		// The durable floor includes quiet/cancelled terminal observations, so
		// neither a rewind nor a restart can give their old ACK a new identity.
		if err := s.db.Raw(`SELECT MAX(
			COALESCE((SELECT MAX(seq) FROM events WHERE thread_id = ?), 0),
			COALESCE((SELECT terminal_seq_high_water FROM threads WHERE id = ?), 0))`,
			threadID, threadID).Scan(&cur).Error; err != nil {
			return 0, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, raced := s.evtSeq[threadID]; raced {
		cur = existing
	}
	cur++
	s.evtSeq[threadID] = cur
	return cur, nil
}

// The terminal event and its attention watermark commit before broadcasting.
// A reader can acknowledge the event even while FinishTurn is still pending.
func (s *Store) appendCompletionEvent(db *gorm.DB, e *Event) error {
	e.Completion = false
	if !primaryTerminal(e) {
		return db.Create(e).Error
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if !e.SuppressCompletion && e.TurnID != "" {
			var turn Turn
			err := tx.First(&turn, "id = ? AND thread_id = ?", e.TurnID, e.ThreadID).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			e.Completion = err == nil && !turn.Quiet && turn.Status != TurnCancelled
		}
		if err := tx.Create(e).Error; err != nil {
			return err
		}
		fields := map[string]any{"terminal_seq_high_water": gorm.Expr("MAX(terminal_seq_high_water, ?)", e.Seq)}
		if e.Completion {
			fields["completed_seq"] = gorm.Expr("MAX(completed_seq, ?)", e.Seq)
		}
		return tx.Model(&Thread{}).Where("id = ?", e.ThreadID).Updates(fields).Error
	})
}

// MarkThreadRead confirms one observed terminal event, never the latest answer
// at request processing time. Delayed/retried reads cannot consume newer work.
func (s *Store) MarkThreadRead(threadID string, seq int64) error {
	if seq <= 0 {
		return ErrInvalidCompletionSeq
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var th Thread
		if err := tx.First(&th, "id = ?", threadID).Error; err != nil {
			return err
		}
		var ev Event
		if err := tx.Where("thread_id = ? AND seq = ?", threadID, seq).First(&ev).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInvalidCompletionSeq
			}
			return err
		}
		if !primaryTerminal(&ev) {
			return ErrInvalidCompletionSeq
		}
		// Cancelled, quiet and pre-upgrade terminal events are valid observations
		// but do not acknowledge another completion's watermark.
		if !ev.Completion || seq <= th.ReadCompletedSeq {
			return nil
		}
		if seq > th.CompletedSeq {
			return ErrInvalidCompletionSeq
		}
		return tx.Model(&Thread{}).Where("id = ? AND read_completed_seq < ?", threadID, seq).
			UpdateColumn("read_completed_seq", seq).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: mark thread read: %w", err)
	}
	return nil
}

func (s *Store) ListUnreadThreads() ([]Thread, error) {
	var rows []Thread
	if err := s.db.Where("archived = ? AND completed_seq > read_completed_seq", false).
		Order("last_active_at desc, id desc").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("store: list unread completions: %w", err)
	}
	return rows, nil
}

func refreshCompletionWatermark(tx *gorm.DB, threadID string) error {
	var seq int64
	if err := tx.Model(&Event{}).Where("thread_id = ? AND completion = ?", threadID, true).
		Select("COALESCE(MAX(seq), 0)").Scan(&seq).Error; err != nil {
		return err
	}
	return tx.Model(&Thread{}).Where("id = ?", threadID).Updates(map[string]any{
		"completed_seq":      seq,
		"read_completed_seq": gorm.Expr("MIN(read_completed_seq, ?)", seq),
	}).Error
}

func (s *Store) suppressTurnCompletion(id string, fields map[string]any) error {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var turn Turn
		if err := tx.First(&turn, "id = ?", id).Error; err != nil {
			return err
		}
		if err := tx.Model(&Turn{}).Where("id = ?", id).Updates(fields).Error; err != nil {
			return err
		}
		if err := tx.Model(&Event{}).Where("turn_id = ? AND completion = ?", id, true).
			UpdateColumn("completion", false).Error; err != nil {
			return err
		}
		return refreshCompletionWatermark(tx, turn.ThreadID)
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: quiet turn completion: %w", err)
	}
	return nil
}
