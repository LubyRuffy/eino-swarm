package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
)

// ErrHistoryReferenceInvalid means a caller supplied an invalid or repeated locator.
var ErrHistoryReferenceInvalid = errors.New("store: invalid original history reference")

// ErrHistorySourceUnavailable means a source is missing, ambiguous, private or outside its project.
var ErrHistorySourceUnavailable = errors.New("store: original history source is unavailable")

// ErrHistoryEvidenceLimit means complete source text and metadata exceed the caller's budget.
var ErrHistoryEvidenceLimit = errors.New("store: original history evidence exceeds its budget")

// ErrHistoryEvidenceChanged means a bound source snapshot changed before publication.
var ErrHistoryEvidenceChanged = errors.New("store: original history evidence changed before answer admission")

// ErrHistoryRegistryInheritanceInvalid means a Registry edge is invalid or conflicts with its stored parent.
var ErrHistoryRegistryInheritanceInvalid = errors.New("store: invalid original history registry inheritance")

const historyRegistryLineageSQL = `WITH RECURSIVE lineage(id) AS (
	SELECT id FROM turns WHERE thread_id=? AND id=?
	UNION
	SELECT source.id FROM lineage AS child
	JOIN turns AS current ON current.id=child.id AND current.thread_id=?
	JOIN events AS edge INDEXED BY idx_evt_thread_turn_kind_seq ON edge.thread_id=current.thread_id AND edge.turn_id=current.id AND edge.kind='history_registry_inherited'
	JOIN turns AS source ON source.id=edge.source_turn_id AND source.thread_id=current.thread_id AND source.seq>0 AND source.seq<current.seq
)`

