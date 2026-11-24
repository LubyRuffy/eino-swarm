package store

import (
	"context"

	"gorm.io/gorm"
)

// ProjectDistillationSource identifies a saved source without duplicating its text.
type ProjectDistillationSource struct {
	ProjectID  string `gorm:"index;size:64"`
	ReviewID   string `gorm:"primaryKey;size:64"`
	EventID    uint   `gorm:"primaryKey;autoIncrement:false"`
	ThreadID   string `gorm:"size:64"`
	TurnID     string `gorm:"size:64"`
	Seq        int64
	Kind       string `gorm:"size:32"`
	TurnStatus string `gorm:"size:32"`
}

func distillationSources(db *gorm.DB, projectID, threadID string) *gorm.DB {
	query := db.Table("events AS e").
		Joins("JOIN turns t ON t.id=e.turn_id AND t.thread_id=e.thread_id").
		Joins("JOIN threads h ON h.id=t.thread_id").
		Where("h.project_id = ? AND e.work_record_payload = 0", projectID).
		Where("(t.status=? AND e.kind IN ?) OR (t.status=? AND e.kind IN ?)",
			TurnDone, []string{"user_message", "steer", "agent_message", "tool_result"},
			TurnError, []string{"user_message", "steer"})
	if threadID != "" {
		query = query.Where("h.id = ?", threadID)
	}
	return query
}

// ProjectDistillationAnchor provides a Trace destination while a batch is queued.
func (s *Store) ProjectDistillationAnchor(ctx context.Context, projectID, threadID string) (Turn, error) {
	var turn Turn
	query := s.db.WithContext(ctx).Table("turns t").Joins("JOIN threads h ON h.id=t.thread_id").
		Where("h.project_id=?", projectID).
		Where(`EXISTS (SELECT 1 FROM events e WHERE e.thread_id=t.thread_id AND e.turn_id=t.id AND e.work_record_payload=0
			AND ((t.status=? AND e.kind IN ?) OR (t.status=? AND e.kind IN ?)))`,
			TurnDone, []string{"user_message", "steer", "agent_message", "tool_result"},
			TurnError, []string{"user_message", "steer"})
	if threadID != "" {
		query = query.Where("h.id=?", threadID)
	}
	result := query.Select("t.id,t.thread_id,t.status,t.provider_id,t.model,t.reasoning_effort").
		Order("COALESCE(t.ended_at,t.started_at) DESC, t.id DESC").Limit(1).Scan(&turn)
	return turn, result.Error
}

// StartDistillationQueue fixes the source identities and the denominator in
// the same transaction as the first receipt. A later turn cannot enter the job.
func (s *Store) StartDistillationQueue(ctx context.Context, row ProjectDistillation, threadID string, receipt func(int) ([]byte, error)) (int, error) {
	total := 0
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		previous := tx.Table(row.table()).Select("id").Where("project_id = ?", row.ProjectID)
		if err := tx.Delete(&ProjectDistillationSource{}, "project_id = ? AND review_id IN (?)", row.ProjectID, previous).Error; err != nil {
			return err
		}
		query := distillationSources(tx, row.ProjectID, threadID).
			Select("?,?,e.id,e.thread_id,e.turn_id,e.seq,e.kind,t.status", row.ProjectID, row.ID)
		insert := tx.Exec(`INSERT INTO project_distillation_sources
			(project_id,review_id,event_id,thread_id,turn_id,seq,kind,turn_status) ?`, query)
		if insert.Error != nil {
			return insert.Error
		}
		total = int(insert.RowsAffected)
		if total == 0 {
			return ErrNotFound
		}
		var err error
		row.Receipt, err = receipt(total)
		if err != nil {
			return err
		}
		return startProjectDistillation(tx, row)
	})
	return total, err
}

// DistillationSourceRow separates the frozen queue position from the currently
// available evidence. A vanished source still has a position to account for.
type DistillationSourceRow struct {
	Event
	Turn         Turn `gorm:"embedded;embeddedPrefix:origin_"`
	Position     uint
	CallID       uint
	Oversize     bool
	WikiReviewed bool
}

const distillationEventIdentity = "e.id,e.thread_id,e.turn_id,e.seq,e.kind,e.agent_id,e.role,e.tool_call_id,e.created_at"

const distillationEventProjection = distillationEventIdentity + `,
	CASE WHEN length(CAST(e.text AS BLOB)) <= 65536 THEN e.text END AS text,
	CASE WHEN length(CAST(e.err AS BLOB)) <= 65536 THEN e.err END AS err,
	COALESCE(length(CAST(e.text AS BLOB)),0) > 65536 OR COALESCE(length(CAST(e.err AS BLOB)),0) > 65536 AS oversize`

const distillationQueueJoins = "\n FROM project_distillation_sources q" + distillationQueueOwners

