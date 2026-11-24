package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/knowledge"
	"github.com/LubyRuffy/eino-swarm/internal/skills"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const wikiLocalRows = 4096
const wikiLocalFirstRows = 384
const wikiLocalReadRows = 128
const wikiLocalReadBytes = 32 * 1024 * 1024
const wikiLocalStructuredBytes = 8 * 1024 * 1024
const wikiLocalSourceBytes = 128 * 1024

// The local stage reads the same frozen queue as the reviewer, but never waits
// for a model or for that project's model-review slot.
func (e *Engine) runWikiLocalScan(ctx context.Context, job *projectDistillWork, reviewCursor uint) error {
	for !job.result.WikiScanComplete {
		if err := e.scanWikiLocalWindow(ctx, job, reviewCursor); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) scanWikiLocalWindow(ctx context.Context, job *projectDistillWork, reviewCursor uint) error {
	if err := context.Cause(ctx); err != nil {
		return err
	}
	done, err := e.beginLifecycleAdmission()
	if err != nil {
		return err
	}
	defer done()
	e.workspaceMu.Lock()
	defer e.workspaceMu.Unlock()
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	if err := e.validateDistillationDestinations(job.projectID, job.kb, job.result); err != nil {
		return err
	}

	current := job.result
	rows, err := e.readWikiLocalWindow(ctx, current)
	if err != nil {
		return err
	}
	if len(rows) == 0 || current.WikiScanProcessed+len(rows) > current.Progress.Total {
		return errors.New("wiki local scan encountered an inconsistent frozen source queue")
	}
	batch, err := e.extractLocalWikiWindow(ctx, job, rows)
	if err != nil {
		return err
	}
	checkpoint := distillCheckpoint{e: e, projectID: job.projectID, current: &job.result, from: reviewCursor}
	if len(batch.nodes) > 0 {
		if err := checkpoint.beforeWrite(); err != nil {
			return err
		}
		if err := job.kb.EnsureWikiSchema(); err != nil {
			return err
		}
		stamp := knowledge.Stamp{UpdatedAt: time.Now(), UpdatedTurn: batch.anchor.ID, UpdatedThread: batch.anchor.ThreadID}
		if err := job.kb.StageWikiNodes(ctx, current.ReviewID, batch.nodes, stamp); err != nil {
			return err
		}
	}

	next := current
	next.WikiScanCursor = rows[len(rows)-1].Position
	next.WikiScanProcessed += len(rows)
	next.WikiScanSkipped += batch.skipped
	next.WikiScanDeferred += batch.deferred
	next.WikiScanComplete = next.WikiScanProcessed == next.Progress.Total
	if next.WikiScanComplete {
		next.WikiPhase = "graph"
	}
	row, err := next.row(job.projectID)
	if err != nil {
		return err
	}
	row.CursorEventID = reviewCursor
	// A file may already have been published when this process is cancelled.
	// Commit the completed window with the same bounded receipt policy as
	// semantic progress. A failed commit leaves the prior cursor replayable.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := e.store.CheckpointWikiScan(saveCtx, row, current.WikiScanCursor, batch.indexed); err != nil {
		return err
	}
	job.result = next
	return nil
}

func (e *Engine) readWikiLocalWindow(ctx context.Context, current DistillationResult) ([]store.WikiScanSource, error) {
	var rows []store.WikiScanSource
	cursor := current.WikiScanCursor
	readBytes := 0
	limit := min(wikiLocalRows, current.Progress.Total-current.WikiScanProcessed)
	if current.WikiScanProcessed == 0 {
		limit = min(limit, wikiLocalFirstRows)
	}
	for len(rows) < limit && readBytes < wikiLocalReadBytes {
		part, err := e.store.WikiScanWindow(ctx, current.ReviewID, cursor, min(wikiLocalReadRows, limit-len(rows)))
		if err != nil {
			return nil, err
		}
		if len(part) == 0 {
			break
		}
		for _, row := range part {
			readBytes += len(row.Part.Text)
		}
		rows = append(rows, part...)
		cursor = part[len(part)-1].Position
	}
	return rows, nil
}

func localWikiReference(row store.WikiScanSource) (store.HistoryReference, bool) {
	if !row.Available || row.Event.CreatedAt.IsZero() || (row.Turn.Status != store.TurnDone && row.Event.Kind != KindUser && row.Event.Kind != KindSteer) {
		return store.HistoryReference{}, false
	}
	ref := store.HistoryReference{Kind: "event", ThreadID: row.Event.ThreadID, Seq: row.Event.Seq}
	switch row.Event.Kind {
	case KindSteer:
		if row.Part.MessageSeq <= 0 {
			return store.HistoryReference{}, false
		}
		ref.Kind, ref.Seq = "message", row.Part.MessageSeq
	case swarm.NotifyToolResult.String():
		name := toolCallName(row.CallHead)
		if name == "" || name == skills.ToolLoad || derivedDistillationTool(name) {
			return store.HistoryReference{}, false
		}
	}
	return ref, true
}

