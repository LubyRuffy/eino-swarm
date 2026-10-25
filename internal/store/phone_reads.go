package store

import (
	"fmt"
	"sort"
)

// ListSummaryTurns pages nonquiet turns newest-first in SQL, returning each
// page in conversation order. Only fields used by an inbox summary are read.
// A caller can continue before the page's first Seq when protocol-only text
// does not yield a human-readable summary.
func (s *Store) ListSummaryTurns(threadID string, before int64, limit int) ([]Turn, error) {
	if limit <= 0 {
		return []Turn{}, nil
	}
	q := s.db.Select("seq", "user_text", "final").Where("thread_id = ? AND COALESCE(quiet, false) = ?", threadID, false)
	if before > 0 {
		q = q.Where("seq < ?", before)
	}
	var out []Turn
	if err := q.Order("seq desc").Limit(limit).Find(&out).Error; err != nil {
		return nil, fmt.Errorf("store: list summary turns: %w", err)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// ListCompletedTurnPreviews returns only the newest requested nonrunning
// turns, in conversation order. Quiet turns remain present as in open detail.
func (s *Store) ListCompletedTurnPreviews(threadID string, limit int) ([]Turn, error) {
	if limit <= 0 {
		return []Turn{}, nil
	}
	var out []Turn
	err := s.db.Select("id", "status", "user_text", "final").Where("thread_id = ? AND (status IS NULL OR status <> ?)", threadID, TurnRunning).
		Order("seq desc").Limit(limit).Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("store: list completed turn previews: %w", err)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// LatestTurnID identifies the live-edge turn without loading its transcript or
// every earlier turn. An empty conversation returns an empty ID.
func (s *Store) LatestTurnID(threadID string) (string, error) {
	var turn Turn
	if err := s.db.Select("id").Where("thread_id = ?", threadID).Order("seq desc").Limit(1).Find(&turn).Error; err != nil {
		return "", fmt.Errorf("store: latest turn id: %w", err)
	}
	return turn.ID, nil
}

// ListTailEventsByKinds reads only preview fields from the requested turn and
// kinds. Each kind owns an indexed bounded tail; merging at most kinds*limit
// rows avoids sorting or decoding a whole long turn for an IN-kind query.
// The resulting page is oldest first and can be continued before its first Seq.
func (s *Store) ListTailEventsByKinds(threadID, turnID string, kinds []string, before int64, limit int) ([]Event, error) {
	if limit <= 0 {
		return []Event{}, nil
	}
	var candidates []Event
	for _, kind := range kinds {
		q := s.db.Table("events INDEXED BY idx_evt_thread_turn_kind_seq").Select("turn_id", "seq", "kind", "text").Where("thread_id = ? AND turn_id = ? AND kind = ? AND seq > 0", threadID, turnID, kind)
		if before > 0 {
			q = q.Where("seq < ?", before)
		}
		var page []Event
		if err := q.Order("seq desc").Limit(limit).Find(&page).Error; err != nil {
			return nil, fmt.Errorf("store: list tail events by kinds: %w", err)
		}
		candidates = append(candidates, page...)
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Seq > candidates[j].Seq })
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	for i, j := 0, len(candidates)-1; i < j; i, j = i+1, j-1 {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	}
	return candidates, nil
}
