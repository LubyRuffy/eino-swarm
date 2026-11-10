package store

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
)

const (
	dreamHistoryPeers    = 7
	dreamHistoryReviews  = 8
	dreamHistoryDocBytes = 24_000
	// DreamToolEventLimit bounds pairing work for a single source turn. A
	// longer turn cannot prove that a tool ID is unique in this search.
	DreamToolEventLimit = 192
	DreamToolTextBytes  = 8_000
)

// DreamReviewEvents reads bounded review tails owned by the current project and provider route.
// The triggering conversation and at most seven recently created peers each
// own a separate indexed tail. A moved conversation's prior turns do not qualify.
func (s *Store) DreamReviewEvents(ctx context.Context, projectID, providerID, threadID, turnID string) ([]Event, error) {
	var out []Event
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := dreamHistoryOwner(tx, projectID, providerID, threadID, turnID); err != nil {
			return err
		}
		var peers []string
		if err := tx.Raw(`SELECT id FROM threads WHERE project_id=? AND id<>? ORDER BY rowid DESC LIMIT ?`, projectID, threadID, dreamHistoryPeers).Scan(&peers).Error; err != nil {
			return err
		}
		for _, id := range append([]string{threadID}, peers...) {
			var events []Event
			// Apply the cap to the index tail before testing ownership or reading
			// the body. Filtering first could walk an unlimited history of a
			// moved thread in search of eligible records.
			const query = `SELECT ev.id, ev.thread_id, ev.turn_id, ev.seq, ev.kind, ev.agent_id, ev.err, ev.created_at,
				ev.source_thread_id, ev.source_turn_id,
				CASE WHEN length(CAST(ev.text AS BLOB))<=? THEN ev.text END AS text
				FROM (SELECT id, turn_id FROM events INDEXED BY idx_evt_thread_kind_seq
					WHERE thread_id=? AND kind='memory_review' ORDER BY seq DESC LIMIT ?) AS recent
				JOIN events AS ev ON ev.id=recent.id
				JOIN turns AS t ON t.id=recent.turn_id AND t.thread_id=ev.thread_id
				WHERE t.project_id=? AND t.provider_id=? AND t.status=?`
			if err := tx.Raw(query, dreamHistoryDocBytes, id, dreamHistoryReviews, projectID, providerID, TurnDone).Scan(&events).Error; err != nil {
				return err
			}
			out = append(out, events...)
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DreamToolEvents retrieves complete bounded call/result identity from an
// owned completed turn on the same provider route. Oversized bodies are omitted, retaining the rows so
// duplicates and unavailable evidence cannot masquerade as successful pairs.
func (s *Store) DreamToolEvents(ctx context.Context, projectID, providerID, threadID, turnID string) ([]Event, error) {
	var out []Event
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := dreamHistoryOwner(tx, projectID, providerID, threadID, turnID); err != nil {
			return err
		}
		for _, kind := range []string{"tool_call", "tool_result"} {
			var events []Event
			const query = `SELECT id, thread_id, turn_id, seq, kind, agent_id, role, err, created_at,
				source_thread_id, source_turn_id, worker_generation, tool_call_id, tool_receipt_scope,
				CASE WHEN length(CAST(text AS BLOB))<=? THEN text END AS text
				FROM events INDEXED BY idx_evt_thread_turn_kind_seq
				WHERE thread_id=? AND turn_id=? AND kind=? ORDER BY seq DESC LIMIT ?`
			if err := tx.Raw(query, DreamToolTextBytes, threadID, turnID, kind, DreamToolEventLimit+1).Scan(&events).Error; err != nil {
				return err
			}
			out = append(out, events...)
			if len(out) > DreamToolEventLimit {
				return ErrHistoryEvidenceLimit
			}
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func dreamHistoryOwner(tx *gorm.DB, projectID, providerID, threadID, turnID string) error {
	if projectID == "" || providerID == "" || threadID == "" || turnID == "" {
		return ErrHistorySourceUnavailable
	}
	var owns bool
	const query = `SELECT EXISTS(SELECT 1 FROM turns AS t JOIN threads AS th ON th.id=t.thread_id
		JOIN projects AS p ON p.id=th.project_id
		WHERE t.id=? AND t.thread_id=? AND t.project_id=? AND p.id=? AND t.provider_id=? AND t.status=?)`
	if err := tx.Raw(query, turnID, threadID, projectID, projectID, providerID, TurnDone).Scan(&owns).Error; err != nil {
		return err
	}
	if !owns {
		return ErrHistorySourceUnavailable
	}
	return nil
}
