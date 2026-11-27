package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ProjectDistillation stores the latest manual review receipt for one project.
// It contains no transcript and is replaced only by the next explicit run.
type ProjectDistillation struct {
	ProjectID                          string `gorm:"primaryKey;size:64"`
	ID                                 string `gorm:"size:64;not null"`
	State                              string `gorm:"size:24;not null"`
	Receipt                            []byte
	CursorEventID                      uint `gorm:"not null;default:0"`
	ProviderID, Model, ReasoningEffort string
}

func (r ProjectDistillation) table() string { return "project_distillations" }

// ProjectDistillationReceipt reads the latest run, if any.
func (s *Store) ProjectDistillationReceipt(ctx context.Context, projectID string) (*ProjectDistillation, error) {
	row := ProjectDistillation{}
	err := s.db.WithContext(ctx).Table(row.table()).Take(&row, "project_id = ?", projectID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}

// StartProjectDistillation replaces the latest receipt before a reviewer can run.
func (s *Store) StartProjectDistillation(ctx context.Context, row ProjectDistillation) error {
	return startProjectDistillation(s.db.WithContext(ctx), row)
}

func startProjectDistillation(db *gorm.DB, row ProjectDistillation) error {
	return db.Table(row.table()).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "project_id"}}, UpdateAll: true,
	}).Create(&row).Error
}

// FinishProjectDistillation updates only the running receipt it owns.
func (s *Store) FinishProjectDistillation(ctx context.Context, row ProjectDistillation) error {
	result := s.db.WithContext(ctx).Table(row.table()).
		Where("project_id = ? AND id = ? AND state = ?", row.ProjectID, row.ID, "running").
		Updates(map[string]any{"state": row.State, "receipt": row.Receipt})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

// DistillationTurn is the bounded evidence captured from one terminal turn.
type DistillationTurn struct {
	Turn   Turn
	Events []Event
}

// ProjectDistillationHistory captures one read snapshot. The query fixes current
// project membership and terminal status independently of reviewer output.
func (s *Store) ProjectDistillationHistory(ctx context.Context, projectID, threadID string, latest bool, kinds []string) ([]DistillationTurn, bool, error) {
	const maxTurns, maxEvents, maxEventBytes = 64, 768, 64 << 10
	var turns []DistillationTurn
	omitted := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		where := tx.Table("turns AS t").Joins("JOIN threads AS h ON h.id = t.thread_id").
			Where("h.project_id = ? AND t.status IN ?", projectID, []string{TurnDone, TurnError})
		limit := maxTurns + 1
		order := "COALESCE(t.ended_at,t.started_at) DESC, t.id DESC"
		if threadID != "" {
			where = where.Where("t.thread_id = ?", threadID)
		}
		if latest {
			where = where.Where("t.status = ?", TurnDone)
			limit = 1
			order = "t.seq DESC"
		}
		var selected []Turn
		if err := where.Select("t.id, t.thread_id, t.project_id, t.status, t.provider_id, t.model, t.reasoning_effort").
			Order(order).Limit(limit).Find(&selected).Error; err != nil {
			return err
		}
		omitted = len(selected) > maxTurns
		remaining := maxEvents
		for _, turn := range selected[:min(len(selected), maxTurns)] {
			var rows []struct {
				Event
				Oversize bool
			}
			if err := tx.Model(&Event{}).
				Select("id, thread_id, turn_id, seq, kind, agent_id, role, tool_call_id, created_at, "+
					"CASE WHEN length(CAST(text AS BLOB)) <= ? THEN text END AS text, "+
					"CASE WHEN length(CAST(err AS BLOB)) <= ? THEN err END AS err, "+
					"COALESCE(length(CAST(text AS BLOB)),0) > ? OR COALESCE(length(CAST(err AS BLOB)),0) > ? AS oversize",
					maxEventBytes, maxEventBytes, maxEventBytes, maxEventBytes).
				Where("thread_id = ? AND turn_id = ? AND kind IN ? AND work_record_payload = 0", turn.ThreadID, turn.ID, kinds).
				Order("seq DESC").Limit(remaining + 1).Find(&rows).Error; err != nil {
				return err
			}
			if len(rows) > remaining {
				rows = rows[:remaining]
				omitted = true
			}
			remaining -= len(rows)
			events := make([]Event, 0, len(rows))
			slices.Reverse(rows)
			unsafeSteers := false
			for _, row := range rows {
				if row.Oversize {
					omitted = true
					unsafeSteers = unsafeSteers || row.Kind == "steer_revised" || row.Kind == "steer_retracted"
					continue
				}
				events = append(events, row.Event)
			}
			if unsafeSteers {
				events = slices.DeleteFunc(events, func(ev Event) bool { return ev.Kind == "steer" })
			}
			turns = append(turns, DistillationTurn{Turn: turn, Events: events})
			if remaining == 0 {
				omitted = omitted || len(turns) < len(selected)
				break
			}
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	return turns, omitted, err
}

// DistillationSteerMessage identifies the current human caption corresponding
// to a steer event. Knowledge uses the message's mutable-content identity.
func (s *Store) DistillationSteerMessage(ctx context.Context, event Event) (*Message, error) {
	var rows []Message
	err := s.db.WithContext(ctx).Select("seq", "content").
		Where("thread_id = ? AND turn_id = ? AND event_seq = ? AND role = ?", event.ThreadID, event.TurnID, event.Seq, "user").
		Limit(2).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, ErrHistorySourceUnavailable
	}
	return &rows[0], nil
}
