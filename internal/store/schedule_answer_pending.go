package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// PendingScheduleAnswer is private admission state, never an event or replay
// message. Its payload may contain prose that has not passed verification.
// FinishTurn clears that payload atomically with its terminal status, retaining
// only decision and retry metadata; a failed finish preserves recovery bytes.
type PendingScheduleAnswer struct {
	ID                     string    `gorm:"primaryKey;size:64" json:"id"`
	ThreadID               string    `gorm:"index;size:64" json:"thread_id"`
	TurnID                 string    `gorm:"index;size:64;uniqueIndex:idx_pending_answer_turn_ordinal,priority:1" json:"turn_id"`
	Ordinal                int64     `gorm:"uniqueIndex:idx_pending_answer_turn_ordinal,priority:2" json:"ordinal"`
	Kind                   string    `gorm:"size:16" json:"kind"`
	Payload                string    `json:"payload"`
	State                  string    `gorm:"size:16" json:"state"`
	CreatedAt              time.Time `json:"created_at"`
	Attempts               int       `json:"attempts"`
	ClassificationFailures int       `json:"classification_failures"`
	RetryAt                time.Time `json:"retry_at"`
}

// SavePendingScheduleAnswer binds immutable candidate bytes to a live owner.
// Re-saving an identity reads its existing decision and retry state back into p.
func (s *Store) SavePendingScheduleAnswer(p *PendingScheduleAnswer) error {
	if p == nil || p.ThreadID == "" || p.TurnID == "" || (p.Kind != "answer" && p.Kind != "report") || p.Attempts < 0 || p.ClassificationFailures < 0 || p.ClassificationFailures > p.Attempts {
		return ErrScheduleAnswerChanged
	}
	row := *p
	if row.ID == "" {
		row.ID = NewID("answer")
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var existing PendingScheduleAnswer
		err := tx.First(&existing, "id = ?", row.ID).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil && (existing.ThreadID != row.ThreadID || existing.TurnID != row.TurnID || existing.Kind != row.Kind || existing.Payload != row.Payload) {
			return ErrScheduleAnswerChanged
		}
		if err := scheduleAnswerOwner(tx, row.ThreadID, row.TurnID); err != nil {
			return err
		}
		if existing.ID != "" {
			row = existing
			return nil
		}
		if row.Payload == "" || row.State != "" && row.State != "pending" {
			return ErrScheduleAnswerChanged
		}
		row.State = "pending"
		if row.CreatedAt.IsZero() {
			row.CreatedAt = time.Now().UTC()
		}
		var lastOrdinal int64
		if err := tx.Model(&PendingScheduleAnswer{}).Where("turn_id = ?", row.TurnID).Select("COALESCE(MAX(ordinal), 0)").Scan(&lastOrdinal).Error; err != nil {
			return err
		}
		row.Ordinal = lastOrdinal + 1
		if err := requireScheduleAnswerWrite(tx.Create(&row), 1); err != nil {
			return err
		}
		if row.Kind != "answer" {
			return nil
		}
		return compactOlderScheduleAnswers(tx, row.TurnID, row.ID)
	})
	if err != nil {
		return fmt.Errorf("store: save pending schedule answer: %w", err)
	}
	*p = row
	return nil
}

// Deferred candidates retain unpublished content, but only the latest answer
// needs its transcript for recovery. Keeping every prefix would grow a long
// goal quadratically. Reports retain their distinct action receipts unchanged.
func compactOlderScheduleAnswers(tx *gorm.DB, turnID, latestID string) error {
	older := "turn_id = ? AND kind = 'answer' AND id <> ? AND (payload <> '' OR state = 'pending')"
	var rows []PendingScheduleAnswer
	if err := tx.Where(older, turnID, latestID).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		payload, state := "", row.State
		if state == "pending" {
			state = "rejected"
		} else if state == "deferred" {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(row.Payload), &fields); err != nil {
				return fmt.Errorf("store: decode deferred schedule answer for compaction: %w", err)
			}
			if fields == nil {
				return fmt.Errorf("store: deferred schedule answer is not an object")
			}
			delete(fields, "transcript")
			body, err := json.Marshal(fields)
			if err != nil {
				return fmt.Errorf("store: compact deferred schedule answer: %w", err)
			}
			payload = string(body)
		}
		if err := requireScheduleAnswerWrite(tx.Model(&PendingScheduleAnswer{}).Where("id = ?", row.ID).Updates(map[string]any{"payload": payload, "state": state}), 1); err != nil {
			return err
		}
	}
	return nil
}

// GetPendingScheduleAnswer includes settled candidates so recovery can detect
// an admission that committed before the process lost its acknowledgement.
func (s *Store) GetPendingScheduleAnswer(id string) (*PendingScheduleAnswer, error) {
	var row PendingScheduleAnswer
	err := s.db.First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get pending schedule answer: %w", err)
	}
	return &row, nil
}

// ListPendingScheduleAnswers returns all decisions in durable insertion order.
func (s *Store) ListPendingScheduleAnswers(turnID string) ([]PendingScheduleAnswer, error) {
	var rows []PendingScheduleAnswer
	if err := s.db.Where("turn_id = ?", turnID).Order("ordinal ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("store: list pending schedule answers: %w", err)
	}
	return rows, nil
}

