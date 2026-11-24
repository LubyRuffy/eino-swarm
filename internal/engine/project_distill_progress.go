package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/knowledge"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// DistillationProgress counts candidate historical records in the admitted
// snapshot. Support records such as tool calls do not count twice.
type DistillationProgress struct {
	Total             int  `json:"total"`
	Processed         int  `json:"processed"`
	Remaining         int  `json:"remaining"`
	Skipped           int  `json:"skipped"`
	Batches           int  `json:"batches"`
	CoverageKnown     bool `json:"coverage_known,omitempty"`
	Excerpted         int  `json:"excerpted,omitempty"`
	OversizedSkipped  int  `json:"oversized_skipped,omitempty"`
	SegmentBytes      int  `json:"segment_bytes,omitempty"`
	SegmentTotalBytes int  `json:"segment_total_bytes,omitempty"`
}

type distillationRoute struct {
	provider, model, effort string
}

var errDistillationPaused = errors.New("distillation paused by user")

func (r DistillationResult) hasPending() bool {
	return r.Progress != nil && (r.Progress.Remaining > 0 || (r.WikiGraph && r.State != "completed"))
}

type activeDistillation struct {
	id     string
	ctx    context.Context
	cancel context.CancelCauseFunc
}

func (a activeDistillation) showPause(result *DistillationResult) {
	if result.State == "running" && errors.Is(context.Cause(a.ctx), errDistillationPaused) {
		result.State = "pausing"
	}
}

func (e *Engine) controlDistillation(ctx context.Context, projectID string, req DistillationRequest) (DistillationResult, error) {
	done, err := e.beginAdmission()
	if err != nil {
		return DistillationResult{}, err
	}
	defer done()
	e.distillMu.Lock()
	defer e.distillMu.Unlock()
	if _, err := e.store.GetProject(projectID); err != nil {
		return DistillationResult{}, err
	}
	row, err := e.distillationReceipt(ctx, projectID, req.WikiGraph)
	if err != nil {
		return DistillationResult{}, err
	}
	if row == nil || (row.ID != req.ResumeReviewID && row.ID != req.PauseReviewID) {
		return DistillationResult{}, store.ErrNotFound
	}
	var result DistillationResult
	if err := json.Unmarshal(row.Receipt, &result); err != nil {
		return DistillationResult{}, err
	}
	active := e.distilling[distillationKey(projectID, req.WikiGraph)]
	if active.id == row.ID {
		if req.PauseReviewID != "" && result.State == "running" {
			active.cancel(errDistillationPaused)
		}
		active.showPause(&result)
		return result, nil
	}
	if req.PauseReviewID != "" || !result.hasPending() {
		return e.projectDistillationStatus(ctx, projectID, req.WikiGraph)
	}
	if req.WikiGraph && !e.cfg.SwarmSnapshot().Knowledge {
		return DistillationResult{}, ErrDistillationTargets
	}
	kb, err := e.ProjectKnowledge(projectID)
	if err != nil {
		return DistillationResult{}, err
	}
	gate := e.reviews.begin(projectID)
	if gate == nil {
		return DistillationResult{}, ErrRestarting
	}
	result.State, result.Error = "running", ""
	result.ErrorCode, result.ErrorProviderID = "", ""
	row.Receipt, err = json.Marshal(result)
	if err == nil {
		err = e.store.ResumeDistillation(ctx, *row, row.State)
	}
	if err != nil {
		e.reviews.done()
		return DistillationResult{}, err
	}
	e.launchDistillation(gate, projectDistillWork{projectID: projectID, result: result, kb: kb,
		route: distillationRoute{row.ProviderID, row.Model, row.ReasoningEffort}}, row.CursorEventID)
	return result, nil
}

// Only one accepted proposal can advance this checkpoint. Before the first
// file write the receipt records the unresolved attempt, so a process loss
// between filesystem publication and SQLite commit is visible on resumption.
type distillCheckpoint struct {
	e                               *Engine
	current                         *DistillationResult
	projectID                       string
	from, to                        uint
	count                           int
	skipped                         int
	excerpted, oversizedSkipped     int
	segmentBytes, segmentTotalBytes int
	accepted                        bool
	wikiReviewed                    []uint
}

func (c *distillCheckpoint) save(result DistillationResult, cursor uint, wikiReviewed ...uint) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	row, err := result.row(c.projectID)
	if err != nil {
		return err
	}
	row.CursorEventID = cursor
	if len(wikiReviewed) > 0 {
		return c.e.store.CheckpointWikiReview(ctx, row, c.from, wikiReviewed)
	}
	return c.e.store.CheckpointDistillation(ctx, row, c.from)
}

func (c *distillCheckpoint) beforeWrite() error {
	result := *c.current
	result.Uncertain = true
	if err := c.save(result, c.from); err != nil {
		return err
	}
	*c.current = result
	return nil
}