type localWikiWindow struct {
	nodes    []knowledge.WikiNode
	indexed  []uint
	skipped  int
	deferred int
	anchor   store.Turn
}

func (e *Engine) extractLocalWikiWindow(ctx context.Context, job *projectDistillWork, rows []store.WikiScanSource) (localWikiWindow, error) {
	var result localWikiWindow
	var steers []store.HistoryReference
	for _, row := range rows {
		if ref, ok := localWikiReference(row); ok && ref.Kind == "message" {
			steers = append(steers, ref)
		}
	}
	steerIdentities, err := e.store.ReadProjectHistoryIdentities(ctx, job.projectID, steers)
	if err != nil {
		return localWikiWindow{}, err
	}
	byID := make(map[string]knowledge.WikiNode)
	for _, row := range rows {
		if row.LocallyIndexed {
			continue
		}
		ref, ok := localWikiReference(row)
		if !ok {
			result.skipped++
			continue
		}
		// The frozen scan query already examines the unambiguous event
		// and the current owner under recordMu. Steer captions have their
		// own mutable identities and are resolved through the message path.
		identity := &store.HistoryIdentity{Role: row.Event.Role, EventKind: row.Event.Kind,
			OriginTurnID: row.Event.TurnID,
			CreatedAt:    sql.NullTime{Time: row.Event.CreatedAt, Valid: true}}
		if ref.Kind == "message" {
			identity, steerIdentities = steerIdentities[0], steerIdentities[1:]
			if identity == nil || !identity.CreatedAt.Valid || identity.CreatedAt.Time.IsZero() || identity.TurnID != row.Event.TurnID {
				result.skipped++
				continue
			}
		}
		source := knowledge.Source{Kind: ref.Kind, ThreadID: ref.ThreadID, Seq: ref.Seq,
			Timestamp: identity.CreatedAt.Time.UTC(), Classification: knowledgeSourceClass(identity), RecordID: sourceRecordID(identity)}
		found, deferred, err := e.extractLocalWikiSource(ctx, job, row, source)
		if err != nil {
			return localWikiWindow{}, err
		}
		if deferred {
			result.deferred++
			continue
		}
		for _, node := range found {
			if current, ok := byID[node.ID]; ok {
				node.Evidence = append(current.Evidence, node.Evidence...)
			}
			byID[node.ID] = node
		}
		if len(found) > 0 {
			result.anchor = row.Turn
		}
		result.indexed = append(result.indexed, row.Position)
	}

	result.nodes = make([]knowledge.WikiNode, 0, len(byID))
	for _, id := range slices.Sorted(maps.Keys(byID)) {
		result.nodes = append(result.nodes, byID[id])
	}
	return result, nil
}

func (e *Engine) extractLocalWikiSource(ctx context.Context, job *projectDistillWork, row store.WikiScanSource, source knowledge.Source) ([]knowledge.WikiNode, bool, error) {
	part := row.Part
	prefix := strings.TrimSpace(part.Text)
	structured := len(part.Text) < part.TotalBytes &&
		(prefix == "" || strings.HasPrefix(prefix, "{") || strings.HasPrefix(prefix, "["))
	if structured {
		if part.TotalBytes > wikiLocalStructuredBytes {
			// A fragment can look like prose and admit command/query
			// fields. Keep the original record in the semantic queue.
			return nil, true, nil
		}
		full, err := e.store.ReadWikiSourceText(ctx, job.result.ReviewID, row.Position, 0, part.TotalBytes)
		if err != nil {
			return nil, false, err
		}
		if len(full.Text) != part.TotalBytes || full.MessageSeq != part.MessageSeq || !utf8.ValidString(full.Text) {
			return nil, false, store.ErrHistorySourceUnavailable
		}
		return localWikiNodes(full.Text, source), false, nil
	}
	var nodes []knowledge.WikiNode
	for {
		if err := context.Cause(ctx); err != nil {
			return nil, false, err
		}
		nodes = append(nodes, localWikiNodes(part.Text, source)...)
		cursor := part.Offset + len(part.Text)
		if cursor >= part.TotalBytes {
			return nodes, false, nil
		}
		segment := *job
		progress := *job.result.Progress
		segment.result.Progress = &progress
		progress.SegmentTotalBytes = part.TotalBytes
		more, err := e.distillationFullText(ctx, &segment, row.Position, "wiki", cursor, wikiLocalSourceBytes)
		if err != nil {
			return nil, false, err
		}
		if more.Offset+len(more.Text) <= cursor || more.Offset > cursor {
			return nil, false, fmt.Errorf("%w: wiki source stopped at byte %d", store.ErrHistorySourceUnavailable, cursor)
		}
		part = more
	}
}

func localWikiNodes(text string, source knowledge.Source) []knowledge.WikiNode {
	nodes := knowledge.ExtractWikiObservations(text, source)
	for i := range nodes {
		for j := range nodes[i].Evidence {
			nodes[i].Evidence[j].Local = true
		}
	}
	return nodes
}
