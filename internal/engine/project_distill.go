package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/knowledge"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/tool"
)

// DistillationRequest selects the saved history for one manual project review.
type DistillationRequest struct {
	Scope           string `json:"scope"`
	ThreadID        string `json:"thread_id"`
	EnableKnowledge bool   `json:"enable_knowledge"`
	FullText        bool   `json:"full_text"`
	WikiGraph       bool   `json:"wiki_graph"`
	ResumeReviewID  string `json:"resume_review_id"`
	PauseReviewID   string `json:"pause_review_id"`
}

// ErrDistillationScope reports an invalid combination of scope and conversation.
var ErrDistillationScope = errors.New("invalid distillation scope or conversation")

// ErrDistillationTargets reports that none of the requested project's stores is enabled.
var ErrDistillationTargets = errors.New("enable Memory or Knowledge before distilling")

// ErrDistillationPending protects the existing checkpoint from a new scope.
var ErrDistillationPending = errors.New("continue the incomplete distillation before starting another")

// DistillationResult is the durable status shared by both project panels.
type DistillationResult struct {
	ReviewID          string                `json:"review_id,omitempty"`
	State             string                `json:"state"`
	Scope             string                `json:"scope,omitempty"`
	FullText          bool                  `json:"full_text,omitempty"`
	WikiGraph         bool                  `json:"wiki_graph,omitempty"`
	WikiPhase         string                `json:"wiki_phase,omitempty"`
	WikiScanCursor    uint                  `json:"wiki_scan_cursor,omitempty"`
	WikiScanProcessed int                   `json:"wiki_scan_processed,omitempty"`
	WikiScanSkipped   int                   `json:"wiki_scan_skipped,omitempty"`
	WikiScanDeferred  int                   `json:"wiki_scan_deferred,omitempty"`
	WikiScanComplete  bool                  `json:"wiki_scan_complete,omitempty"`
	WikiDocuments     int                   `json:"wiki_documents,omitempty"`
	WikiDocumented    int                   `json:"wiki_documented,omitempty"`
	ThreadID          string                `json:"thread_id,omitempty"`
	TurnID            string                `json:"turn_id,omitempty"`
	Progress          *DistillationProgress `json:"progress,omitempty"`
	HistoryOmitted    bool                  `json:"history_omitted,omitempty"`
	MemoryEnabled     bool                  `json:"memory_enabled,omitempty"`
	KnowledgeEnabled  bool                  `json:"knowledge_enabled,omitempty"`
	Changes           []memory.Change       `json:"changes,omitempty"`
	Knowledge         []distillChange       `json:"knowledge,omitempty"`
	Error             string                `json:"error,omitempty"`
	ErrorCode         string                `json:"error_code,omitempty"`
	ErrorProviderID   string                `json:"error_provider_id,omitempty"`
	Uncertain         bool                  `json:"uncertain,omitempty"`
}

type distillChange struct {
	Action   string `json:"action"`
	ID       string `json:"id,omitempty"`
	Title    string `json:"title,omitempty"`
	Revision string `json:"revision"`
}

type projectDistillWork struct {
	projectID  string
	result     DistillationResult
	anchor     store.Turn
	route      distillationRoute
	sources    distillInput
	kb         *knowledge.Store
	checkpoint *distillCheckpoint
}

// ProjectDistillationStatus reads the last explicit review without starting work.
func (e *Engine) ProjectDistillationStatus(ctx context.Context, projectID string) (DistillationResult, error) {
	return e.projectReviewStatus(ctx, projectID, false)
}

func (e *Engine) ProjectWikiBuildStatus(ctx context.Context, projectID string) (DistillationResult, error) {
	return e.projectReviewStatus(ctx, projectID, true)
}

