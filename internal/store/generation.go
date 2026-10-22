package store

import "fmt"

// ListGenerationEvents reconstructs request lifetimes without loading tool
// output bodies from a long conversation.
func (s *Store) ListGenerationEvents(threadID string) ([]Event, error) {
	var events []Event
	err := s.db.Where("thread_id = ? AND kind IN ?", threadID, []string{"generation_started", "generation_completed", "generation_interrupted", "generation_recovered"}).Order("seq asc").Find(&events).Error
	if err != nil {
		return nil, fmt.Errorf("store: list generation events: %w", err)
	}
	return events, nil
}
