package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestDistillationFullTextReachesLongRecordTail(t *testing.T) {
	const last = "Retain the final observation."
	body := "Begin of original record.\n" + strings.Repeat("original material 甲乙丙\n", 3900) + last
	prefix := regexp.MustCompile(`^\[seq=1 .* byte_offset=(\d+)\] human: `)
	var mu sync.Mutex
	var assembled strings.Builder
	var calls atomic.Int32
	blocked := make(chan string, 1)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		step := calls.Add(1)
		_, transcript, _ := strings.Cut(req.Messages[1].Content, reviewSourceHeader)
		transcript, _, _ = strings.Cut(transcript, "\n\n\nCurrent project notes")
		if step == 2 {
			blocked <- transcript
			<-r.Context().Done()
			return
		}
		match := prefix.FindStringSubmatch(transcript)
		if len(match) != 2 {
			t.Errorf("batch %d did not carry a source with a byte offset", step)
			http.Error(w, "invalid source", http.StatusBadRequest)
			return
		}
		offset, _ := strconv.Atoi(match[1])
		part := transcript[len(match[0]):]
		mu.Lock()
		if offset != assembled.Len() {
			t.Errorf("batch %d began at byte %d after %d bytes", step, offset, assembled.Len())
		}
		assembled.WriteString(part)
		mu.Unlock()
		proposal := `{"changes":[]}`
		if strings.HasSuffix(part, last) {
			proposal = `{
				"changes":[
					{"target":"memory","action":"add","content":"Retain the final observation.","evidence":[{"seq":1,"quote":"Retain the final observation."}]},
					{"target":"skill_manage","action":"create","name":"retain","description":"Retention workflow","content":"Retain the final observation.","evidence":[{"seq":1,"quote":"Retain the final observation."}]}
				]
			}`
		}
		scriptedToolReply(w, req, fmt.Sprintf("batch-%d", step), distillBatchName, proposal)
	}))
	t.Cleanup(endpoint.Close)
	dir := t.TempDir()
	engine := func(t *testing.T) *Engine {
		e := newTestEngineAt(t, dir)
		e.cfg.Models.Providers[0].BaseURL = endpoint.URL
		e.cfg.Models.Providers[0].Model = "fixture"
		e.pool = provider.New(e.cfg)
		return e
	}
	var projectID, reviewID string
	var saved DistillationProgress
	t.Run("pause inside a record", func(t *testing.T) {
		e := engine(t)
		p, err := e.CreateProject("Long history", "", "", true)
		if err != nil {
			t.Fatal(err)
		}
		th := distillFixtureThread(t, e, p.ID, "Long task")
		distillEvent(t, e, th, store.TurnDone, KindUser, body)
		projectID = p.ID
		start, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "thread", ThreadID: th.ID, FullText: true})
		if err != nil || !start.FullText {
			t.Fatalf("full text not admitted: %+v %v", start, err)
		}
		reviewID = start.ReviewID
		select {
		case <-blocked:
		case <-time.After(8 * time.Second):
			t.Fatal("second segment did not reach the model")
		}
		if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{PauseReviewID: reviewID}); err != nil {
			t.Fatal(err)
		}
		result := waitDistill(t, e, p.ID)
		saved = *result.Progress
		row, err := e.store.ProjectDistillationReceipt(t.Context(), p.ID)
		if err != nil || row.CursorEventID != 0 || result.State != "paused" || saved.Processed != 0 ||
			saved.Remaining != 1 || saved.Batches != 1 || saved.SegmentBytes <= 0 || saved.SegmentTotalBytes != len(body) {
			t.Fatalf("partial record was counted or its offset lost: %+v progress=%+v %v", result, saved, err)
		}
	})
	t.Run("resume at the persisted byte after restart", func(t *testing.T) {
		e := engine(t)
		resumed, err := e.StartProjectDistillation(t.Context(), projectID, DistillationRequest{ResumeReviewID: reviewID})
		if err != nil || !resumed.FullText || resumed.Scope != "thread" || *resumed.Progress != saved {
			t.Fatalf("resume lost the mode, scope or offset: %+v %v", resumed, err)
		}
		result := waitDistill(t, e, projectID)
		if !result.FullText || result.State != "completed" || result.HistoryOmitted ||
			result.Progress.Processed != 1 || result.Progress.Skipped != 0 || result.Progress.Remaining != 0 ||
			result.Progress.SegmentBytes != 0 || len(result.Changes) != 2 {
			t.Fatalf("large source was not reviewed across both destinations: %+v progress=%+v", result, result.Progress)
		}
		mu.Lock()
		matches := assembled.String() == body
		mu.Unlock()
		if !matches {
			t.Fatal("model inputs did not reconstruct the original UTF-8 bytes")
		}
	})
}