func (e *Engine) projectReviewStatus(ctx context.Context, projectID string, wikiGraph bool) (DistillationResult, error) {
	e.distillMu.Lock()
	defer e.distillMu.Unlock()
	if _, err := e.store.GetProject(projectID); err != nil {
		return DistillationResult{}, err
	}
	return e.projectDistillationStatus(ctx, projectID, wikiGraph)
}

func distillationKey(projectID string, wikiGraph bool) string {
	if wikiGraph {
		return "wiki:" + projectID
	}
	return projectID
}

func (e *Engine) distillationReceipt(ctx context.Context, projectID string, wikiGraph bool) (*store.ProjectDistillation, error) {
	if wikiGraph {
		return e.store.ProjectWikiBuildReceipt(ctx, projectID)
	}
	return e.store.ProjectDistillationReceipt(ctx, projectID)
}

func (e *Engine) projectDistillationStatus(ctx context.Context, projectID string, wikiGraph bool) (DistillationResult, error) {
	row, err := e.distillationReceipt(ctx, projectID, wikiGraph)
	if err != nil {
		return DistillationResult{}, err
	}
	if row == nil {
		return DistillationResult{State: "idle"}, nil
	}
	var result DistillationResult
	if err := json.Unmarshal(row.Receipt, &result); err != nil {
		return DistillationResult{}, err
	}
	if result.State == "running" {
		active := e.distilling[distillationKey(projectID, wikiGraph)]
		if active.id != row.ID {
			result.State = "interrupted"
			result.Uncertain = result.Uncertain || result.Progress == nil
		} else {
			active.showPause(&result)
		}
	}
	if result.ThreadID != "" {
		thread, err := e.store.GetThreadContext(ctx, result.ThreadID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return DistillationResult{}, err
		}
		turn, err := e.store.GetTurnContext(ctx, result.TurnID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return DistillationResult{}, err
		}
		if thread == nil || thread.ProjectID != projectID || turn == nil || turn.ThreadID != thread.ID {
			result.ThreadID, result.TurnID = "", ""
		}
	}
	return result, nil
}

