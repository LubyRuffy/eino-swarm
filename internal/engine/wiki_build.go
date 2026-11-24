package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/knowledge"
)

const wikiExtractParallelism = 6
const wikiExtractBytes = 8 * 1024
const wikiMinimumExtractBytes = 2 * 1024
const wikiDocumentBatch = 64

type wikiDraftResult struct {
	proposal *wikiProposal
	code     string
	err      error
}

func (e *Engine) runWikiBuild(ctx context.Context, gate chan struct{}, job *projectDistillWork, cursor uint) {
	if !job.result.WikiScanComplete && job.result.WikiPhase != "documents" {
		if err := e.runWikiLocalScan(ctx, job, cursor); err != nil {
			stopDistillation(ctx, &job.result, err)
			return
		}
	}
	if err := e.renderAvailableWikiDocuments(ctx, job, cursor); err != nil {
		stopDistillation(ctx, &job.result, err)
		return
	}
	maxBytes, parallelism := wikiExtractBytes, wikiExtractParallelism
	if job.result.Progress.Batches == 0 {
		// Publish a small first batch before starting bulk requests.
		maxBytes, parallelism = wikiMinimumExtractBytes, 1
	}
	for job.result.Progress.Remaining > 0 {
		select {
		case gate <- struct{}{}:
		case <-ctx.Done():
			job.result.State = "interrupted"
			return
		}
		next, retryBytes, err := func() (uint, int, error) {
			defer func() { <-gate }()
			return e.runWikiWindow(ctx, job, cursor, maxBytes, parallelism)
		}()
		cursor = next
		if retryBytes > 0 && ctx.Err() == nil {
			maxBytes, parallelism = retryBytes, 1
			continue
		}
		if err != nil {
			stopDistillation(ctx, &job.result, err)
			return
		}
		if err := e.renderAvailableWikiDocuments(ctx, job, cursor); err != nil {
			stopDistillation(ctx, &job.result, err)
			return
		}
		maxBytes, parallelism = wikiExtractBytes, wikiExtractParallelism
	}
	e.renderWikiBuild(ctx, gate, job, cursor)
}

// Plan independent model inputs with a temporary cursor. Publication and
// durable progress still follow source order. No window can commit past an
// unaccepted batch, and cancellation joins its in-flight reviewers.
func (e *Engine) planWikiWindow(ctx context.Context, job *projectDistillWork, cursor uint, maxBytes, parallelism int) ([]*projectDistillWork, error) {
	plan := *job
	progress := *plan.result.Progress
	plan.result.Progress = &progress
	var batches []*projectDistillWork
	for len(batches) < parallelism && progress.Remaining > 0 {
		batch, err := e.loadDistillationSlice(ctx, &plan, cursor, maxBytes)
		if err != nil {
			return nil, err
		}
		batches = append(batches, batch)
		next := batch.checkpoint
		cursor = next.to
		progress.Remaining -= next.count
		progress.SegmentBytes, progress.SegmentTotalBytes = next.segmentBytes, next.segmentTotalBytes
	}
	return batches, nil
}

func (e *Engine) runWikiWindow(ctx context.Context, job *projectDistillWork, cursor uint, maxBytes, parallelism int) (uint, int, error) {
	if err := e.validateDistillationDestinations(job.projectID, job.kb, job.result); err != nil {
		return cursor, 0, err
	}
	batches, err := e.planWikiWindow(ctx, job, cursor, maxBytes, parallelism)
	if err != nil {
		return cursor, 0, err
	}
	windowCtx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() {
		cancel()
		workers.Wait()
	}()
	results := make([]chan wikiDraftResult, len(batches))
	for i, batch := range batches {
		results[i] = make(chan wikiDraftResult, 1)
		if len(batch.sources.sources) == 0 {
			results[i] <- wikiDraftResult{}
			continue
		}
		result := results[i]
		workers.Go(func() {
			proposal, code, err := e.extractWikiDraft(windowCtx, batch)
			if err != nil && windowCtx.Err() == nil &&
				(code == "temporary_model" || (code == "evidence" && len(batch.sources.transcript) <= wikiMinimumExtractBytes)) {
				proposal, code, err = e.extractWikiDraft(windowCtx, batch)
			}
			result <- wikiDraftResult{proposal, code, err}
		})
	}
	for i, batch := range batches {
		select {
		case <-windowCtx.Done():
			return cursor, 0, context.Cause(ctx)
		case draft := <-results[i]:
			if draft.err != nil {
				if smaller := wikiRetryBudget(draft.code, len(batch.sources.transcript)); smaller > 0 {
					return cursor, smaller, draft.err
				}
				job.result.ErrorCode, job.result.ErrorProviderID = draft.code, job.route.provider
				return cursor, 0, draft.err
			}
			batch.checkpoint.current = &job.result
			if err := e.publishWikiDraft(ctx, batch, draft.proposal); err != nil {
				return cursor, 0, err
			}
			cursor = batch.checkpoint.to
		}
	}
	return cursor, 0, nil
}