func TestDistillationDefaultReportsExcerptsAndOversizedSkipsSeparately(t *testing.T) {
	e, calls := distillWire(t, func(w http.ResponseWriter, req scheduleCompletionRequest, _ int) {
		input := req.Messages[1].Content
		if !strings.Contains(input, "visible prefix") || strings.Contains(input, "hidden tail") || strings.Contains(input, "oversized record") {
			t.Error("default distillation must keep its bounded excerpt policy")
		}
		scriptedToolReply(w, req, "excerpt", distillBatchName, `{"changes":[]}`)
	})
	p, err := e.CreateProject("Long history", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := distillFixtureThread(t, e, p.ID, "Long task")
	distillEvent(t, e, th, store.TurnDone, KindUser, "visible prefix"+strings.Repeat("x", reviewMaxCharsPerMessage)+"hidden tail")
	distillEvent(t, e, th, store.TurnDone, KindUser, "oversized record"+strings.Repeat("x", 65<<10))
	distillEvent(t, e, th, store.TurnDone, KindSteer, "caption is missing")
	start, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "project"})
	if err != nil {
		t.Fatal(err)
	}
	result := waitDistill(t, e, p.ID)
	if start.FullText || result.State != "completed" || !result.HistoryOmitted || !result.Progress.CoverageKnown ||
		result.Progress.Processed != 3 || result.Progress.Excerpted != 1 || result.Progress.OversizedSkipped != 1 ||
		result.Progress.Skipped != 2 || result.Progress.Remaining != 0 || calls.Load() != 1 {
		t.Fatalf("receipt conflated excerpts with entirely skipped records: %+v progress=%+v calls=%d", result, result.Progress, calls.Load())
	}
}

func TestDistillationFullTextKeepsEvidenceEligibilityAndSteerAttribution(t *testing.T) {
	const revised = "Retain the corrected observation."
	const goodResult = "Observed result."
	e, calls := distillWire(t, func(w http.ResponseWriter, req scheduleCompletionRequest, _ int) {
		input := req.Messages[1].Content
		for _, visible := range []string{revised, goodResult, `paired tool="inspect"`, "Peer history.", "Assistant observation.", "Requirement from a failed turn."} {
			if !strings.Contains(input, visible) {
				t.Errorf("missing evidence %q", visible)
			}
		}
		for _, excluded := range []string{"Original caption.", "Skipped failed receipt.", "Skipped derived receipt.", "Skipped private receipt.", "Skipped unmatched result.", "Long call arguments", "Invalid encoding"} {
			if strings.Contains(input, excluded) {
				t.Errorf("ineligible evidence reached the model: %q", excluded)
			}
		}
		scriptedToolReply(w, req, "steer", distillBatchName, `{"changes":[
			{"target":"memory","action":"add","content":"Retain the corrected observation.","evidence":[{"seq":2,"quote":"Retain the corrected observation."}]}
		]}`)
	})
	p, err := e.CreateProject("Full project", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := distillFixtureThread(t, e, p.ID, "Main task")
	peer := distillFixtureThread(t, e, p.ID, "Peer task")
	turn, _ := distillEvent(t, e, th, store.TurnDone, KindUser, "Initial request.")
	steer := store.Event{Kind: KindSteer, Text: "Original caption."}
	for _, ev := range []*store.Event{
		&steer,
		{Kind: "tool_call", AgentID: "worker", ToolCallID: "original", Text: "inspect(" + strings.Repeat("Long call arguments", 4000) + ")"},
		{Kind: "tool_result", AgentID: "worker", ToolCallID: "original", Text: goodResult},
		{Kind: "tool_result", AgentID: "worker", ToolCallID: "original", Text: `{"value":"Skipped failed receipt.` + strings.Repeat("x", 70<<10) + `","success":false}`},
		{Kind: "tool_call", AgentID: "worker", ToolCallID: "derived", Text: ToolQueryHistory + "()"},
		{Kind: "tool_result", AgentID: "worker", ToolCallID: "derived", Text: "Skipped derived receipt."},
		{Kind: "tool_result", AgentID: "worker", ToolCallID: "original", Text: "Skipped private receipt.", WorkRecordPayload: true},
		{Kind: "tool_result", AgentID: "other", ToolCallID: "original", Text: "Skipped unmatched result."},
		{Kind: KindUser, Text: "Invalid encoding\xff"},
		{Kind: KindUser, Text: " \n "},
	} {
		ev.ThreadID, ev.TurnID = th.ID, turn.ID
		if err := e.store.AppendEvent(ev); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.store.AppendMessages(th.ID, turn.ID, []store.Message{{Role: "user", EventSeq: steer.Seq, Content: "[steer] " + revised}}); err != nil {
		t.Fatal(err)
	}
	distillEvent(t, e, peer, store.TurnDone, KindUser, "Peer history.")
	distillEvent(t, e, peer, store.TurnDone, "agent_message", "Assistant observation.")
	distillEvent(t, e, peer, store.TurnError, KindUser, "Requirement from a failed turn.")
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "project", FullText: true}); err != nil {
		t.Fatal(err)
	}
	result := waitDistill(t, e, p.ID)
	if result.State != "completed" || result.Progress.Processed != 11 || result.Progress.Skipped != 5 || calls.Load() != 1 {
		t.Fatalf("full project review changed evidence admission: %+v progress=%+v", result, result.Progress)
	}
	if len(result.Changes) != 1 {
		t.Fatalf("a steer citation was not accepted at its canonical source: %+v", result)
	}
}