// StartProjectDistillation admits at most one manual review per project.
func (e *Engine) StartProjectDistillation(ctx context.Context, projectID string, req DistillationRequest) (DistillationResult, error) {
	if req.ResumeReviewID != "" || req.PauseReviewID != "" {
		if req.Scope != "" || req.ThreadID != "" || req.EnableKnowledge || req.FullText || (req.ResumeReviewID != "" && req.PauseReviewID != "") {
			return DistillationResult{}, ErrDistillationScope
		}
		return e.controlDistillation(ctx, projectID, req)
	}
	switch req.Scope {
	case "latest_turn", "thread":
		if req.ThreadID == "" || (req.Scope == "latest_turn" && (req.FullText || req.WikiGraph)) {
			return DistillationResult{}, ErrDistillationScope
		}
	case "project":
		if req.ThreadID != "" {
			return DistillationResult{}, ErrDistillationScope
		}
	default:
		return DistillationResult{}, ErrDistillationScope
	}
	done, err := e.beginAdmission()
	if err != nil {
		return DistillationResult{}, err
	}
	defer done()
	e.distillMu.Lock()
	defer e.distillMu.Unlock()
	e.workspaceMu.Lock()
	defer e.workspaceMu.Unlock()

	p, err := e.store.GetProject(projectID)
	if err != nil {
		return DistillationResult{}, err
	}
	if req.ThreadID != "" {
		thread, err := e.store.GetThreadContext(ctx, req.ThreadID)
		if err != nil {
			return DistillationResult{}, err
		}
		if thread.ProjectID != projectID {
			return DistillationResult{}, store.ErrNotFound
		}
	}
	if e.distilling[distillationKey(projectID, req.WikiGraph)].id != "" {
		return e.projectDistillationStatus(ctx, projectID, req.WikiGraph)
	}
	gate := e.reviews.begin(projectID)
	if gate == nil {
		return DistillationResult{}, ErrRestarting
	}
	launched := false
	defer func() {
		if !launched {
			e.reviews.done()
		}
	}()
	last, err := e.projectDistillationStatus(ctx, projectID, req.WikiGraph)
	if err != nil {
		return DistillationResult{}, err
	}
	if last.hasPending() {
		return DistillationResult{}, ErrDistillationPending
	}

	var anchor store.Turn
	var input distillInput
	if req.Scope == "latest_turn" {
		turns, omitted, err := e.store.ProjectDistillationHistory(ctx, projectID, req.ThreadID, true, distillEventKinds)
		if err != nil {
			return DistillationResult{}, err
		}
		input = selectDistillationSources(turns, omitted)
		if len(input.sources) == 0 {
			return DistillationResult{State: "no_work", Scope: req.Scope, HistoryOmitted: input.omitted}, nil
		}
		anchor = turns[0].Turn
	} else {
		anchor, err = e.store.ProjectDistillationAnchor(ctx, projectID, req.ThreadID)
		if err != nil {
			return DistillationResult{}, err
		}
		if anchor.ID == "" {
			return DistillationResult{State: "no_work", Scope: req.Scope}, nil
		}
	}
	kb, err := e.ProjectKnowledge(projectID)
	if err != nil {
		return DistillationResult{}, err
	}
	var knowledgeEnabled bool
	if e.cfg.SwarmSnapshot().Knowledge {
		snap, err := kb.Snapshot()
		if err != nil {
			return DistillationResult{}, err
		}
		if (req.EnableKnowledge || req.WikiGraph) && !snap.Enabled() {
			snap, err = e.enableKnowledgeFiles(ctx, projectID, kb)
			if err != nil {
				return DistillationResult{}, err
			}
		}
		knowledgeEnabled = snap.Enabled()
	} else if req.EnableKnowledge || req.WikiGraph {
		return DistillationResult{}, ErrDistillationTargets
	}
	result := DistillationResult{
		ReviewID: store.NewID("distill_"), State: "running", Scope: req.Scope, FullText: req.FullText || req.WikiGraph, WikiGraph: req.WikiGraph,
		ThreadID: anchor.ThreadID, TurnID: anchor.ID, HistoryOmitted: input.omitted,
		MemoryEnabled: !req.WikiGraph && e.MemoryEnabled(p), KnowledgeEnabled: knowledgeEnabled,
	}
	if req.WikiGraph {
		result.WikiPhase = "scan"
	}
	if !result.MemoryEnabled && !result.KnowledgeEnabled {
		return DistillationResult{}, ErrDistillationTargets
	}
	work := projectDistillWork{projectID: projectID, result: result, anchor: anchor, route: distillationRoute{anchor.ProviderID, anchor.Model, anchor.ReasoningEffort}, sources: input, kb: kb}
	if req.Scope == "latest_turn" {
		row, err := result.row(projectID)
		if err == nil {
			err = e.store.StartProjectDistillation(ctx, row)
		}
		if err != nil {
			return DistillationResult{}, err
		}
	} else {
		row := store.ProjectDistillation{ProjectID: projectID, ID: result.ReviewID, State: result.State,
			ProviderID: anchor.ProviderID, Model: anchor.Model, ReasoningEffort: anchor.ReasoningEffort, WikiGraph: req.WikiGraph}
		_, err := e.store.StartDistillationQueue(ctx, row, req.ThreadID, func(total int) ([]byte, error) {
			result.Progress = &DistillationProgress{Total: total, Remaining: total, CoverageKnown: true}
			return json.Marshal(result)
		})
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return DistillationResult{State: "no_work", Scope: req.Scope}, nil
			}
			return DistillationResult{}, err
		}
		work.result = result
	}
	launched = true
	e.launchDistillation(gate, work, 0)
	return result, nil
}

