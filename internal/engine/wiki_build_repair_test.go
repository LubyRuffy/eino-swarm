package engine

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/knowledge"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestWikiReviewRepairsOnlyTheUnresolvedItemsAtTheModelBoundary(t *testing.T) {
	const first = "North Station opened."
	const second = "North Station hosted the Winter Session."
	e, calls := distillWire(t, func(w http.ResponseWriter, req scheduleCompletionRequest, step int) {
		switch step {
		case 1:
			nuwaValidationTool(w, req, "draft", wikiExtractTool, `{
				"reviewed":[1],
				"nodes":[
					{"key":"station","title":"North Station","kind":"entity","category":"place","evidence":{"seq":1,"quote":"North Station opened."}},
					{"key":"session","title":"Winter Session","kind":"event","category":"meeting","evidence":{"seq":2,"quote":"Winter Session was planned."}}],
				"links":[{"from":"station","to":"session","evidence":{"seq":2,"quote":"North Station hosted the Winter Session."}}]
			}`)
		case 2:
			var receipt struct{ Note string }
			if err := json.Unmarshal([]byte(req.Messages[len(req.Messages)-1].Content), &receipt); err != nil {
				t.Errorf("invalid tool feedback: %v", err)
			}
			feedback := receipt.Note
			if !strings.Contains(feedback, "Missing source numbers: [2]") || !strings.Contains(feedback, `node "session"`) ||
				!strings.Contains(feedback, "retained") || strings.Contains(feedback, "North Station opened.") {
				t.Errorf("repair request lacks targeted feedback or repeats accepted evidence: %q", feedback)
			}
			// The first acknowledgement, valid node and dependent link are
			// intentionally omitted. The host must retain and recheck them.
			nuwaValidationTool(w, req, "corrected", wikiExtractTool, `{
				"reviewed":[2],
				"nodes":[{"key":"session","title":"Winter Session","kind":"event","category":"meeting","evidence":{"seq":2,"quote":"North Station hosted the Winter Session."}}]
			}`)
		default:
			t.Errorf("expected one targeted correction, got model call %d", step)
			nuwaValidationTool(w, req, "unexpected", wikiExtractTool, `{"reviewed":[1,2],"nodes":[],"links":[]}`)
		}
	})
	e.cfg.Memory.ReviewMaxIterations = 2
	p, err := e.CreateProject("Wiki repair", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := knowledgeFixtureThread(t, e, p.ID, "Evidence")
	distillEvent(t, e, th, store.TurnDone, KindUser, first)
	distillEvent(t, e, th, store.TurnDone, KindUser, second)
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "thread", ThreadID: th.ID, WikiGraph: true}); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		result, err := e.ProjectWikiBuildStatus(t.Context(), p.ID)
		if err != nil {
			t.Fatal(err)
		}
		if result.State == "running" {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if result.State != "completed" || result.Progress.Processed != 2 || result.Progress.Batches != 1 || calls.Load() != 2 {
			t.Fatalf("item repair did not commit exactly one covered batch: %+v, progress=%+v, calls=%d", result, result.Progress, calls.Load())
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
		if err != nil || len(graph.Nodes) != 2 || len(graph.Edges) != 1 {
			t.Fatalf("retained draft produced wrong graph: %+v %v", graph, err)
		}
		return
	}
	t.Fatal("targeted Wiki review did not settle")
}

func TestWikiRepairWithdrawsOnlyUnsupportedPendingItems(t *testing.T) {
	visible := map[int64]store.Event{1: {Text: "North Station opened."}, 2: {Text: "The session remained unverified."}}
	valid := wikiModelNode{Key: "station", Title: "North Station", Kind: "entity", Category: "place",
		Evidence: reviewCitation{Seq: 1, Quote: "North Station opened."}}
	unsupported := wikiModelNode{Key: "session", Title: "Winter Session", Kind: "event", Category: "meeting",
		Evidence: reviewCitation{Seq: 2, Quote: "Winter Session occurred."}}
	link := wikiModelLink{From: "station", To: "session", Evidence: unsupported.Evidence}
	ack := []int64{1, 1, 100}
	nodes := []wikiModelNode{valid, unsupported}
	links := []wikiModelLink{link}
	if _, err := newWikiDraftRepair(wikiProposal{Nodes: &nodes}); err == nil {
		t.Fatal("a partial first submission was admitted")
	}
	first := wikiProposal{Reviewed: &ack, Nodes: &nodes, Links: &links}
	repair, err := newWikiDraftRepair(first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repair.apply(first, visible); err == nil || !strings.Contains(err.Error(), "unknown or repeated source number") {
		t.Fatalf("invalid flags or unsupported claims passed: %v", err)
	}
	if _, err := repair.apply(wikiProposal{DropNodes: []string{"station"}}, visible); err == nil {
		t.Fatal("the repair removed an already accepted item")
	}
	if _, err := repair.apply(wikiProposal{DropLinks: []wikiLinkPair{{"station", "unknown"}}}, visible); err == nil {
		t.Fatal("the repair accepted an unknown link ID")
	}
	replacement := valid
	replacement.Title = "Other Station"
	changed := []wikiModelNode{replacement}
	if _, err := repair.apply(wikiProposal{Nodes: &changed}, visible); err == nil {
		t.Fatal("the repair replaced an already accepted node")
	}
	duplicate := []wikiModelNode{valid, valid}
	if _, err := repair.apply(wikiProposal{Nodes: &duplicate}, visible); err == nil {
		t.Fatal("the repair accepted ambiguous node keys")
	}
	badLink := []wikiModelLink{{From: "station", To: "\n"}}
	if _, err := repair.apply(wikiProposal{Links: &badLink}, visible); err == nil {
		t.Fatal("the repair accepted an invalid endpoint key")
	}
	remaining := []int64{2}
	if _, err := repair.apply(wikiProposal{Reviewed: &remaining, DropNodes: []string{"session"}}, visible); err == nil {
		t.Fatal("withdrawing a node silently forgot its dependent link")
	}
	p, err := repair.apply(wikiProposal{DropLinks: []wikiLinkPair{{"session", "station"}}}, visible)
	if err != nil || len(*p.Nodes) != 1 || (*p.Nodes)[0] != valid || len(*p.Links) != 0 || len(*p.Reviewed) != 2 {
		t.Fatalf("withdrawal corrupted prior decisions: %+v, %v", p, err)
	}
}

func TestWikiSemanticPageReusesASourceBoundLocalIdentifier(t *testing.T) {
	const quote = "Observed https://DOCUMENT.TEST/notes."
	source := knowledge.Source{Kind: "event", ThreadID: "thread", Seq: 4}
	local := knowledge.ExtractWikiObservations(quote, source)
	if len(local) != 1 {
		t.Fatalf("missing canonical local node: %+v", local)
	}
	nodes := []wikiModelNode{{Key: "host", Title: "DOCUMENT.TEST", Kind: "entity", Category: "web-site",
		Evidence: reviewCitation{Seq: 1, Quote: quote}}}
	links := []wikiModelLink{}
	semantic := wikiPages(wikiProposal{Nodes: &nodes, Links: &links}, map[int64]knowledge.Source{1: source})
	if len(semantic) != 1 || semantic[0].ID != local[0].ID || semantic[0].Category != local[0].Category ||
		semantic[0].Title != local[0].Title || semantic[0].Evidence[0].Quote != quote {
		t.Fatalf("model duplicated or changed the local subject: local=%+v semantic=%+v", local, semantic)
	}
}