// ProvenHistoryRegistryOwner selects only a supplied owner whose stored lineage
// proves that all supplied owners belonged to the same inherited Registry.
func (s *Store) ProvenHistoryRegistryOwner(ctx context.Context, threadID string, ownerIDs []string) (ownerID string, err error) {
	defer func() {
		if cause := context.Cause(ctx); cause != nil {
			err = errors.Join(err, cause)
		}
		if err != nil {
			ownerID = ""
		}
	}()
	if cause := context.Cause(ctx); cause != nil {
		return "", cause
	}
	if len(ownerIDs) == 0 {
		return "", nil
	}
	if threadID == "" {
		return "", ErrHistoryRegistryInheritanceInvalid
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(ownerIDs))
	for _, id := range ownerIDs {
		if id == "" {
			return "", ErrHistoryRegistryInheritanceInvalid
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var turns []Turn
		if err := tx.Select("id", "seq").Where("thread_id=? AND id IN ?", threadID, ids).Find(&turns).Error; err != nil {
			return err
		}
		if len(turns) != len(ids) {
			return ErrHistoryRegistryInheritanceInvalid
		}
		var latestSeq int64
		for _, turn := range turns {
			if turn.Seq <= 0 {
				return ErrHistoryRegistryInheritanceInvalid
			}
			if turn.Seq > latestSeq {
				ownerID, latestSeq = turn.ID, turn.Seq
			}
		}
		if len(ids) == 1 {
			return nil
		}
		var count int64
		if err := tx.Raw(historyRegistryLineageSQL+` SELECT COUNT(*) FROM lineage WHERE id IN ?`, threadID, ownerID, threadID, ids).Scan(&count).Error; err != nil {
			return err
		}
		if count != int64(len(ids)) {
			return ErrHistoryRegistryInheritanceInvalid
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	return ownerID, err
}

// RecordHistoryRegistryInheritance records a host-observed Registry transfer.
// It accepts no model prose and does not change either turn's original owner.
func (s *Store) RecordHistoryRegistryInheritance(ctx context.Context, threadID, targetTurnID, sourceTurnID string) error {
	_, _, err := s.RecordHistoryRegistryInheritanceEvent(ctx, threadID, targetTurnID, sourceTurnID)
	return err
}

// RecordHistoryRegistryInheritanceEvent returns the new durable event for publication.
// Idempotent calls return no receipt and created=false, so callers do not rebroadcast.
func (s *Store) RecordHistoryRegistryInheritanceEvent(ctx context.Context, threadID, targetTurnID, sourceTurnID string) (event *Event, created bool, err error) {
	defer func() {
		if cause := context.Cause(ctx); cause != nil {
			err = errors.Join(err, cause)
		}
		if err != nil {
			event, created = nil, false
		}
	}()
	run := func() error {
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
		if threadID == "" || targetTurnID == "" || sourceTurnID == "" || targetTurnID == sourceTurnID {
			return ErrHistoryRegistryInheritanceInvalid
		}
		// As with answer admission, reserve the event identity before entering the
		// single-connection transaction. Legacy cold sequence seeding has no ctx.
		seq, err := s.nextEventSeq(threadID)
		if err != nil {
			return err
		}
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var turns []Turn
			if err := tx.Select("id", "thread_id", "seq").Where("id IN ?", []string{targetTurnID, sourceTurnID}).Find(&turns).Error; err != nil {
				return err
			}
			var source, target Turn
			for _, turn := range turns {
				if turn.ID == sourceTurnID {
					source = turn
				} else {
					target = turn
				}
			}
			if len(turns) != 2 || source.ThreadID != threadID || target.ThreadID != threadID || source.Seq <= 0 || target.Seq <= source.Seq {
				return ErrHistoryRegistryInheritanceInvalid
			}
			var parents []Event
			if err := tx.Select("source_turn_id").Where("thread_id=? AND turn_id=? AND kind=?", threadID, targetTurnID, "history_registry_inherited").Limit(2).Find(&parents).Error; err != nil {
				return err
			}
			if len(parents) > 0 {
				if len(parents) == 1 && parents[0].SourceTurnID == sourceTurnID {
					return nil
				}
				return ErrHistoryRegistryInheritanceInvalid
			}
			inserted := Event{ThreadID: threadID, TurnID: targetTurnID, SourceTurnID: sourceTurnID, Kind: "history_registry_inherited", Seq: seq, CreatedAt: time.Now().UTC()}
			write := tx.Create(&inserted)
			if write.Error != nil {
				return write.Error
			}
			if write.RowsAffected != 1 {
				return fmt.Errorf("original history registry inheritance wrote %d events", write.RowsAffected)
			}
			event = &inserted
			return nil
		})
	}
	err = run()
	return event, event != nil, err
}

// HistoryReference names one canonical message or event by its owning thread and sequence.
type HistoryReference struct {
	Kind     string `json:"kind"`
	ThreadID string `json:"thread_id"`
	Seq      int64  `json:"seq"`
}

// HistorySource is a complete published body with its original locator and attribution.
// It excludes reasoning and private model tool-call payloads.
type HistorySource struct {
	Reference   HistoryReference `json:"reference"`
	RowID       uint             `json:"row_id"`
	ThreadTitle string           `json:"thread_title"`
	ProjectID   string           `json:"project_id"`
	Role        string           `json:"role"`
	Source      string           `json:"source"`
	AgentID     string           `json:"agent_id"`
	Text        string           `json:"text"`
	EventKind   string           `json:"event_kind,omitempty"`
	Err         string           `json:"err,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
	Images      []ImageRef       `json:"images,omitempty"`
}

// HistoryEvidence binds canonical source snapshots to their selected project.
// An empty ProjectID retains standalone conversations' global history scope.
type HistoryEvidence struct {
	ProjectID string          `json:"project_id"`
	Sources   []HistorySource `json:"sources"`
}

// ReadHistorySources returns complete canonical sources in reference order.
func (s *Store) ReadHistorySources(ctx context.Context, projectID string, refs []HistoryReference, maxChars int) (sources []HistorySource, err error) {
	defer func() {
		if ctx.Err() != nil {
			sources = nil
			err = errors.Join(err, context.Cause(ctx))
		}
	}()
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var readErr error
		sources, readErr = readHistorySources(tx, projectID, refs, maxChars)
		return readErr
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return sources, nil
}

// TurnHasHistoryQuery reports a durable history-read start or tool call, including workers.
func (s *Store) TurnHasHistoryQuery(threadID, turnID string) (bool, error) {
	started, err := s.historyTurnEventExists(threadID, turnID, "history_read_started", "")
	if err != nil || started {
		return started, err
	}
	return s.historyTurnEventExists(threadID, turnID, "tool_call", "query_history(")
}

// TurnHasHistoryReportRequest reports the durable explicit project-report request.
func (s *Store) TurnHasHistoryReportRequest(threadID, turnID string) (bool, error) {
	return s.historyTurnEventExists(threadID, turnID, "history_report_requested", "")
}

// AdmitScheduleAnswerWithHistory revalidates source snapshots in the answer transaction.
func (s *Store) AdmitScheduleAnswerWithHistory(threadID, turnID string, expected []Schedule, events []Event, history *HistoryEvidence, candidateIDs ...string) error {
	return s.admitScheduleAnswer(threadID, turnID, expected, events, history, candidateIDs...)
}

func (s *Store) historyTurnEventExists(threadID, turnID, kind, prefix string) (bool, error) {
	if threadID == "" || turnID == "" {
		return false, nil
	}
	query := historyRegistryLineageSQL + ` SELECT EXISTS(SELECT 1 FROM lineage
		JOIN events AS observed INDEXED BY idx_evt_thread_turn_kind_seq ON observed.thread_id=? AND observed.turn_id=lineage.id AND observed.kind=?`
	args := []any{threadID, turnID, threadID, threadID, kind}
	if prefix != "" {
		query += ` AND substr(observed.text,1,?)=?`
		args = append(args, len(prefix), prefix)
	}
	var exists bool
	err := s.db.Raw(query+` LIMIT 1)`, args...).Scan(&exists).Error
	return exists, err
}

func validateHistoryReferences(refs []HistoryReference) error {
	seen := make(map[HistoryReference]bool, len(refs))
	for _, ref := range refs {
		if (ref.Kind != "message" && ref.Kind != "event") || ref.ThreadID == "" || ref.Seq <= 0 || seen[ref] {
			return ErrHistoryReferenceInvalid
		}
		seen[ref] = true
	}
	return nil
}

func requireHistoryProject(tx *gorm.DB, projectID string) error {
	if projectID == "" {
		return nil
	}
	var exists bool
	if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM projects WHERE id=?)`, projectID).Scan(&exists).Error; err != nil {
		return err
	}
	if !exists {
		return ErrHistorySourceUnavailable
	}
	return nil
}

