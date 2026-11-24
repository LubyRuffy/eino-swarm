package store

import (
	"context"
	"database/sql"
	"strings"

	"gorm.io/gorm"
)

// ReadProjectWikiQuoteIdentity proves that an excerpt still exists in the
// current source. Identity and exact excerpt come from one SQLite snapshot.
func (s *Store) ReadProjectWikiQuoteIdentity(ctx context.Context, projectID string, ref HistoryReference, quote string) (*HistoryIdentity, error) {
	if projectID == "" || ref.ThreadID == "" || ref.Seq <= 0 || strings.TrimSpace(quote) == "" {
		return nil, ErrHistoryReferenceInvalid
	}
	table, body := "events", strings.ReplaceAll(wikiSourceProjection, "e.", "s.")
	switch ref.Kind {
	case "event":
	case "message":
		table, body = "messages", "s.content"
	default:
		return nil, ErrHistoryReferenceInvalid
	}
	var identity *HistoryIdentity
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := readProjectHistoryIdentity(tx, projectID, ref)
		if err != nil || row == nil {
			return err
		}
		var count int64
		query := scopedHistorySourceQuery(tx, table, projectID, ref).
			Where("s.id=? AND instr("+body+", ?) > 0", row.RowID, quote)
		if row.EventKind == "tool_result" {
			success := strings.ReplaceAll(distillationToolSucceeded, "e.", "s.")
			query = query.Where("COALESCE(s.err,'')='' AND (" + success + ")")
		}
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count == 1 {
			identity = row
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	return identity, err
}
