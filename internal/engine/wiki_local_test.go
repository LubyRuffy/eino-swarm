package engine

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/knowledge"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestWikiBuildPublishesLocalNodesWhileTheModelIsWaiting(t *testing.T) {
	const quote = "The reference is https://example.test/notes."
	entered := make(chan struct{})
	release := make(chan struct{})
	var first, closed sync.Once
	defer closed.Do(func() { close(release) })
	e, _ := distillWire(t, func(w http.ResponseWriter, req scheduleCompletionRequest, _ int) {
		first.Do(func() { close(entered) })
		select {
		case <-release:
		case <-t.Context().Done():
			return
		}
		nuwaValidationTool(w, req, "review", wikiExtractTool, `{"reviewed":[1],"nodes":[],"links":[]}`)
	})
	p, err := e.CreateProject("Local observations", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := knowledgeFixtureThread(t, e, p.ID, "History")
	turn, source := distillEvent(t, e, th, store.TurnDone, KindUser, quote)
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "thread", ThreadID: th.ID, WikiGraph: true}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("review did not reach the real model HTTP boundary")
	}
	got, err := e.ProjectWikiBuildStatus(t.Context(), p.ID)
	if err != nil || got.State != "running" || got.Progress.Processed != 0 {
		t.Fatalf("review stage must still be in progress: %+v %v", got, err)
	}
	wire, _ := json.Marshal(got)
	if !strings.Contains(string(wire), `"wiki_scan_complete":true`) {
		t.Fatalf("local scan did not finish before the first model call: %s", wire)
	}
	kb, err := e.ProjectKnowledge(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := kb.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	graph, err := snap.Graph()
	if err != nil || len(graph.Nodes) == 0 {
		t.Fatalf("no local node was visible while the model was blocked: %+v %v", graph, err)
	}
	for _, node := range graph.Nodes {
		if node.Title != "example.test" {
			continue
		}
		page, err := e.ProjectKnowledgePage(t.Context(), p.ID, node.ID)
		if err != nil || len(page.Sources) == 0 || page.Sources[0].Seq != source.Seq || page.Sources[0].TurnID != turn.ID ||
			len(page.WikiObservations) != 1 || !strings.Contains(quote, page.WikiObservations[0].Quote) {
			t.Fatalf("local graph node lost its bound source: %+v %v", page, err)
		}
		// Manual edits to a Markdown quote or original record cannot hand
		// the frontend an excerpt that the current original does not contain.
		if err := e.store.DB().Exec("UPDATE events SET text=? WHERE id=?", "Changed source", source.ID).Error; err != nil {
			t.Fatal(err)
		}
		current, err := e.ProjectKnowledgePage(t.Context(), p.ID, node.ID)
		if err != nil || len(current.WikiObservations) != 0 {
			t.Fatalf("a stale quote was returned as original evidence: %+v %v", current, err)
		}
		return
	}
	t.Fatalf("local node title missing from the graph: %+v", graph.Nodes)
}