func readHistorySources(tx *gorm.DB, projectID string, refs []HistoryReference, maxChars int) ([]HistorySource, error) {
	if maxChars < 2 {
		return nil, ErrHistoryEvidenceLimit
	}
	if err := validateHistoryReferences(refs); err != nil {
		return nil, err
	}
	if err := requireHistoryProject(tx, projectID); err != nil {
		return nil, err
	}
	out := make([]HistorySource, 0, len(refs))
	used := 2
	for _, ref := range refs {
		source, err := readHistorySource(tx, projectID, ref, maxChars-used)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(source)
		if err != nil {
			return nil, fmt.Errorf("encode canonical history source: %w", err)
		}
		cost := utf8.RuneCount(body)
		if len(out) > 0 {
			cost++
		}
		if cost > maxChars-used {
			return nil, ErrHistoryEvidenceLimit
		}
		out = append(out, source)
		used += cost
	}
	return out, nil
}

type historyCanonicalRow struct {
	RowID       uint
	ThreadTitle string
	ProjectID   string
	Role        string
	Source      string
	AgentID     string
	Text        string
	EventKind   string
	Err         string
	CreatedAt   time.Time
	Images      []ImageRef `gorm:"serializer:json"`
}

func readHistorySource(tx *gorm.DB, projectID string, ref HistoryReference, maxChars int) (HistorySource, error) {
	table, body, origin, eventKind, observedError := "messages", "content", "s.source", "''", "''"
	if ref.Kind == "event" {
		table, body, origin, eventKind, observedError = "events", "text", "''", "s.kind", "s.err"
	}
	query := tx.Table(table+" AS s").Joins("JOIN threads AS th ON th.id=s.thread_id").Where("s.thread_id=? AND s.seq=?", ref.ThreadID, ref.Seq)
	if projectID != "" {
		query = query.Where("th.project_id=?", projectID)
	}
	if ref.Kind == "event" {
		query = query.Where("s.kind IS NOT ?", "engine_restart_checkpoint")
	}
	metadata := []string{"th.title", "th.project_id", "s.thread_id", "s.role", origin, "s.agent_id", eventKind, observedError, "s.created_at", "s.images"}
	lengths := make([]string, len(metadata))
	for i, column := range metadata {
		lengths[i] = "COALESCE(length(CAST(" + column + " AS BLOB)),0)"
	}
	var sizes []struct {
		RowID                    uint
		BodyBytes, MetadataBytes int64
	}
	projection := "s.id AS row_id,COALESCE(length(CAST(s." + body + " AS BLOB)),0) AS body_bytes," + strings.Join(lengths, "+") + " AS metadata_bytes"
	if err := query.Session(&gorm.Session{}).Select(projection).Limit(2).Find(&sizes).Error; err != nil {
		return HistorySource{}, err
	}
	if len(sizes) != 1 {
		return HistorySource{}, ErrHistorySourceUnavailable
	}
	bytes := sizes[0].BodyBytes + sizes[0].MetadataBytes
	// UTF-8 uses at most four bytes per rune. This allocation preflight is
	// conservative; the exact encoded source/array budget is checked above.
	if bytes > 0 && (bytes-1)/utf8.UTFMax >= int64(maxChars) {
		return HistorySource{}, ErrHistoryEvidenceLimit
	}
	fields := []string{"s.id AS row_id", "th.title AS thread_title", "th.project_id AS project_id", "s.role", origin + " AS source", "s.agent_id", "s." + body + " AS text", eventKind + " AS event_kind", observedError + " AS err", "s.created_at", "s.images"}
	var rows []historyCanonicalRow
	if err := query.Session(&gorm.Session{}).Select(strings.Join(fields, ",")).Limit(2).Find(&rows).Error; err != nil {
		return HistorySource{}, err
	}
	if len(rows) != 1 || rows[0].RowID != sizes[0].RowID {
		return HistorySource{}, ErrHistorySourceUnavailable
	}
	row := rows[0]
	return HistorySource{Reference: ref, RowID: row.RowID, ThreadTitle: row.ThreadTitle, ProjectID: row.ProjectID, Role: row.Role, Source: row.Source, AgentID: row.AgentID, Text: row.Text, EventKind: row.EventKind, Err: row.Err, CreatedAt: row.CreatedAt, Images: row.Images}, nil
}