func (e *Engine) launchDistillation(gate chan struct{}, work projectDistillWork, cursor uint) {
	ctx, cancel := context.WithCancelCause(e.reviews.dreamCtx)
	if e.distilling == nil {
		e.distilling = make(map[string]activeDistillation)
	}
	e.distilling[distillationKey(work.projectID, work.result.WikiGraph)] = activeDistillation{id: work.result.ReviewID, ctx: ctx, cancel: cancel}
	go e.runProjectDistillation(ctx, cancel, gate, work, cursor)
}

func (r DistillationResult) row(projectID string) (store.ProjectDistillation, error) {
	body, err := json.Marshal(r)
	return store.ProjectDistillation{ProjectID: projectID, ID: r.ReviewID, State: r.State, Receipt: body, WikiGraph: r.WikiGraph}, err
}

func (e *Engine) runProjectDistillation(ctx context.Context, cancel context.CancelCauseFunc, gate chan struct{}, work projectDistillWork, cursor uint) {
	defer e.reviews.done()
	defer cancel(nil)
	defer func() {
		if recovered := recover(); recovered != nil {
			e.log.Error("project distillation panicked", "review", work.result.ReviewID, "panic", recovered)
			work.result.State, work.result.Uncertain = "interrupted", true
		} else if work.result.State == "interrupted" && errors.Is(context.Cause(ctx), errDistillationPaused) {
			work.result.State, work.result.Error = "paused", ""
			work.result.ErrorCode, work.result.ErrorProviderID = "", ""
		}
		e.distillMu.Lock()
		defer e.distillMu.Unlock()
		delete(e.distilling, distillationKey(work.projectID, work.result.WikiGraph))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		row, err := work.result.row(work.projectID)
		if err == nil {
			err = e.store.FinishProjectDistillation(ctx, row)
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			e.log.Error("could not retain project distillation receipt", "review", work.result.ReviewID, "err", err)
		}
	}()
	if work.result.WikiGraph {
		e.runWikiBuild(ctx, gate, &work, cursor)
		return
	}
	if work.result.Progress != nil {
		e.runDistillationQueue(ctx, gate, &work, cursor)
		return
	}
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		work.result.State = "interrupted"
		return
	}
	e.reviewProjectDistillation(ctx, &work)
}