func TestWikiLocalResumeKeepsPublishedNodesAfterCheckpointFailure(t *testing.T) {
	e, requests := distillWire(t, func(w http.ResponseWriter, req scheduleCompletionRequest, _ int) {
		nuwaValidationTool(w, req, "repair-resume", wikiExtractTool, `{"reviewed":[1],"nodes":[],"links":[]}`)
	})
	p, err := e.CreateProject("Local receipt recovery", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := knowledgeFixtureThread(t, e, p.ID, "Retained record")
	_, source := distillEvent(t, e, th, store.TurnDone, KindUser, "Recorded link https://persisted.example.test/reference")
	db := e.store.DB()
	if err := db.Exec(`CREATE TRIGGER refuse_local_scan_receipt BEFORE UPDATE OF receipt ON project_wiki_builds
		WHEN json_extract(NEW.receipt, '$.wiki_scan_cursor') > 0
		BEGIN SELECT RAISE(ABORT, 'fixture local receipt failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	req := DistillationRequest{Scope: "thread", ThreadID: th.ID, WikiGraph: true}
	started, err := e.StartProjectDistillation(t.Context(), p.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	failed := waitLocalWiki(t, e, p.ID)
	if failed.State != "failed" || failed.WikiScanCursor != 0 || !failed.Uncertain || requests.Load() != 0 {
		t.Fatalf("local failure advanced or started model review: %+v requests=%d", failed, requests.Load())
	}
	kb, err := e.ProjectKnowledge(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := kb.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	graph, err := before.Graph()
	if err != nil || len(graph.Nodes) == 0 {
		t.Fatalf("local filesystem publication was lost: %+v %v", graph, err)
	}
	if err := db.Exec("DROP TRIGGER refuse_local_scan_receipt").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{ResumeReviewID: started.ReviewID, WikiGraph: true}); err != nil {
		t.Fatal(err)
	}
	got := waitLocalWiki(t, e, p.ID)
	if got.State != "completed" || !got.WikiScanComplete || got.WikiScanProcessed != 1 || got.Progress.Processed != 1 || requests.Load() != 1 {
		t.Fatalf("local checkpoint did not recover: %+v %+v requests=%d", got, got.Progress, requests.Load())
	}
	after, err := kb.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	finalGraph, err := after.Graph()
	if err != nil || len(finalGraph.Nodes) != len(graph.Nodes) || len(finalGraph.Edges) != len(graph.Edges) {
		t.Fatalf("resume duplicated the graph: before=%+v after=%+v %v", graph, finalGraph, err)
	}
	for _, node := range graph.Nodes {
		page, err := e.ProjectKnowledgePage(t.Context(), p.ID, node.ID)
		if err != nil || len(page.Sources) != 1 || page.Sources[0].Seq != source.Seq || len(page.Wiki.Evidence) != 1 {
			t.Fatalf("resume duplicated the receipt's source observation: %+v %v", page, err)
		}
	}
}

func TestWikiBuildOnlyRevisitsNewImmutableHistory(t *testing.T) {
	var resent atomic.Bool
	e, requests := distillWire(t, func(w http.ResponseWriter, req scheduleCompletionRequest, step int) {
		input := req.Messages[1].Content
		if step > 1 && (strings.Contains(input, "retained-start.example.test") || !strings.Contains(input, "retained-next.example.test")) {
			resent.Store(true)
		}
		nuwaValidationTool(w, req, "incremental", wikiExtractTool, `{"reviewed":[1],"nodes":[],"links":[]}`)
	})
	p, err := e.CreateProject("Incremental local wiki", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := knowledgeFixtureThread(t, e, p.ID, "Retained records")
	build := func(body string, total int) {
		t.Helper()
		distillEvent(t, e, th, store.TurnDone, KindUser, body)
		if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "project", WikiGraph: true}); err != nil {
			t.Fatal(err)
		}
		got := waitLocalWiki(t, e, p.ID)
		if got.State != "completed" || !got.WikiScanComplete || got.WikiScanProcessed != total ||
			got.Progress.Processed != total || got.WikiScanSkipped != 0 || got.Progress.Skipped != 0 {
			t.Fatalf("incremental scope was not fully accounted for: %+v %+v", got, got.Progress)
		}
	}
	build("Read https://retained-start.example.test/view", 1)
	build("Read https://retained-next.example.test/view", 2)
	if requests.Load() != 2 || resent.Load() {
		t.Fatalf("model saw already checked history: calls=%d reused_old=%v", requests.Load(), resent.Load())
	}
	kb, err := e.ProjectKnowledge(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := kb.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	graph, err := snap.Graph()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"retained-start.example.test", "retained-next.example.test"} {
		count := 0
		for _, node := range graph.Nodes {
			if node.Title == want {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("preserved and new observations must each appear exactly once: %q found %d", want, count)
		}
	}
}

func waitLocalWiki(t *testing.T, e *Engine, projectID string) DistillationResult {
	t.Helper()
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		r, err := e.ProjectWikiBuildStatus(t.Context(), projectID)
		if err != nil {
			t.Fatal(err)
		}
		if r.State != "running" && r.State != "pausing" {
			return r
		}
	}
	t.Fatal("wiki build did not settle")
	return DistillationResult{}
}

func TestWikiLocalPreservesLargeStructuredSourceContext(t *testing.T) {
	t.Run("bounded structured source", func(t *testing.T) {
		e, _ := distillWire(t, func(w http.ResponseWriter, r scheduleCompletionRequest, _ int) {
			// Report ordinary refusal. The test checks the graph the local
			// phase made visible before the model needed to finish.
			nuwaValidationTool(w, r, "no-review", wikiExtractTool, `{"reviewed":[],"nodes":[],"links":[]}`)
		})
		e.cfg.Memory.ReviewMaxIterations = 1
		p, err := e.CreateProject("Structured observation", "", "", true)
		if err != nil {
			t.Fatal(err)
		}
		th := knowledgeFixtureThread(t, e, p.ID, "Retained structured source")
		body := `{"hostname":"confirmed.example.test","query":"` + strings.Repeat("a", wikiLocalSourceBytes+40) + ` https://ignored.example.test/a"}`
		distillEvent(t, e, th, store.TurnDone, KindUser, body)
		if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "thread", ThreadID: th.ID, WikiGraph: true}); err != nil {
			t.Fatal(err)
		}
		result := waitLocalWiki(t, e, p.ID)
		if !result.WikiScanComplete || result.WikiScanProcessed != 1 || result.WikiScanSkipped != 0 || result.WikiScanDeferred != 0 {
			t.Fatalf("the structured record was not scanned: %+v", result)
		}
		kb, err := e.ProjectKnowledge(p.ID)
		if err != nil {
			t.Fatal(err)
		}
		snap, err := kb.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		graph, err := snap.Graph()
		if err != nil || len(graph.Nodes) != 1 || graph.Nodes[0].Title != "confirmed.example.test" {
			t.Fatalf("split JSON lost a typed observation or admitted query text: %+v %v", graph, err)
		}
	})
	t.Run("above local structured budget", func(t *testing.T) {
		e := newTestEngine(t)
		row := store.WikiScanSource{Part: store.DistillationText{
			Text: `{"query":"`, TotalBytes: wikiLocalStructuredBytes + 1,
		}}
		nodes, deferred, err := e.extractLocalWikiSource(t.Context(), nil, row, knowledge.Source{})
		if err != nil || !deferred || len(nodes) != 0 {
			t.Fatalf("an oversized structured record was not deferred intact: %+v %v %v", nodes, deferred, err)
		}
	})
}