func (c *distillCheckpoint) accept(batch DistillationResult, changes []memory.Change, reviewed bool) error {
	result := *c.current
	progress := *result.Progress
	progress.Processed += c.count
	progress.Remaining -= c.count
	progress.Skipped += c.skipped
	if progress.CoverageKnown {
		progress.Excerpted += c.excerpted
		progress.OversizedSkipped += c.oversizedSkipped
	}
	progress.SegmentBytes, progress.SegmentTotalBytes = c.segmentBytes, c.segmentTotalBytes
	progress.Batches++
	result.Progress = &progress
	result.ThreadID, result.TurnID = batch.ThreadID, batch.TurnID
	result.HistoryOmitted = result.HistoryOmitted || batch.HistoryOmitted
	result.Uncertain = result.Uncertain && !reviewed
	result.Changes = append(slices.Clone(result.Changes), changes...)
	result.Knowledge = append(slices.Clone(result.Knowledge), batch.Knowledge...)
	if progress.Remaining == 0 && !result.WikiGraph {
		result.State = "completed"
	}
	var wikiReviewed []uint
	if reviewed {
		wikiReviewed = c.wikiReviewed
	}
	if err := c.save(result, c.to, wikiReviewed...); err != nil {
		return err
	}
	*c.current = result
	c.accepted = true
	return nil
}

func (e *Engine) runDistillationQueue(ctx context.Context, gate chan struct{}, job *projectDistillWork, cursor uint) {
	for job.result.Progress.Remaining > 0 {
		select {
		case gate <- struct{}{}:
		case <-ctx.Done():
			job.result.State = "interrupted"
			return
		}
		next, advanced := func() (uint, bool) {
			defer func() { <-gate }()
			return e.runDistillationSlice(ctx, job, cursor)
		}()
		if !advanced {
			return
		}
		cursor = next
	}
}

func (e *Engine) runDistillationSlice(ctx context.Context, job *projectDistillWork, cursor uint) (uint, bool) {
	// Validate the originally advertised Trace anchor before the first call.
	if cursor == 0 && job.anchor.ID != "" {
		if err := e.checkDistillationAnchor(ctx, job); err != nil {
			stopDistillation(ctx, &job.result, err)
			return cursor, false
		}
	}
	batch, err := e.loadDistillationSlice(ctx, job, cursor, reviewMaxChars)
	if err != nil {
		stopDistillation(ctx, &job.result, err)
		return cursor, false
	}
	checkpoint := batch.checkpoint
	if len(batch.sources.sources) == 0 {
		if err := checkpoint.accept(batch.result, nil, false); err != nil {
			stopDistillation(ctx, &job.result, err)
			return cursor, false
		}
		return checkpoint.to, true
	}
	if err := e.validateDistillationDestinations(job.projectID, job.kb, job.result); err != nil {
		stopDistillation(ctx, &job.result, err)
		return cursor, false
	}
	e.reviewProjectDistillation(ctx, batch)
	if checkpoint.accepted {
		return checkpoint.to, true
	}
	job.result.State, job.result.Error = batch.result.State, batch.result.Error
	job.result.ErrorCode, job.result.ErrorProviderID = batch.result.ErrorCode, batch.result.ErrorProviderID
	job.result.Changes = append(job.result.Changes, batch.result.Changes...)
	job.result.Knowledge = append(job.result.Knowledge, batch.result.Knowledge...)
	if len(batch.result.Changes)+len(batch.result.Knowledge) > 0 {
		job.result.State = "partial"
	}
	return cursor, false
}

func (e *Engine) validateDistillationDestinations(projectID string, kb *knowledge.Store, result DistillationResult) error {
	project, err := e.store.GetProject(projectID)
	if err != nil {
		return err
	}
	if result.MemoryEnabled && !e.MemoryEnabled(project) {
		return errors.New("Memory was disabled during distillation")
	}
	if result.KnowledgeEnabled {
		if !e.cfg.SwarmSnapshot().Knowledge {
			return knowledge.ErrDisabled
		}
		snapshot, err := kb.Snapshot()
		if err != nil {
			return err
		}
		if !snapshot.Enabled() {
			return knowledge.ErrDisabled
		}
	}
	return nil
}

func stopDistillation(ctx context.Context, result *DistillationResult, err error) {
	if ctx.Err() != nil {
		result.State = "interrupted"
		return
	}
	result.State, result.Error = "failed", publicTurnError(err)
}

func (e *Engine) loadDistillationSlice(ctx context.Context, job *projectDistillWork, cursor uint, maxBytes int) (*projectDistillWork, error) {
	load := e.store.DistillationWindow
	if job.result.FullText {
		load = e.store.DistillationMetadataWindow
	}
	if job.result.WikiGraph {
		load = e.store.WikiReviewMetadataWindow
	}
	rows, err := load(ctx, job.result.ReviewID, cursor, 128)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("distillation history checkpoint has %d records remaining but no saved source", job.result.Progress.Remaining)
	}
	if job.result.FullText {
		return e.distillationFullSlice(ctx, job, cursor, rows, maxBytes)
	}
	return e.distillationSliceFromRows(ctx, job, cursor, rows)
}
