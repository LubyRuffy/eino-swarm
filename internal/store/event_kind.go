package store

import "fmt"

// ListEventsOfKind pages a durable inbox without scanning unrelated tool
// streams. The cursor remains the timeline sequence used by replay and Trace.
func (s *Store) ListEventsOfKind(threadID, kind string, since int64, limit int) ([]Event, error) {
	var events []Event
	err := s.db.Where("thread_id = ? AND kind = ? AND seq > ?", threadID, kind, since).
		Order("seq asc").Limit(limit).Find(&events).Error
	if err != nil {
		return nil, fmt.Errorf("store: list event kind: %w", err)
	}
	return events, nil
}

// AdvanceTaskMessageCursor never lets a reread of an older page regress the
// default inbox cursor. Explicit after_seq can always recover old messages.
func (s *Store) AdvanceTaskMessageCursor(threadID string, seq int64) error {
	if err := s.db.Model(&Thread{}).Where("id = ? AND task_message_through_seq < ?", threadID, seq).
		UpdateColumn("task_message_through_seq", seq).Error; err != nil {
		return fmt.Errorf("store: advance task message cursor: %w", err)
	}
	return nil
}
