package store

import (
	"fmt"
	"strings"
)

const (
	MessageSourceHuman   = "human"
	MessageSourceRuntime = "runtime"
)

// MessageHasHumanSource resolves only legacy rows which match a runtime cue.
// A real human event is authoritative even when its text quotes that cue.
func (s *Store) MessageHasHumanSource(row Message) (bool, error) {
	steer := strings.TrimPrefix(row.Content, "[steer] ")
	var human bool
	err := s.db.Raw(`SELECT (
 NOT EXISTS (SELECT 1 FROM messages WHERE thread_id = @thread AND turn_id = @turn AND role = 'user' AND seq < @seq)
 AND (EXISTS (
  SELECT 1 FROM events WHERE thread_id = @thread AND turn_id = @turn AND kind = 'user_message' AND text = @content
 ) OR EXISTS (
  SELECT 1 FROM turns WHERE thread_id = @thread AND id = @turn AND user_text = @content
  AND goal_continue = false AND schedule_continue = false
 ))
) OR EXISTS (
 SELECT 1 FROM events WHERE thread_id = @thread AND turn_id = @turn AND kind = 'steer'
 AND @steerIdentity AND text = @steer AND (@eventSeq = 0 OR seq = @eventSeq)
)`, map[string]any{
		"thread": row.ThreadID, "turn": row.TurnID, "seq": row.Seq, "content": row.Content,
		"steer": steer, "eventSeq": row.EventSeq,
		"steerIdentity": row.EventSeq > 0 || strings.HasPrefix(row.Content, "[steer] "),
	}).Scan(&human).Error
	if err != nil {
		return false, fmt.Errorf("store: resolve human message source: %w", err)
	}
	return human, nil
}
