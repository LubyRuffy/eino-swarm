package store

import (
	"context"
	"unicode/utf8"
)

// A bounded first slice keeps each page of the scan predictable. Larger
// sources keep their original size and use ReadWikiSourceText for the tail.
const wikiScanBytes = 128 << 10
const wikiScanWindowRows = 128

// WikiScanSource preserves its frozen position when the live evidence is
// unavailable. CallHead never contains an unbounded tool argument.
type WikiScanSource struct {
	DistillationSourceRow
	Part           DistillationText `gorm:"embedded;embeddedPrefix:part_"`
	CallHead       string
	Available      bool `gorm:"-"`
	LocallyIndexed bool
}

const wikiUnambiguousEvent = `e.id IS NOT NULL AND NOT EXISTS (
	SELECT 1 FROM events duplicate WHERE duplicate.thread_id=e.thread_id AND duplicate.seq=e.seq
	AND duplicate.id<>e.id AND duplicate.kind IS NOT 'engine_restart_checkpoint'
	AND COALESCE(duplicate.work_record_payload,0)=0)`

const wikiScanBody = `CASE
	WHEN NOT (` + wikiUnambiguousEvent + `) OR ` + wikiSourceIndexed + ` THEN NULL
	WHEN e.kind='steer' THEN CASE WHEN substr(m.content,1,8)='[steer] ' THEN m.content END
	WHEN e.kind='tool_result' THEN
		CASE WHEN c.id IS NOT NULL AND COALESCE(e.err,'')='' AND (` + distillationToolSucceeded + `)
		THEN ` + wikiSourceProjection + ` END
	ELSE e.text END`

// WikiScanWindow loads original evidence in frozen-queue order. Per-row
// projection checks tool success and uses the current unambiguous steer caption.
// Only the first bounded UTF-8 slice crosses the database boundary.
func (s *Store) WikiScanWindow(ctx context.Context, reviewID string, after uint, limit int) ([]WikiScanSource, error) {
	var rows []WikiScanSource
	err := s.db.WithContext(ctx).Raw(`
		WITH queued AS MATERIALIZED (
			SELECT * FROM project_distillation_sources
			WHERE review_id=? AND event_id>? ORDER BY event_id LIMIT ?
		), evidence AS (
			SELECT q.event_id AS position, `+distillationEventIdentity+`,
				t.id AS origin_id, t.thread_id AS origin_thread_id, t.status AS origin_status,
				c.id AS call_id, `+distillationCallHead("c")+` AS call_head,
				CAST(`+wikiScanBody+` AS BLOB) AS body,
				CASE WHEN e.kind='steer' THEN 8 ELSE 0 END AS prefix,
				COALESCE(m.seq,0) AS message_seq, (`+wikiUnambiguousEvent+` AND `+wikiSourceIndexed+`) AS locally_indexed
			FROM queued q`+distillationQueueOwners+`
			LEFT JOIN events c ON c.id=(`+distillationPairedCall+`) AND c.work_record_payload=0
			LEFT JOIN messages m ON e.kind='steer' AND m.id=(
				SELECT CASE WHEN COUNT(*)=1 THEN MIN(id) END FROM messages
				WHERE thread_id=e.thread_id AND turn_id=e.turn_id AND event_seq=e.seq AND role='user'
			)
		)
		SELECT position,id,thread_id,turn_id,seq,kind,agent_id,role,tool_call_id,created_at,
			origin_id,origin_thread_id,origin_status,call_id,call_head,
			substr(body,prefix+1,?) AS part_text, COALESCE(length(body),0)-prefix AS part_total_bytes,
			message_seq AS part_message_seq,locally_indexed
		FROM evidence ORDER BY position`, reviewID, after, min(max(limit, 0), wikiScanWindowRows), wikiScanBytes+1).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for i := range rows {
		part := &rows[i].Part
		if len(part.Text) > wikiScanBytes {
			end := wikiScanBytes
			for !utf8.RuneStart(part.Text[end]) && end > wikiScanBytes-utf8.UTFMax {
				end--
			}
			part.Text = part.Text[:end]
		}
		if rows[i].LocallyIndexed || (utf8.ValidString(part.Text) && part.Text != "") {
			rows[i].Available = true
		} else {
			*part = DistillationText{}
		}
	}
	return rows, nil
}
