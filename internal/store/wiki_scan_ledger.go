package store

import (
	"context"
	"slices"
	"time"

	"gorm.io/gorm"
)

// WikiSourceScan remembers successful work for an original immutable event.
// It is scoped to the project and does not persist source content. Editable
// steer captions are always read again.
type WikiSourceScan struct {
	ProjectID       string `gorm:"primaryKey;size:64"`
	EventID         uint   `gorm:"primaryKey;autoIncrement:false;index"`
	ThreadID        string `gorm:"size:64"`
	TurnID          string `gorm:"size:64"`
	Seq             int64
	Kind            string `gorm:"size:32"`
	SourceCreatedAt time.Time
	LocallyIndexed  bool
	WikiReviewed    bool
}

// Removing or editing a retained event invalidates its derived acknowledgement
// even before the next scan. New history arrives through inserts.
func ensureWikiSourceScanInvalidation(db *gorm.DB) error {
	if err := db.Exec(`CREATE TRIGGER IF NOT EXISTS wiki_source_scan_deleted
		AFTER DELETE ON events BEGIN
			DELETE FROM wiki_source_scans WHERE event_id=OLD.id;
		END`).Error; err != nil {
		return err
	}
	return db.Exec(`CREATE TRIGGER IF NOT EXISTS wiki_source_scan_changed
		AFTER UPDATE OF text,err,thread_id,turn_id,seq,kind,agent_id,tool_call_id,work_record_payload,created_at ON events BEGIN
			DELETE FROM wiki_source_scans WHERE event_id=OLD.id;
		END`).Error
}

// ForgetWikiSourceScans discards derived receipts when a knowledge store
// is initialized again. Its Markdown files remain the publication authority.
func (s *Store) ForgetWikiSourceScans(ctx context.Context, projectID string) error {
	return s.db.WithContext(ctx).Delete(&WikiSourceScan{}, "project_id = ?", projectID).Error
}

const wikiSourceIdentity = `s.project_id=q.project_id AND s.event_id=e.id AND s.thread_id=e.thread_id
	AND s.turn_id=e.turn_id AND s.seq=e.seq AND s.kind=e.kind AND s.source_created_at=e.created_at`
const wikiSourceIndexed = `EXISTS (SELECT 1 FROM wiki_source_scans s WHERE ` + wikiSourceIdentity + ` AND s.locally_indexed=1)`
const wikiSourceReviewed = `EXISTS (SELECT 1 FROM wiki_source_scans s WHERE ` + wikiSourceIdentity + ` AND s.wiki_reviewed=1)`

// WikiReviewMetadataWindow reports source identities already fully accepted
// during an earlier build. Current project, turn and event ownership are
// checked before reusing the result.
func (s *Store) WikiReviewMetadataWindow(ctx context.Context, reviewID string, after uint, limit int) ([]DistillationSourceRow, error) {
	return s.distillationWindow(ctx, reviewID, after, limit, distillationEventIdentity+", "+wikiSourceReviewed+" AS wiki_reviewed")
}

// CheckpointWikiScan commits the local receipt and newly indexed source
// identities together. The independent semantic cursor stays unchanged.
func (s *Store) CheckpointWikiScan(ctx context.Context, row ProjectDistillation, previousScan uint, eventIDs []uint) error {
	if !row.WikiGraph || row.State != "running" {
		return ErrNotFound
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Table(row.table()).
			Where(`project_id=? AND id=? AND state=? AND cursor_event_id=? AND
				COALESCE(json_extract(CAST(receipt AS TEXT),'$.wiki_scan_cursor'),0)=?`,
				row.ProjectID, row.ID, "running", row.CursorEventID, previousScan).
			Update("receipt", row.Receipt)
		if err := result.Error; err != nil {
			return err
		}
		if result.RowsAffected != 1 {
			return ErrNotFound
		}
		return acknowledgeWikiSources(tx, row, eventIDs, true)
	})
}

// CheckpointWikiReview acknowledges fully reviewed sources in the same
// transaction as advancing the existing semantic cursor. A source split
// across multiple windows is reusable only after its final window is accepted.
func (s *Store) CheckpointWikiReview(ctx context.Context, row ProjectDistillation, previous uint, eventIDs []uint) error {
	if !row.WikiGraph {
		return ErrNotFound
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := acknowledgeWikiSources(tx, row, eventIDs, false); err != nil {
			return err
		}
		return checkpointDistillation(tx, row, previous)
	})
}

const wikiAckIdentity = `wiki_source_scans.thread_id=excluded.thread_id AND wiki_source_scans.turn_id=excluded.turn_id
	AND wiki_source_scans.seq=excluded.seq AND wiki_source_scans.kind=excluded.kind
	AND wiki_source_scans.source_created_at=excluded.source_created_at`

func acknowledgeWikiSources(tx *gorm.DB, row ProjectDistillation, eventIDs []uint, indexed bool) error {
	if len(eventIDs) == 0 {
		return nil
	}
	ids := slices.Clone(eventIDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	const live = distillationQueueJoins + ` WHERE q.project_id=? AND q.review_id=? AND e.id IN ?`
	var counts struct {
		Live  int
		Steer int
	}
	if err := tx.Raw(`SELECT COUNT(*) AS live, COALESCE(SUM(e.kind='steer'),0) AS steer`+
		live, row.ProjectID, row.ID, ids).Scan(&counts).Error; err != nil {
		return err
	}
	if counts.Live != len(ids) {
		return ErrHistorySourceUnavailable
	}
	res := tx.Exec(`INSERT INTO wiki_source_scans
		(project_id,event_id,thread_id,turn_id,seq,kind,source_created_at,locally_indexed,wiki_reviewed)
		SELECT q.project_id,e.id,e.thread_id,e.turn_id,e.seq,e.kind,e.created_at,?,?`+live+` AND e.kind<>'steer'
		ON CONFLICT(project_id,event_id) DO UPDATE SET
			thread_id=excluded.thread_id, turn_id=excluded.turn_id, seq=excluded.seq, kind=excluded.kind,
			source_created_at=excluded.source_created_at,
			locally_indexed=excluded.locally_indexed OR ((`+wikiAckIdentity+`) AND wiki_source_scans.locally_indexed),
			wiki_reviewed=excluded.wiki_reviewed OR ((`+wikiAckIdentity+`) AND wiki_source_scans.wiki_reviewed)`,
		indexed, !indexed, row.ProjectID, row.ID, ids)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != int64(counts.Live-counts.Steer) {
		return ErrHistorySourceUnavailable
	}
	return nil
}