// ListScheduleReportsForRecovery reads original report-call identities for a
// thread snapshot without loading full answer transcripts. Rejected reports
// remain relevant until terminal persistence clears their private payloads.
func (s *Store) ListScheduleReportsForRecovery(threadID string) ([]PendingScheduleAnswer, error) {
	var rows []PendingScheduleAnswer
	if err := s.db.Where("thread_id = ? AND kind = ? AND payload <> ?", threadID, "report", "").Order("turn_id ASC, ordinal ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("store: list reports for recovery: %w", err)
	}
	return rows, nil
}

// SetScheduleAnswerRetry persists the consumed allowance before another call.
// A stale write cannot rewind attempts or revive a settled/deleted candidate.
func (s *Store) SetScheduleAnswerRetry(id string, attempts int, retryAt time.Time) error {
	return s.setScheduleAnswerRecovery(id, attempts, nil, retryAt, false, false)
}

// SetScheduleAnswerRecovery saves classifier failures and its durable deferral
// decision together. A deferred candidate cannot regain execution ownership by
// retrying or by restarting; its unpublished bytes remain private.
func (s *Store) SetScheduleAnswerRecovery(id string, attempts, classificationFailures int, retryAt time.Time, deferred bool) error {
	return s.setScheduleAnswerRecovery(id, attempts, &classificationFailures, retryAt, deferred, false)
}

// RestoreScheduleAnswerRecovery records a legacy candidate's existing failure
// evidence without consuming another attempt. It only fills a missing counter;
// the engine must derive that counter from this candidate's durable receipts.
func (s *Store) RestoreScheduleAnswerRecovery(id string, attempts, classificationFailures int, retryAt time.Time, deferred bool) error {
	return s.setScheduleAnswerRecovery(id, attempts, &classificationFailures, retryAt, deferred, true)
}

func (s *Store) setScheduleAnswerRecovery(id string, attempts int, classificationFailures *int, retryAt time.Time, deferred, restore bool) error {
	if attempts < 0 || classificationFailures != nil && (*classificationFailures < 0 || *classificationFailures > attempts || deferred && *classificationFailures == 0) {
		return ErrScheduleAnswerChanged
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var row PendingScheduleAnswer
		if err := tx.First(&row, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if row.State == "accepted" {
			return ErrScheduleAnswerAlreadyAdmitted
		}
		if row.State != "pending" || attempts < row.Attempts || (attempts == row.Attempts && !row.RetryAt.IsZero() && !row.RetryAt.Equal(retryAt)) {
			return ErrScheduleAnswerChanged
		}
		failures := row.ClassificationFailures
		if classificationFailures != nil {
			failures = *classificationFailures
			if restore {
				if row.ClassificationFailures != 0 || attempts != row.Attempts || failures == 0 || !row.RetryAt.Equal(retryAt) {
					return ErrScheduleAnswerChanged
				}
			} else if attempts == row.Attempts && (failures != row.ClassificationFailures || deferred) {
				return ErrScheduleAnswerChanged
			}
		}
		if err := scheduleAnswerOwner(tx, row.ThreadID, row.TurnID); err != nil {
			return err
		}
		state := "pending"
		if deferred {
			state = "deferred"
		}
		return requireScheduleAnswerWrite(tx.Model(&row).Updates(map[string]any{"attempts": attempts, "classification_failures": failures, "retry_at": retryAt, "state": state}), 1)
	})
	if err != nil {
		return fmt.Errorf("store: update schedule answer retry: %w", err)
	}
	return nil
}

// RejectPendingScheduleAnswer may settle an owned candidate after Stop, but
// cannot change an accepted decision or another conversation's candidate.
func (s *Store) RejectPendingScheduleAnswer(threadID, turnID, id string) error {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var row PendingScheduleAnswer
		if err := tx.First(&row, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if row.ThreadID != threadID || row.TurnID != turnID {
			return ErrScheduleAnswerChanged
		}
		if row.State == "accepted" {
			return ErrScheduleAnswerAlreadyAdmitted
		}
		if row.State == "rejected" {
			return nil
		}
		if row.State != "pending" && row.State != "deferred" {
			return ErrScheduleAnswerChanged
		}
		return requireScheduleAnswerWrite(tx.Model(&row).Update("state", "rejected"), 1)
	})
	if err != nil {
		return fmt.Errorf("store: reject pending schedule answer: %w", err)
	}
	return nil
}

func scheduleAnswerOwner(tx *gorm.DB, threadID, turnID string) error {
	var thread Thread
	if err := tx.Select("id").First(&thread, "id = ?", threadID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	var turn Turn
	if err := tx.First(&turn, "id = ? AND thread_id = ?", turnID, threadID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	if turn.Status != TurnRunning {
		return ErrScheduleAnswerChanged
	}
	var stopped int64
	if err := tx.Model(&Event{}).Where("thread_id = ? AND turn_id = ? AND kind = ?", threadID, turnID, "user_stop_requested").Count(&stopped).Error; err != nil {
		return err
	}
	if stopped != 0 {
		return ErrScheduleAnswerStopped
	}
	return nil
}

func requireScheduleAnswerWrite(result *gorm.DB, expected int64) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != expected {
		return fmt.Errorf("store: incomplete schedule answer write: affected %d rows, expected %d", result.RowsAffected, expected)
	}
	return nil
}