func TestDistillationFullTextDefersTheNextRecordAtTheBatchBoundary(t *testing.T) {
	const next = "Next original record."
	e, calls := distillWire(t, func(w http.ResponseWriter, req scheduleCompletionRequest, step int) {
		if strings.Contains(req.Messages[1].Content, next) != (step == 2) {
			t.Errorf("second record appeared in the wrong batch: %d", step)
		}
		scriptedToolReply(w, req, fmt.Sprintf("boundary-%d", step), distillBatchName, `{"changes":[]}`)
	})
	p, err := e.CreateProject("Boundary", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := distillFixtureThread(t, e, p.ID, "Task")
	event := store.Event{ID: 1, Seq: 1, Kind: KindUser, ThreadID: th.ID}
	header := distillationFullHeader(store.DistillationSourceRow{Event: event}, 1, 0, store.Event{})
	distillEvent(t, e, th, store.TurnDone, KindUser, strings.Repeat("x", reviewMaxChars-len(reviewSourceHeader+header+"\n\n")))
	distillEvent(t, e, th, store.TurnDone, KindUser, next)
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "thread", ThreadID: th.ID, FullText: true}); err != nil {
		t.Fatal(err)
	}
	result := waitDistill(t, e, p.ID)
	if result.State != "completed" || result.Progress.Processed != 2 || result.Progress.Skipped != 0 || calls.Load() != 2 {
		t.Fatalf("batch boundary truncated or dropped the next record: %+v progress=%+v calls=%d", result, result.Progress, calls.Load())
	}
}

func TestDistillationFullTextAccountsForSourcesLostBetweenSegments(t *testing.T) {
	e := newTestEngine(t)
	p, err := e.CreateProject("Changed history", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := distillFixtureThread(t, e, p.ID, "Task")
	turn, changed := distillEvent(t, e, th, store.TurnDone, KindUser, "Saved text with a longer tail.")
	_, removed := distillEvent(t, e, th, store.TurnDone, KindUser, "Will disappear.")
	result := DistillationResult{ReviewID: "saved", State: "running", Scope: "thread", FullText: true,
		ThreadID: th.ID, TurnID: turn.ID, MemoryEnabled: true,
		Progress: &DistillationProgress{Total: 2, Remaining: 2, SegmentBytes: 5, SegmentTotalBytes: len(changed.Text)}}
	row, err := result.row(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.StartDistillationQueue(t.Context(), row, th.ID, func(int) ([]byte, error) { return row.Receipt, nil }); err != nil {
		t.Fatal(err)
	}
	job := &projectDistillWork{projectID: p.ID, result: result}
	if err := e.store.DB().Model(&store.Event{}).Where("id=?", changed.ID).Update("text", "Saved text.").Error; err != nil {
		t.Fatal(err)
	}
	if err := e.store.DB().Delete(&store.Event{}, removed.ID).Error; err != nil {
		t.Fatal(err)
	}
	batch, err := e.loadDistillationSlice(t.Context(), job, 0, reviewMaxChars)
	if err != nil || batch.checkpoint.count != 2 || batch.checkpoint.skipped != 2 ||
		batch.checkpoint.segmentBytes != 0 || len(batch.sources.sources) != 0 {
		t.Fatalf("changed sources supplied a different suffix to the next batch: %+v %v", batch, err)
	}
	rows, err := e.store.DistillationMetadataWindow(t.Context(), row.ID, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := e.distillationFullSlice(ctx, job, 0, rows, reviewMaxChars); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation became a skipped record: %v", err)
	}
}