const distillationQueueOwners = `
	LEFT JOIN threads h ON h.id=q.thread_id AND h.project_id=q.project_id
	LEFT JOIN turns t ON t.id=q.turn_id AND t.thread_id=h.id AND t.status=q.turn_status
	LEFT JOIN events e ON e.id=q.event_id AND e.turn_id=t.id AND e.thread_id=h.id AND e.seq=q.seq AND e.kind=q.kind AND e.work_record_payload=0`

// The nearest preceding invocation must match the result's turn and agent.
const distillationPairedCall = `SELECT c.id FROM events c
	WHERE e.kind='tool_result' AND e.tool_call_id<>'' AND
		c.thread_id=e.thread_id AND c.turn_id=e.turn_id AND c.kind='tool_call'
		AND c.tool_call_id=e.tool_call_id AND c.agent_id=e.agent_id AND c.seq<e.seq AND c.id<e.id
	ORDER BY c.seq DESC LIMIT 1`

func (s *Store) DistillationWindow(ctx context.Context, reviewID string, after uint, limit int) ([]DistillationSourceRow, error) {
	return s.distillationWindow(ctx, reviewID, after, limit, distillationEventProjection)
}

func (s *Store) DistillationMetadataWindow(ctx context.Context, reviewID string, after uint, limit int) ([]DistillationSourceRow, error) {
	return s.distillationWindow(ctx, reviewID, after, limit, distillationEventIdentity)
}

func (s *Store) distillationWindow(ctx context.Context, reviewID string, after uint, limit int, projection string) ([]DistillationSourceRow, error) {
	var rows []DistillationSourceRow
	err := s.db.WithContext(ctx).Raw(`
		SELECT q.event_id AS position, `+projection+`,
			t.id AS origin_id, t.thread_id AS origin_thread_id, t.status AS origin_status,
			(`+distillationPairedCall+`) AS call_id
		`+distillationQueueJoins+`
		WHERE q.review_id=? AND q.event_id>? ORDER BY q.event_id LIMIT ?`, reviewID, after, limit).Scan(&rows).Error
	return rows, err
}

func (s *Store) DistillationToolCalls(ctx context.Context, ids []uint) (map[uint]Event, error) {
	return s.distillationToolCalls(ctx, ids, distillationEventProjection)
}

// DistillationCallHeads returns enough of a paired call to identify the tool
// without loading its arguments. When clipped, only a name followed by "("
// within the prefix is identifiable.
func (s *Store) DistillationCallHeads(ctx context.Context, ids []uint) (map[uint]Event, error) {
	return s.distillationToolCalls(ctx, ids, distillationEventIdentity+", "+distillationCallHead("e")+" AS text")
}

func distillationCallHead(alias string) string {
	text := alias + ".text"
	return "CASE WHEN length(CAST(" + text + " AS BLOB)) <= 512 THEN " + text +
		" WHEN instr(substr(" + text + ",1,128), '(') > 0 THEN substr(" + text + ",1,128) END"
}

func (s *Store) distillationToolCalls(ctx context.Context, ids []uint, projection string) (map[uint]Event, error) {
	calls := make(map[uint]Event, len(ids))
	if len(ids) == 0 {
		return calls, nil
	}
	var rows []DistillationSourceRow
	if err := s.db.WithContext(ctx).Table("events e").Select(projection).
		Where("e.id IN ? AND e.kind = ? AND e.work_record_payload = 0", ids, "tool_call").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		if !row.Oversize {
			calls[row.ID] = row.Event
		}
	}
	return calls, nil
}

// CheckpointDistillation installs a receipt and cursor together. The old
// cursor and owner must still match even after a cancellation or restart.
func (s *Store) CheckpointDistillation(ctx context.Context, row ProjectDistillation, previous uint) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return checkpointDistillation(tx, row, previous)
	})
}

func checkpointDistillation(tx *gorm.DB, row ProjectDistillation, previous uint) error {
	result := tx.Table(row.table()).
		Where("project_id=? AND id=? AND state=? AND cursor_event_id=?", row.ProjectID, row.ID, "running", previous).
		Updates(map[string]any{"state": row.State, "receipt": row.Receipt, "cursor_event_id": row.CursorEventID})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	if row.State == "completed" {
		return tx.Delete(&ProjectDistillationSource{}, "review_id = ?", row.ID).Error
	}
	return nil
}

// ResumeDistillation claims the same receipt without changing its checkpoint.
func (s *Store) ResumeDistillation(ctx context.Context, row ProjectDistillation, previousState string) error {
	result := s.db.WithContext(ctx).Table(row.table()).
		Where("project_id=? AND id=? AND state=? AND cursor_event_id=?", row.ProjectID, row.ID, previousState, row.CursorEventID).
		Updates(map[string]any{"state": "running", "receipt": row.Receipt})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}