// Re-read the uncommitted source from the same cursor and byte offset.
// Smaller inputs reduce model output pressure without losing any evidence.
func wikiRetryBudget(code string, size int) int {
	if size <= wikiMinimumExtractBytes {
		return 0
	}
	switch code {
	case "evidence", "context_overflow", "iteration_limit", "agent_timeout":
		return max(wikiMinimumExtractBytes, size/2)
	default:
		return 0
	}
}

func (e *Engine) publishWikiDraft(ctx context.Context, batch *projectDistillWork, proposal *wikiProposal) error {
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
	if !e.cfg.SwarmSnapshot().Knowledge {
		return knowledge.ErrDisabled
	}
	if proposal == nil || len(*proposal.Nodes) == 0 {
		return batch.checkpoint.accept(batch.result, nil, proposal != nil)
	}
	if err := e.checkDistillationAnchor(ctx, batch); err != nil {
		return err
	}
	handles := wikiSourceHandles(*proposal)
	refs, err := e.verifyDistillationSources(ctx, batch, handles)
	if err != nil {
		return err
	}
	ordered := make([]knowledgeSourceRef, 0, len(handles))
	for _, handle := range handles {
		ordered = append(ordered, refs[handle])
	}
	resolved, err := e.resolveKnowledgeSources(ctx, batch.projectID, ordered)
	if err != nil {
		return err
	}
	sources := make(map[int64]knowledge.Source, len(handles))
	for i, handle := range handles {
		sources[handle] = resolved[i]
	}
	pages := wikiPages(*proposal, sources)
	if err := batch.checkpoint.beforeWrite(); err != nil {
		return err
	}
	if err := batch.kb.EnsureWikiSchema(); err != nil {
		return err
	}
	stamp := knowledge.Stamp{UpdatedAt: time.Now(), UpdatedTurn: batch.anchor.ID, UpdatedThread: batch.anchor.ThreadID}
	if err := batch.kb.StageWikiNodes(ctx, batch.result.ReviewID, pages, stamp); err != nil {
		return err
	}
	return batch.checkpoint.accept(batch.result, nil, true)
}

// Publish a bounded group of already verified observations before waiting
// on the next model window. Final document coverage is measured after review.
func (e *Engine) renderAvailableWikiDocuments(ctx context.Context, job *projectDistillWork, cursor uint) error {
	if !job.result.WikiScanComplete || job.result.WikiPhase != "graph" || job.result.Progress.Remaining == 0 {
		return nil
	}
	checkpoint := &distillCheckpoint{e: e, current: &job.result, from: cursor, projectID: job.projectID}
	return e.renderWikiDocumentBatch(ctx, job, checkpoint, cursor, false)
}

func (e *Engine) renderWikiBuild(ctx context.Context, gate chan struct{}, job *projectDistillWork, cursor uint) {
	checkpoint := &distillCheckpoint{e: e, current: &job.result, from: cursor, projectID: job.projectID}
	for {
		select {
		case gate <- struct{}{}:
		case <-ctx.Done():
			job.result.State = "interrupted"
			return
		}
		err := func() error {
			defer func() { <-gate }()
			return e.renderWikiDocumentBatch(ctx, job, checkpoint, cursor, true)
		}()
		if err != nil {
			stopDistillation(ctx, &job.result, err)
			return
		}
		if job.result.State == "completed" {
			return
		}
	}
}

func (e *Engine) renderWikiDocumentBatch(ctx context.Context, job *projectDistillWork, checkpoint *distillCheckpoint, cursor uint, finalPass bool) error {
	done, err := e.beginLifecycleAdmission()
	if err != nil {
		return err
	}
	defer done()
	e.workspaceMu.Lock()
	defer e.workspaceMu.Unlock()
	if !e.cfg.SwarmSnapshot().Knowledge {
		return knowledge.ErrDisabled
	}
	if _, err := e.store.GetProject(job.projectID); err != nil {
		return err
	}
	snap, err := job.kb.Snapshot()
	if err != nil {
		return err
	}
	if !snap.Enabled() {
		return knowledge.ErrDisabled
	}
	result := job.result
	total, written := snap.WikiDocumentProgress(result.ReviewID)
	if !finalPass && written == total && total == result.WikiDocuments && written == result.WikiDocumented {
		return nil
	}
	if finalPass && result.WikiPhase == "graph" {
		// Freeze the final document denominator after source review.
		result.WikiPhase, result.WikiDocuments, result.WikiDocumented = "documents", total, written
	} else if written < total {
		if err := checkpoint.beforeWrite(); err != nil {
			return err
		}
		if err := job.kb.WriteWikiDocuments(ctx, result.ReviewID, wikiDocumentBatch); err != nil {
			return err
		}
		snap, err = job.kb.Snapshot()
		if err != nil {
			return err
		}
		total, written = snap.WikiDocumentProgress(result.ReviewID)
	}
	if finalPass && total != result.WikiDocuments {
		return fmt.Errorf("wiki graph changed during document publication: %d of %d pages remain associated with this run", total, result.WikiDocuments)
	}
	result.WikiDocuments, result.WikiDocumented = total, written
	if finalPass && written == total {
		result.State = "completed"
	}
	if err := checkpoint.save(result, cursor); err != nil {
		return err
	}
	job.result = result
	return nil
}
