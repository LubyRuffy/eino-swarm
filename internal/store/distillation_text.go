package store

import "context"

type DistillationText struct {
	Text       string
	TotalBytes int
	MessageSeq int64
	Offset     int `gorm:"-"`
}

// ReadDistillationText returns a byte window from a still-owned source. The
// caller cuts on a UTF-8 boundary. For steering, the current human transcript
// caption is authoritative, and a missing or ambiguous caption is unavailable.
// Tool success is checked against the whole result when a record is entered.
func (s *Store) ReadDistillationText(ctx context.Context, reviewID string, position uint, offset, limit int) (DistillationText, error) {
	return s.readDistillationText(ctx, reviewID, position, offset, limit, "e.text")
}

// A command receipt also contains the original command, its execution path
// and diagnostics. Only the observed output can support wiki claims.
const wikiSourceProjection = `CASE WHEN e.kind='tool_result' AND json_valid(e.text) THEN
	CASE WHEN json_type(e.text,'$.full_command')='text' AND json_type(e.text,'$.stdout')='text' THEN
		CASE WHEN json_type(e.text,'$.failed')='true' OR COALESCE(json_extract(e.text,'$.exit_code'),0)<>0 THEN ''
			WHEN COALESCE(json_extract(e.text,'$.stderr'),'')='' THEN json_extract(e.text,'$.stdout')
			ELSE json_extract(e.text,'$.stdout') || char(10) || json_extract(e.text,'$.stderr') END
		ELSE e.text END
	ELSE e.text END`

const distillationToolSucceeded = `CASE
	WHEN NOT json_valid(e.text) THEN 1
	WHEN json_type(e.text)<>'object' THEN 1
	ELSE NOT EXISTS (
		SELECT 1 FROM json_each(e.text) j
		WHERE j.key IN ('ok','success') AND j.type='false'
	) END`

func (s *Store) ReadWikiSourceText(ctx context.Context, reviewID string, position uint, offset, limit int) (DistillationText, error) {
	return s.readDistillationText(ctx, reviewID, position, offset, limit, wikiSourceProjection)
}

func (s *Store) readDistillationText(ctx context.Context, reviewID string, position uint, offset, limit int, projection string) (DistillationText, error) {
	var rows []DistillationText
	err := s.db.WithContext(ctx).Raw(`
		WITH source AS (
			SELECT CASE WHEN e.kind='steer' THEN
				CASE WHEN substr(m.content,1,8)='[steer] ' THEN CAST(m.content AS BLOB) END
				ELSE CAST(`+projection+` AS BLOB) END AS body,
				CASE WHEN e.kind='steer' THEN 8 ELSE 0 END AS prefix,
				COALESCE(m.seq,0) AS message_seq
			`+distillationQueueJoins+`
			LEFT JOIN messages m ON e.kind='steer' AND m.thread_id=e.thread_id
				AND m.turn_id=e.turn_id AND m.event_seq=e.seq AND m.role='user'
			WHERE q.review_id=? AND q.event_id=? AND e.id IS NOT NULL
				AND (e.kind<>'tool_result' OR CASE
					WHEN COALESCE(e.err,'')<>'' THEN 0
					WHEN ?<>0 THEN 1
					ELSE `+distillationToolSucceeded+` END)
			LIMIT 2
		)
		SELECT substr(body,prefix+?+1,?) AS text, COALESCE(length(body),0)-prefix AS total_bytes, message_seq
		FROM source`, reviewID, position, offset, offset, limit).Scan(&rows).Error
	if err != nil {
		return DistillationText{}, err
	}
	if len(rows) != 1 || rows[0].TotalBytes < 0 {
		return DistillationText{}, ErrHistorySourceUnavailable
	}
	part := rows[0]
	part.Offset = offset
	return part, nil
}
