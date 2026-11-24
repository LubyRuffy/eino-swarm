package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/knowledge"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestWikiBuildHonorsKnowledgeSwitchAtAdmissionAndPublication(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var once sync.Once
	releaseModel := func() { once.Do(func() { close(release) }) }
	defer releaseModel()
	e, requests := distillWire(t, func(w http.ResponseWriter, req scheduleCompletionRequest, _ int) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-t.Context().Done():
			return
		}
		nuwaValidationTool(w, req, "switch", wikiExtractTool, `{"reviewed":[1],"nodes":[
			{"key":"subject","title":"Retained topic","kind":"entity","category":"topic",
			 "evidence":{"seq":1,"quote":"Retained topic was observed."}}],"links":[]}`)
	})
	p, err := e.CreateProject("Wiki switch", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := knowledgeFixtureThread(t, e, p.ID, "Original task")
	distillEvent(t, e, th, store.TurnDone, KindUser, "Retained topic was observed.")
	toggle := func(enabled bool) {
		t.Helper()
		if err := e.UpdateConfig(func(cfg *config.Config) error {
			cfg.Swarm.Knowledge = enabled
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	req := DistillationRequest{Scope: "thread", ThreadID: th.ID, WikiGraph: true}
	toggle(false)
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, req); !errors.Is(err, ErrDistillationTargets) || requests.Load() != 0 {
		t.Fatalf("disabled Knowledge admitted wiki extraction: %v, requests=%d", err, requests.Load())
	}
	toggle(true)
	started, err := e.StartProjectDistillation(t.Context(), p.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("model request did not arrive")
	}
	toggle(false)
	releaseModel()
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		status, err := e.ProjectWikiBuildStatus(t.Context(), p.ID)
		if err != nil {
			t.Fatal(err)
		}
		if status.State != "running" {
			if status.State != "failed" || status.Progress.Processed != 0 {
				t.Fatalf("wiki accepted a batch after Knowledge was disabled: %+v %+v", status, status.Progress)
			}
			if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{WikiGraph: true, ResumeReviewID: started.ReviewID}); !errors.Is(err, ErrDistillationTargets) {
				t.Fatalf("wiki resumed while Knowledge was disabled: %v", err)
			}
			toggle(true)
			kb, err := e.ProjectKnowledge(p.ID)
			if err != nil {
				t.Fatal(err)
			}
			snap, err := kb.Snapshot()
			if err != nil || snap.Status(time.Now()).PageCount != 0 {
				t.Fatalf("disabled Knowledge permitted publication: %+v %v", snap, err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("wiki build did not stop")
}

func TestWikiProposalRefusesIncompleteCoverageAndUnsupportedRelations(t *testing.T) {
	visible := map[int64]store.Event{
		1: {Text: "First participant entered the session."},
		2: {Text: "Second participant recorded the result."},
	}
	nodes := []wikiModelNode{
		{Key: "one", Title: "First participant", Kind: "entity", Category: "person",
			Evidence: reviewCitation{Seq: 1, Quote: "First participant entered the session."}},
		{Key: "two", Title: "Second participant", Kind: "entity", Category: "person",
			Evidence: reviewCitation{Seq: 2, Quote: "Second participant recorded the result."}},
	}
	links := []wikiModelLink{}
	reviewed := []int64{1}
	draft := wikiProposal{Reviewed: &reviewed, Nodes: &nodes, Links: &links}
	if err := draft.validate(visible); err == nil || !strings.Contains(err.Error(), "Missing source numbers") {
		t.Fatalf("an incomplete source review was accepted: %v", err)
	}
	reviewed = []int64{1, 2}
	links = append(links, wikiModelLink{From: "one", To: "two", Evidence: nodes[0].Evidence})
	if err := draft.validate(visible); err == nil || !strings.Contains(err.Error(), "links[0]") {
		t.Fatalf("unrelated source supplied an edge: %v", err)
	}
	links = links[:0]
	nodes[0].Identity = "Identity that is absent"
	if err := draft.validate(visible); err == nil || !strings.Contains(err.Error(), "nodes[0]") {
		t.Fatalf("invented identity was accepted: %v", err)
	}
	nodes[0].Identity = ""
	if err := draft.validate(visible); err != nil {
		t.Fatalf("independent attributable nodes were refused: %v", err)
	}
}

func TestWikiBuildRetriesIncompleteCoverageWithoutAdvancing(t *testing.T) {
	e, requests := distillWire(t, func(w http.ResponseWriter, req scheduleCompletionRequest, step int) {
		proposal := `{"reviewed":[],"nodes":[],"links":[]}`
		if step > 2 {
			proposal = `{"reviewed":[1],"nodes":[],"links":[]}`
		}
		nuwaValidationTool(w, req, fmt.Sprintf("coverage-%d", step), wikiExtractTool, proposal)
	})
	e.cfg.Memory.ReviewMaxIterations = 1
	p, err := e.CreateProject("Coverage", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := knowledgeFixtureThread(t, e, p.ID, "Original task")
	distillEvent(t, e, th, store.TurnDone, KindUser, "Source that contains no graph facts.")
	started, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "project", WikiGraph: true})
	if err != nil {
		t.Fatal(err)
	}
	resumed := false
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		r, err := e.ProjectWikiBuildStatus(t.Context(), p.ID)
		if err != nil {
			t.Fatal(err)
		}
		if r.State != "running" {
			if !resumed {
				if r.State != "failed" || r.ErrorCode != "evidence" || requests.Load() != 2 || r.Progress.Processed != 0 || r.Progress.Batches != 0 {
					t.Fatalf("exhausted evidence retries changed the saved position: %+v %+v requests=%d", r, r.Progress, requests.Load())
				}
				if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{
					ResumeReviewID: started.ReviewID, WikiGraph: true,
				}); err != nil {
					t.Fatal(err)
				}
				resumed = true
				continue
			}
			if r.State != "completed" || requests.Load() != 3 || r.Progress.Processed != 1 || r.Progress.Batches != 1 || r.WikiDocumented != 0 {
				t.Fatalf("an incomplete model batch was counted or not recovered: %+v %+v requests=%d", r, r.Progress, requests.Load())
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the incomplete graph batch was not settled")
}

func TestWikiBuildNarrowsARefusedBatchWithoutLosingSourceBytes(t *testing.T) {
	const beginning = "Start of observation. "
	const ending = " End of observation."
	source := beginning + strings.Repeat("原始记录。 ", 680) + ending
	framing := regexp.MustCompile(`(?m)^\[seq=1 kind=[^\n]+byte_offset=(\d+)\] human: `)
	var mu sync.Mutex
	covered := make([]bool, len(source))
	var refused int
	e, _ := distillWire(t, func(w http.ResponseWriter, r scheduleCompletionRequest, step int) {
		input := r.Messages[1].Content
		match := framing.FindStringSubmatchIndex(input)
		if match == nil {
			t.Error("model request did not include the saved source")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		offset, _ := strconv.Atoi(input[match[2]:match[3]])
		part := strings.TrimSuffix(input[match[1]:], "\n\n")
		end := offset + len(part)
		if end > len(source) || part != source[offset:end] {
			t.Error("a re-planned chunk sent different bytes from the original source")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		proposal := `{"reviewed":[1],"nodes":[],"links":[]}`
		mu.Lock()
		if len(input) > 4096 {
			// Reject the wide window, even on the in-session correction.
			// Bytes from this window count only if a smaller one is accepted.
			refused++
			proposal = `{"reviewed":[],"nodes":[],"links":[]}`
		} else {
			for i := offset; i < end; i++ {
				covered[i] = true
			}
		}
		mu.Unlock()
		nuwaValidationTool(w, r, fmt.Sprintf("narrow-%d", step), wikiExtractTool, proposal)
	})
	e.cfg.Memory.ReviewMaxIterations = 5
	p, err := e.CreateProject("Bounded wiki review", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := knowledgeFixtureThread(t, e, p.ID, "Source")
	distillEvent(t, e, th, store.TurnDone, KindUser, source)
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "thread", ThreadID: th.ID, WikiGraph: true}); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		status, err := e.ProjectWikiBuildStatus(t.Context(), p.ID)
		if err != nil {
			t.Fatal(err)
		}
		if status.State == "running" {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if status.State != "completed" || status.Progress.Processed != 1 || status.Progress.Skipped != 0 || status.Progress.Batches < 3 {
			t.Fatalf("refused batch did not recover without skipping the record: %+v %+v", status, status.Progress)
		}
		mu.Lock()
		defer mu.Unlock()
		if refused < 2 {
			t.Fatalf("did not exercise a refused input and correction: %d", refused)
		}
		for i, accepted := range covered {
			if !accepted {
				t.Fatalf("source byte %d was not sent in any accepted input", i)
			}
		}
		return
	}
	t.Fatal("wiki build did not settle after smaller retries")
}

func TestWikiBuildOnlyNarrowsInputForRecoverableBatchFailures(t *testing.T) {
	for _, code := range []string{"evidence", "context_overflow", "iteration_limit", "agent_timeout"} {
		if got := wikiRetryBudget(code, 8192); got != 4096 {
			t.Errorf("%s retries with %d bytes, want 4096", code, got)
		}
		if got := wikiRetryBudget(code, 2896); got != 2048 {
			t.Errorf("%s retries with %d bytes, want the 2048 byte floor", code, got)
		}
		if got := wikiRetryBudget(code, 2048); got != 0 {
			t.Errorf("%s can retry indefinitely at the minimum budget: %d", code, got)
		}
	}
	for _, code := range []string{"rate_limited", "model_settings", "temporary_model", "first_byte_timeout", ""} {
		if got := wikiRetryBudget(code, 8192); got != 0 {
			t.Errorf("%s unexpectedly replanned the batch: %d", code, got)
		}
	}
}

func TestWikiBuildRecoversPublishedDocumentAfterReceiptWriteFails(t *testing.T) {
	e := newTestEngine(t)
	p, err := e.CreateProject("Document receipts", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := knowledgeFixtureThread(t, e, p.ID, "Original task")
	turn, event := distillEvent(t, e, th, store.TurnDone, KindUser, "Persist the observation.")
	kb, err := e.ProjectKnowledge(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kb.Enable(); err != nil {
		t.Fatal(err)
	}
	if err := kb.EnsureWikiSchema(); err != nil {
		t.Fatal(err)
	}
	result := DistillationResult{
		ReviewID: "wiki-document-checkpoint", State: "running", Scope: "thread", WikiGraph: true,
		FullText: true, KnowledgeEnabled: true, WikiPhase: "documents", WikiDocuments: 1,
		ThreadID: th.ID, TurnID: turn.ID, Progress: &DistillationProgress{Total: 1, Processed: 1},
	}
	node := knowledge.WikiNode{ID: "observation", Title: "Observation", Kind: "entity", Category: "topic",
		Evidence: []knowledge.WikiEvidence{{Source: knowledge.Source{Kind: "event", ThreadID: th.ID, Seq: event.Seq}, Quote: event.Text}}}
	if err := kb.StageWikiNodes(t.Context(), result.ReviewID, []knowledge.WikiNode{node}, knowledge.Stamp{
		UpdatedAt: time.Now(), UpdatedTurn: turn.ID, UpdatedThread: th.ID,
	}); err != nil {
		t.Fatal(err)
	}
	row, err := result.row(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.StartProjectDistillation(t.Context(), row); err != nil {
		t.Fatal(err)
	}
	db := e.store.DB()
	if err := db.Exec(`CREATE TRIGGER reject_wiki_ack BEFORE UPDATE OF state ON project_wiki_builds
		WHEN NEW.state = 'completed' BEGIN SELECT RAISE(ABORT, 'fixture receipt failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	job := &projectDistillWork{projectID: p.ID, result: result, kb: kb}
	checkpoint := &distillCheckpoint{e: e, current: &job.result, projectID: p.ID}
	if err := e.renderWikiDocumentBatch(t.Context(), job, checkpoint, 0, true); err == nil {
		t.Fatal("a refused receipt incorrectly completed the build")
	}
	page, err := e.ProjectKnowledgePage(t.Context(), p.ID, node.ID)
	if err != nil || page.Wiki.Pending || !strings.Contains(page.Body, event.Text) {
		t.Fatalf("document file did not survive the receipt error: %+v %v", page, err)
	}
	if err := db.Exec("DROP TRIGGER reject_wiki_ack").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{ResumeReviewID: row.ID, WikiGraph: true}); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		got, err := e.ProjectWikiBuildStatus(t.Context(), p.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.State == "completed" {
			if got.WikiDocumented != 1 {
				t.Fatalf("receipt lost the previously written document: %+v", got)
			}
			after, err := e.ProjectKnowledgePage(t.Context(), p.ID, node.ID)
			if err != nil || after.Revision != page.Revision {
				t.Fatalf("resume rewrote the published document: %+v %v", after, err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("resumed wiki did not recognize its published document")
}

func TestWikiBuildPublishesVerifiedObservationsAndResumesFullSource(t *testing.T) {
	const first = "Sable Finch attended the Harbor Briefing."
	const tail = "The Amber Seal was mentioned later."
	headers := regexp.MustCompile(`(?m)^\[seq=([0-9]+) kind=`)
	blocked := make(chan struct{}, 1)
	accepted := make(chan struct{}, 1)
	release := make(chan struct{})
	var tailCalls atomic.Int32
	var released sync.Once
	releaseTail := func() { released.Do(func() { close(release) }) }
	defer releaseTail()
	e, _ := distillWire(t, func(w http.ResponseWriter, r scheduleCompletionRequest, step int) {
		transcript := r.Messages[1].Content
		var reviewed []int64
		for _, match := range headers.FindAllStringSubmatch(transcript, -1) {
			seq, _ := strconv.ParseInt(match[1], 10, 64)
			reviewed = append(reviewed, seq)
		}
		proposal := map[string]any{"reviewed": reviewed, "nodes": []any{}, "links": []any{}}
		if strings.Contains(transcript, first) {
			// The leading interactive request must stay small even if the
			// first record is huge. Later requests can use the normal budget.
			if len(transcript) > 2048 {
				t.Errorf("first model request sent %d bytes of history; want at most 2048", len(transcript))
			}
			evidence := map[string]any{"seq": 1, "quote": first}
			proposal["nodes"] = []any{
				map[string]any{"key": "person", "title": "Sable Finch", "kind": "entity", "category": "person", "evidence": evidence},
				map[string]any{"key": "event", "title": "Harbor Briefing", "kind": "event", "category": "meeting", "evidence": evidence},
			}
			proposal["links"] = []any{map[string]any{"from": "event", "to": "person", "evidence": evidence}}
			select {
			case accepted <- struct{}{}:
			default:
			}
		} else if strings.Contains(transcript, tail) {
			select {
			case blocked <- struct{}{}:
			default:
			}
			// The harness delivers this model response only after observing the
			// first window's persisted graph and requesting a pause.
			if tailCalls.Add(1) == 1 {
				select {
				case <-release:
				case <-t.Context().Done():
					return
				}
			}
			proposal["nodes"] = []any{map[string]any{
				"key": "seal", "title": "Amber Seal", "kind": "entity", "category": "artifact",
				"evidence": map[string]any{"seq": 1, "quote": tail},
			}}
		}
		raw, err := json.Marshal(proposal)
		if err != nil {
			t.Error(err)
			return
		}
		nuwaValidationTool(w, r, fmt.Sprintf("wiki-%d", step), wikiExtractTool, string(raw))
	})
	p, err := e.CreateProject("Wiki project", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := knowledgeFixtureThread(t, e, p.ID, "Original task")
	_, event := distillEvent(t, e, th, store.TurnDone, KindUser, first+"\n"+strings.Repeat("original material\n", 2000)+tail)
	legacy := store.ProjectDistillation{ProjectID: p.ID, ID: "existing-distill", State: "paused"}
	if _, err := e.store.StartDistillationQueue(t.Context(), legacy, th.ID, func(int) ([]byte, error) {
		return []byte(`{"state":"paused","review_id":"existing-distill","progress":{"total":1,"remaining":1}}`), nil
	}); err != nil {
		t.Fatal(err)
	}
	start, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "thread", ThreadID: th.ID, WikiGraph: true})
	if err != nil || !start.WikiGraph || !start.FullText || start.MemoryEnabled {
		t.Fatalf("wiki build did not get independent full-source admission: %+v %v", start, err)
	}
	select {
	case <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("initial graph draft was never requested")
	}
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("parallel model window did not reach the tail of the source")
	}
	var id string
	kb, err := e.ProjectKnowledge(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap, err := kb.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		graph, err := snap.Graph()
		if err == nil && len(graph.Nodes) == 2 && len(graph.Edges) == 1 {
			id = graph.Nodes[0].ID
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if id == "" {
		t.Fatal("the verified graph did not appear while another source batch was pending")
	}
	page, err := e.ProjectKnowledgePage(t.Context(), p.ID, id)
	if err != nil || page.Wiki == nil || page.Wiki.Pending || !strings.Contains(page.Body, first) || page.Sources[0].Seq != event.Seq {
		t.Fatalf("a verified observation did not become readable while later evidence was waiting: %+v %v", page, err)
	}
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{PauseReviewID: start.ReviewID, WikiGraph: true}); err != nil {
		t.Fatal(err)
	}
	wait := func() DistillationResult {
		t.Helper()
		for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
			r, err := e.ProjectWikiBuildStatus(t.Context(), p.ID)
			if err != nil {
				t.Fatal(err)
			}
			if r.State != "running" && r.State != "pausing" {
				return r
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("wiki build did not settle")
		return DistillationResult{}
	}
	paused := wait()
	releaseTail()
	if paused.State != "paused" || paused.Progress.Processed != 0 || paused.Progress.SegmentBytes <= 0 {
		t.Fatalf("pause lost the segment or counted a partial record: %+v %+v", paused, paused.Progress)
	}
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{ResumeReviewID: start.ReviewID, WikiGraph: true}); err != nil {
		t.Fatal(err)
	}
	result := wait()
	if result.State != "completed" || result.Progress.Processed != 1 || result.Progress.Skipped != 0 ||
		result.WikiPhase != "documents" || result.WikiDocuments != 3 || result.WikiDocumented != 3 {
		t.Fatalf("wiki did not finish the source and all documents: %+v %+v", result, result.Progress)
	}
	page, err = e.ProjectKnowledgePage(t.Context(), p.ID, id)
	if err != nil || page.Wiki.Pending || !strings.Contains(page.Body, first) {
		t.Fatalf("node identity or original cited text changed during document publication: %+v %v", page, err)
	}
	saved, err := e.ProjectDistillationStatus(t.Context(), p.ID)
	if err != nil || saved.ReviewID != legacy.ID || saved.State != "paused" || saved.Progress.Remaining != 1 {
		t.Fatalf("building a wiki replaced the existing distillation checkpoint: %+v %v", saved, err)
	}
}
