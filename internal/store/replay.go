package store

import "fmt"

// SQLite's trim defaults to ASCII spaces. Replay deduplication has always used
// Go's strings.TrimSpace, including its Unicode White_Space characters.
const replayWhitespace = "\t\n\v\f\r \u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"

// ListReplayMessages reads only conversational rows beyond the compact watermark.
// Reasoning/tool-call columns are not replayed, so their bodies stay in SQLite.
// Pin the role index: ORDER BY alone otherwise favors the old seq index and
// visits every tool row before applying the role filter.
func (s *Store) ListReplayMessages(threadID string, through int64, skipTurnID string) ([]Message, error) {
	q := s.db.Table("messages INDEXED BY idx_msg_thread_role_seq").Select("thread_id", "turn_id", "seq", "role", "content", "images", "event_seq", "source").
		Where("thread_id = ? AND role IN ?", threadID, []string{"user", "assistant"})
	if through > 0 {
		q = q.Where("seq > ?", through)
	}
	if skipTurnID != "" {
		q = q.Where("turn_id <> ?", skipTurnID)
	}
	var rows []Message
	if err := q.Order("seq asc").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("store: list replay messages: %w", err)
	}
	return rows, nil
}

// ListReplayEvents reads retraction evidence and manager answers that still need
// repair. Event seq and message seq have different watermarks: steering evidence
// must remain available even when its seq precedes the message watermark.
func (s *Store) ListReplayEvents(threadID string, through int64, skipTurnID string) ([]Event, error) {
	// A turn can be live even if it contains only tool/empty/retracted rows. These
	// scalar identity checks preserve that rule without loading their bodies.
	live := `EXISTS (SELECT 1 FROM messages AS live INDEXED BY idx_messages_turn_id
  WHERE live.thread_id = events.thread_id AND live.turn_id = events.turn_id`
	args := []any{}
	if through > 0 {
		live += " AND live.seq > ?"
		args = append(args, through)
	}
	if skipTurnID != "" {
		live += " AND live.turn_id <> ?"
		args = append(args, skipTurnID)
	}
	live += ")"
	// Do not repair an answer already stored below the compact watermark: that
	// would give it a new seq and resurrect compacted context. The turn-id index
	// bounds each lookup to one turn rather than rescanning every assistant row.
	missing := `NOT EXISTS (SELECT 1 FROM messages AS stored INDEXED BY idx_messages_turn_id
  WHERE stored.thread_id = events.thread_id AND stored.turn_id = events.turn_id
  AND stored.role = 'assistant' AND trim(stored.content, ?) = trim(events.text, ?))`
	args = append(args, replayWhitespace, replayWhitespace)
	var rows []Event
	err := s.db.Table("events INDEXED BY idx_evt_thread_kind_seq").Where("thread_id = ? AND seq > 0 AND kind IN ?", threadID, []string{"steer_retracted", "steer", "agent_message"}).
		Where("kind <> 'agent_message' OR ((agent_id = '' OR agent_id = 'manager' OR agent_id IS NULL) AND "+live+" AND "+missing+")", args...).
		Order("seq asc").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("store: list replay events: %w", err)
	}
	return rows, nil
}
