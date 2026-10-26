package store

import "fmt"

// TurnInputSource identifies the initial request of one turn. Explicit source
// markers win; a legacy input is human only when its own durable send event
// proves that origin. Later steering cannot change a turn's original source.
func (s *Store) TurnInputSource(threadID, turnID string) (string, error) {
	var source string
	err := s.db.Raw(`SELECT CASE
 WHEN COALESCE(source, '') <> '' THEN source
 WHEN EXISTS (
  SELECT 1 FROM events
  WHERE events.thread_id = messages.thread_id
  AND events.turn_id = messages.turn_id AND kind = 'user_message'
 ) THEN ? ELSE '' END
 FROM messages
 WHERE thread_id = ? AND turn_id = ? AND role = 'user'
 ORDER BY seq LIMIT 1`, MessageSourceHuman, threadID, turnID).Scan(&source).Error
	if err != nil {
		return "", fmt.Errorf("store: read turn input source: %w", err)
	}
	return source, nil
}
