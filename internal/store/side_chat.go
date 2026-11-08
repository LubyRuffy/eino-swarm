package store

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"gorm.io/gorm"
)

const (
	sideChatTaskCharacters  = 4096
	sideChatEventCharacters = 1536
	sideChatTailRows        = 96
	sideChatContextRows     = 12
)

// SideChatContext is the bounded, user-facing text preview for a side question.
type SideChatContext struct {
	Thread Thread
	Latest Turn
	Events []Event
}

// ReadSideChatContext pins the thread, latest turn and transcript tail to one
// read snapshot. Only the text preview crosses the boundary, and the SQLite
// connection is released before the caller contacts a model.
func (s *Store) ReadSideChatContext(ctx context.Context, threadID string) (SideChatContext, error) {
	var out SideChatContext
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Model(&Thread{}).
			Select("provider_id, model, reasoning_effort, substr(goal, 1, ?) AS goal, goal_complete, goal_blocked, goal_capped", sideChatTaskCharacters).
			Where("id = ?", threadID).Take(&out.Thread).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		// A selected-text send places the human's request after its quotes.
		// Start at that boundary before truncating, even with a large quote.
		// The composer escapes this closing tag when it occurs inside a quote.
		const requestBoundary = "\n</selected_text>\n\n<user_request>\n"
		if err := tx.Model(&Turn{}).
			Select(`status, provider_id, model, reasoning_effort, quiet, goal_continue, schedule_continue,
				substr(user_text, CASE WHEN user_text GLOB '<selected_text>*'
					THEN max(1, instr(user_text, ?)) ELSE 1 END, ?) AS user_text`, requestBoundary, sideChatTaskCharacters).
			Where("thread_id = ?", threadID).Order("seq DESC").Limit(1).Find(&out.Latest).Error; err != nil {
			return err
		}
		// Search a fixed number of newest rows first, even in a long task
		// that is producing tool updates but very little visible prose.
		return tx.Raw(`
			SELECT recent.kind, substr(recent.text, 1, ?) AS text
			FROM (
				SELECT turn_id, seq, kind, agent_id, text
				FROM events WHERE thread_id = ? AND seq > 0
				ORDER BY seq DESC LIMIT ?
			) recent
			LEFT JOIN turns t ON t.id = recent.turn_id AND t.thread_id = ?
			WHERE COALESCE(t.quiet, false) = false
			  AND (recent.kind = 'user_message'
			    OR (recent.kind IN ('agent_message', 'done')
			      AND COALESCE(recent.agent_id, '') IN ('', 'manager')))
			ORDER BY recent.seq DESC LIMIT ?`,
			sideChatEventCharacters, threadID, sideChatTailRows, threadID, sideChatContextRows).Scan(&out.Events).Error
	})
	if err != nil {
		return SideChatContext{}, fmt.Errorf("store: read side chat context: %w", err)
	}
	slices.Reverse(out.Events)
	return out, nil
}