func (e *Engine) reviewDistillationAttempt(ctx context.Context, work *projectDistillWork, retry bool) bool {
	if err := e.checkDistillationAnchor(ctx, work); err != nil {
		stopDistillation(ctx, &work.result, err)
		return false
	}
	var mu sync.Mutex
	var outcome reviewOutcome
	batch := &projectDistillBatch{e: e, work: work, mu: &mu, outcome: &outcome}
	pc := &projectContext{}
	var notes *memory.Snapshot
	var tools []tool.BaseTool
	if work.result.MemoryEnabled {
		pc.memory = e.ProjectMemory(work.projectID)
		snapshot, err := pc.memory.Read()
		if err != nil {
			work.result.State, work.result.Error = "failed", publicTurnError(err)
			return false
		}
		notes = &snapshot
		batch.memory = &reviewBatchTool{store: pc.memory, notesRev: notes.Rev, sources: work.sources.memory, mu: &mu, outcome: &outcome}
		tools = memory.ViewTools(pc.memory)
	}
	instruction := projectDistillPrompt
	if retry {
		instruction += "\nThis is a fresh attempt at the same batch. Cross-check every citation's review seq and quote against visible source text before submitting. Resubmit only supported operations."
	}
	if work.checkpoint != nil {
		instruction += "\nThis is one batch of a larger history review. Judge only the sources in this batch. Read current destinations before proposing an update because earlier batches may already have recorded related material."
		if work.result.FullText {
			instruction += "\nA record can span multiple batches. The byte_offset labels the position in the original record and is not evidence. Cite exact text from the visible segment. Preserve decisions and citations from earlier segments when updating an existing entry."
		}
		if work.result.Uncertain {
			instruction += "\nA previous attempt may have saved part of this batch. Inspect all enabled destinations before writing and propose only the changes still needed."
		}
	}
	if work.result.MemoryEnabled {
		instruction += "\nMemory is enabled. Use \"changes\" for both notes and Skills."
		if work.result.KnowledgeEnabled {
			instruction += "\nKeep subject-matter knowledge in the wiki."
		}
	}
	if work.result.KnowledgeEnabled {
		instruction += "\nKnowledge is enabled. Use \"knowledge\" for wiki mutations. Search existing pages and inspect schema first. Read page or schema revisions before editing. Wiki history references come only from proposal evidence."
		tools = append(tools, batch.knowledgeReader())
	}
	settings := e.cfg.Snapshot().Memory
	effort, optional := settings.ReviewEffort(work.route.effort)
	e.driveReviewer(reviewRun{
		ctx: ctx, threadID: work.anchor.ThreadID, turnID: work.anchor.ID, recorder: e.distillationRecorder(work),
		providerID: work.route.provider, model: work.route.model, effort: effort, optionalEffort: optional,
		description: "distills saved project history", instruction: instruction,
		userMsg: e.reviewUserMessageWithNotes(pc, work.sources.transcript, notes),
		maxIter: settings.ReviewIterations(), tools: append(tools, batch), sequential: true,
		completed: func() (string, bool) {
			mu.Lock()
			defer mu.Unlock()
			return outcome.Note, batch.complete
		},
	}, &mu, &outcome)
	// A cancelled model iterator can return while its tool is still unwinding.
	// Join the publisher before completing or allowing the job to resume.
	batch.publishMu.Lock()
	batch.closed = true
	batch.publishMu.Unlock()
	mu.Lock()
	defer mu.Unlock()
	work.result.Changes = outcome.Changes
	if outcome.Err != "" && work.result.Error == "" {
		work.result.Error = outcome.Err
	}
	switch {
	case !batch.complete && ctx.Err() != nil:
		work.result.State, work.result.Error = "interrupted", ""
	case !batch.complete && work.result.Error == "":
		work.result.State, work.result.Error = "failed", "No complete distillation proposal was accepted."
		if batch.lastRefusal != "" {
			work.result.Error = batch.lastRefusal
		}
	case work.result.Error != "" && (len(work.result.Changes) > 0 || len(work.result.Knowledge) > 0):
		work.result.State = "partial"
	case work.result.Error != "":
		work.result.State = "failed"
	default:
		work.result.State = "completed"
	}
	if work.result.State != "failed" || batch.complete {
		return false
	}
	code, retryable := distillationFailure(outcome.failure, outcome.StopReason, batch.citationRefused)
	if code != "" {
		work.result.ErrorCode, work.result.ErrorProviderID = code, work.route.provider
	}
	return !batch.startedWrite && retryable
}

func (e *Engine) checkDistillationAnchor(ctx context.Context, work *projectDistillWork) error {
	thread, err := e.store.GetThreadContext(ctx, work.anchor.ThreadID)
	if err != nil {
		return err
	}
	turn, err := e.store.GetTurnContext(ctx, work.anchor.ID)
	if err != nil {
		return err
	}
	if thread.ProjectID != work.projectID || turn.ThreadID != thread.ID || turn.Status != work.anchor.Status {
		return fmt.Errorf("%w: the review's history changed", store.ErrHistorySourceUnavailable)
	}
	return nil
}

// A late provider completion cannot attribute a project review to a conversation
// after that conversation has moved. Rewind and deletion also have SQL guards.
func (e *Engine) distillationRecorder(work *projectDistillWork) provider.Recorder {
	record := e.callRecorder(work.anchor.ThreadID, work.anchor.ID)
	return func(call provider.CallRecord) {
		e.workspaceMu.Lock()
		defer e.workspaceMu.Unlock()
		if err := e.checkDistillationAnchor(context.Background(), work); err == nil {
			record(call)
		}
	}
}
