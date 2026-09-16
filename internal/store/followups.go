package store

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// EnqueueFollowup appends a waiting message to a conversation. The turn that
// is already running does not see it; the next one does, unless someone
// steers it in earlier.
func (s *Store) EnqueueFollowup(threadID, text string) (*Followup, error) {
	if _, err := s.GetThread(threadID); err != nil {
		return nil, err
	}
	f := &Followup{
		ID:        NewID("fu_"),
		ThreadID:  threadID,
		Text:      text,
		CreatedAt: time.Now().UTC(),
		Seq: s.nextSeq(s.followupSeq, threadID, func() int64 {
			return s.maxSeq(&Followup{}, "seq", threadID)
		}),
	}
	if err := s.db.Create(f).Error; err != nil {
		return nil, fmt.Errorf("store: enqueue follow-up: %w", err)
	}
	return f, nil
}

// ListFollowups returns waiting messages in the order they were typed.
func (s *Store) ListFollowups(threadID string) ([]Followup, error) {
	var out []Followup
	if err := s.db.Where("thread_id = ?", threadID).Order("seq asc").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("store: list follow-ups: %w", err)
	}
	return out, nil
}

// GetFollowup loads one waiting message that belongs to this conversation.
func (s *Store) GetFollowup(threadID, id string) (*Followup, error) {
	var f Followup
	err := s.db.Where("id = ? AND thread_id = ?", id, threadID).First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get follow-up: %w", err)
	}
	return &f, nil
}

// DeleteFollowup drops one waiting message. Missing is not found, not success:
// a Steer click on a row that already flushed would otherwise look like it worked.
func (s *Store) DeleteFollowup(threadID, id string) error {
	res := s.db.Where("id = ? AND thread_id = ?", id, threadID).Delete(&Followup{})
	if res.Error != nil {
		return fmt.Errorf("store: delete follow-up: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func isBusy(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "SQLITE_BUSY") || strings.Contains(s, "database is locked")
}

func retryBusy(n int, fn func() error) error {
	var err error
	for i := 0; i < n; i++ {
		err = fn()
		if err == nil || !isBusy(err) {
			return err
		}
		time.Sleep(time.Duration(15*(i+1)) * time.Millisecond)
	}
	return err
}

// PopFollowup removes and returns the oldest waiting message, or nil when the
// queue is empty. Used when a finished turn starts the next follow-up.
func (s *Store) PopFollowup(threadID string) (*Followup, error) {
	var out *Followup
	err := retryBusy(8, func() error {
		list, err := s.ListFollowups(threadID)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			out = nil
			return nil
		}
		f := list[0]
		if err := s.db.Where("id = ? AND thread_id = ?", f.ID, threadID).Delete(&Followup{}).Error; err != nil {
			return err
		}
		out = &f
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store: pop follow-up: %w", err)
	}
	return out, nil
}

// UnshiftFollowup puts a popped message back at the front of the queue, which
// is how a follow-up survives a StartTurn that lost the race to another run.
func (s *Store) UnshiftFollowup(f *Followup) error {
	if f == nil {
		return nil
	}
	if _, err := s.GetThread(f.ThreadID); err != nil {
		return err
	}
	var minSeq int64
	if err := s.db.Model(&Followup{}).Where("thread_id = ?", f.ThreadID).
		Select("COALESCE(MIN(seq), 1)").Scan(&minSeq).Error; err != nil {
		return fmt.Errorf("store: unshift follow-up: %w", err)
	}
	f.Seq = minSeq - 1
	if f.ID == "" {
		f.ID = NewID("fu_")
	}
	if f.CreatedAt.IsZero() {
		f.CreatedAt = time.Now().UTC()
	}
	if err := s.db.Create(f).Error; err != nil {
		return fmt.Errorf("store: unshift follow-up: %w", err)
	}
	return nil
}