func verifyHistoryEvidence(tx *gorm.DB, threadID string, evidence *HistoryEvidence) error {
	if evidence == nil {
		return nil
	}
	var recipient Thread
	if err := tx.Select("id", "project_id").Where("id=?", threadID).Take(&recipient).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrHistoryEvidenceChanged
		}
		return err
	}
	if recipient.ProjectID != evidence.ProjectID {
		return ErrHistoryEvidenceChanged
	}
	refs := make([]HistoryReference, len(evidence.Sources))
	for i, source := range evidence.Sources {
		refs[i] = source.Reference
	}
	body, err := json.Marshal(evidence.Sources)
	if err != nil {
		return fmt.Errorf("%w: invalid source snapshot: %v", ErrHistoryEvidenceChanged, err)
	}
	actual, err := readHistorySources(tx, evidence.ProjectID, refs, utf8.RuneCount(body))
	if err != nil {
		if errors.Is(err, ErrHistoryReferenceInvalid) || errors.Is(err, ErrHistorySourceUnavailable) || errors.Is(err, ErrHistoryEvidenceLimit) {
			return fmt.Errorf("%w: %w", ErrHistoryEvidenceChanged, err)
		}
		return err
	}
	for i, source := range actual {
		if !sameHistorySource(source, evidence.Sources[i]) {
			return ErrHistoryEvidenceChanged
		}
	}
	return nil
}

func sameHistorySource(a, b HistorySource) bool {
	return a.Reference == b.Reference && a.RowID == b.RowID && a.ThreadTitle == b.ThreadTitle && a.ProjectID == b.ProjectID && a.Role == b.Role && a.Source == b.Source && a.AgentID == b.AgentID && a.Text == b.Text && a.EventKind == b.EventKind && a.Err == b.Err && a.CreatedAt.Equal(b.CreatedAt) && slices.Equal(a.Images, b.Images)
}
