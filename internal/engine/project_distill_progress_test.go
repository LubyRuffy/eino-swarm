package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestDistillationReachesEveryBatchInLongConversation(t *testing.T) {
	var mu sync.Mutex
	seen := make(map[string]int)
	e, calls := distillWire(t, func(w http.ResponseWriter, req scheduleCompletionRequest, step int) {
		mu.Lock()
		for i := range 30 {
			marker := fmt.Sprintf("original-record-%03d", i)
			for _, msg := range req.Messages {
				if strings.Contains(msg.Content, marker) {
					seen[marker]++
				}
			}
		}
		mu.Unlock()
		scriptedToolReply(w, req, fmt.Sprintf("batch-%d", step), distillBatchName, `{"changes":[]}`)
	})
	p, err := e.CreateProject("Long history", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := distillFixtureThread(t, e, p.ID, "Long task")
	turn, _ := distillEvent(t, e, th, store.TurnDone, KindUser, "Initialize the task.")
	for i := range 30 {
		err := e.store.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser,
			Text: fmt.Sprintf("original-record-%03d %s", i, strings.Repeat("material ", 160))})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "thread", ThreadID: th.ID}); err != nil {
		t.Fatal(err)
	}
	result := waitDistill(t, e, p.ID)
	mu.Lock()
	defer mu.Unlock()
	if result.State != "completed" || len(seen) != 30 || calls.Load() < 2 {
		t.Fatalf("completed without seeing the whole history: visited=%d calls=%d state=%s", len(seen), calls.Load(), result.State)
	}
	for marker, n := range seen {
		if n != 1 {
			t.Fatalf("%s reviewed in %d batches", marker, n)
		}
	}
}

func TestDistillationResumesAfterPauseAndEngineRestart(t *testing.T) {
	const convention = "Keep the measurement convention."
	const proposal = `{
		"changes":[
			{"target":"memory","action":"add","content":"Keep the measurement convention.","evidence":[{"seq":1,"quote":"Keep the measurement convention."}]},
			{"target":"skill_manage","action":"create","name":"measurement","description":"Measurement workflow","content":"Read the retained measurement convention.","evidence":[{"seq":1,"quote":"Keep the measurement convention."}]}
		]
	}`
	var calls atomic.Int32
	entered := make(chan string, 1)
	resumed := make(chan string, 1)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var input scheduleCompletionRequest
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Errorf("bad request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		step := calls.Add(1)
		switch step {
		case 1:
			scriptedToolReply(w, input, "first", distillBatchName, proposal)
			return
		case 2:
			entered <- input.Messages[1].Content
			<-req.Context().Done()
			return
		case 3:
			resumed <- input.Messages[1].Content
		}
		scriptedToolReply(w, input, fmt.Sprintf("batch-%d", step), distillBatchName, `{"changes":[]}`)
	}))
	t.Cleanup(endpoint.Close)

	configure := func(t *testing.T, dir string) *Engine {
		e := newTestEngineAt(t, dir)
		e.cfg.Models.Providers[0].BaseURL = endpoint.URL
		e.cfg.Models.Providers[0].Model = "fixture"
		e.pool = provider.New(e.cfg)
		return e
	}
	dir := t.TempDir()
	var projectID, reviewID, secondInput string
	var before DistillationResult
	t.Run("pause the second batch", func(t *testing.T) {
		e := configure(t, dir)
		p, err := e.CreateProject("Long history", "", "", true)
		if err != nil {
			t.Fatal(err)
		}
		projectID = p.ID
		th := distillFixtureThread(t, e, p.ID, "Main task")
		other := distillFixtureThread(t, e, p.ID, "Peer task")
		a, _ := distillEvent(t, e, th, store.TurnDone, KindUser, convention)
		b, _ := distillEvent(t, e, other, store.TurnDone, KindUser, "Retain the peer observation.")
		for i := range 40 {
			turn := a
			if i%2 == 1 {
				turn = b
			}
			err := e.store.AppendEvent(&store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: KindUser,
				Text: fmt.Sprintf("record-%03d %s", i, strings.Repeat("material ", 160))})
			if err != nil {
				t.Fatal(err)
			}
		}
		start, err := e.StartProjectDistillation(t.Context(), projectID, DistillationRequest{Scope: "project"})
		if err != nil || start.Progress == nil || start.Progress.Total != 42 {
			t.Fatalf("wrong frozen scope: %+v %v", start, err)
		}
		reviewID = start.ReviewID
		select {
		case secondInput = <-entered:
		case <-time.After(8 * time.Second):
			t.Fatal("second batch did not reach the model")
		}
		current, err := e.ProjectDistillationStatus(t.Context(), projectID)
		if err != nil || current.State != "running" || current.Progress.Batches != 1 || current.Progress.Remaining == 0 ||
			len(current.Changes) != 2 {
			t.Fatalf("first batch was not durably visible: %+v %v", current, err)
		}
		if _, err := e.StartProjectDistillation(t.Context(), projectID, DistillationRequest{PauseReviewID: "wrong"}); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("pause affected a different review: %v", err)
		}
		stopping, err := e.StartProjectDistillation(t.Context(), projectID, DistillationRequest{PauseReviewID: reviewID})
		if err != nil || stopping.State != "pausing" || stopping.Scope != "project" {
			t.Fatalf("pause was not acknowledged for the saved project scope: %+v %v", stopping, err)
		}
		before = waitDistill(t, e, projectID)
		if before.State != "paused" || before.Error != "" || before.Uncertain || *before.Progress != *current.Progress {
			t.Fatalf("pause changed the committed checkpoint: before=%+v after=%+v", current, before)
		}
		if _, err := e.StartProjectDistillation(t.Context(), projectID, DistillationRequest{Scope: "thread", ThreadID: other.ID}); !errors.Is(err, ErrDistillationPending) {
			t.Fatalf("new scope replaced the checkpoint: %v", err)
		}
		distillEvent(t, e, other, store.TurnDone, KindUser, "Recorded after the snapshot.")
	})
	t.Run("reopen and finish the same scope", func(t *testing.T) {
		e := configure(t, dir)
		restored, err := e.ProjectDistillationStatus(t.Context(), projectID)
		if err != nil || restored.ReviewID != reviewID || restored.State != "paused" || restored.Scope != "project" ||
			*restored.Progress != *before.Progress {
			t.Fatalf("restart lost saved progress: %+v %v", restored, err)
		}
		if _, err := e.StartProjectDistillation(t.Context(), projectID, DistillationRequest{ResumeReviewID: reviewID}); err != nil {
			t.Fatal(err)
		}
		select {
		case input := <-resumed:
			if input != secondInput {
				t.Fatal("resume did not return to the first uncommitted batch")
			}
		case <-time.After(8 * time.Second):
			t.Fatal("resumed batch never reached the model")
		}
		result := waitDistill(t, e, projectID)
		if result.State != "completed" || result.ReviewID != reviewID || result.Progress.Processed != 42 ||
			result.Progress.Remaining != 0 || len(result.Changes) != 2 {
			t.Fatalf("lost coverage or repeated a committed write: %+v progress=%+v", result, result.Progress)
		}
		mem, err := e.ProjectMemory(projectID).Read()
		if err != nil || len(mem.Entries) != 1 || mem.Entries[0] != convention {
			t.Fatalf("wrong notes: %+v %v", mem, err)
		}
		skills, err := e.ProjectMemory(projectID).ListSkills()
		if err != nil || len(skills) != 1 || skills[0].Name != "measurement" {
			t.Fatalf("wrong Skills after resume: %+v %v", skills, err)
		}
		again, err := e.StartProjectDistillation(t.Context(), projectID, DistillationRequest{ResumeReviewID: reviewID})
		if err != nil || again.State != "completed" {
			t.Fatalf("completed run was restarted: %+v %v", again, err)
		}
	})
}

func TestDistillationKeepsUnconfirmedWritesWhenCheckpointFails(t *testing.T) {
	const observation = "The measurement was preserved."
	e, calls := distillWire(t, func(w http.ResponseWriter, req scheduleCompletionRequest, step int) {
		body := `{"changes":[]}`
		if step == 1 {
			body = `{"changes":[{"target":"memory","action":"add","content":"The measurement was preserved.","evidence":[{"seq":1,"quote":"The measurement was preserved."}]}]}`
		}
		scriptedToolReply(w, req, fmt.Sprintf("checkpoint-%d", step), distillBatchName, body)
	})
	p, err := e.CreateProject("Checkpoint failure", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := distillFixtureThread(t, e, p.ID, "Task")
	distillEvent(t, e, th, store.TurnDone, KindUser, observation)
	db := e.store.DB()
	if err := db.Exec(`CREATE TRIGGER reject_distillation_cursor
		BEFORE UPDATE OF cursor_event_id ON project_distillations
		WHEN NEW.cursor_event_id > OLD.cursor_event_id
		BEGIN SELECT RAISE(ABORT,'fixture checkpoint failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	start, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "thread", ThreadID: th.ID})
	if err != nil {
		t.Fatal(err)
	}
	first := waitDistill(t, e, p.ID)
	if first.State != "partial" || !first.Uncertain || first.Progress.Processed != 0 ||
		first.Progress.Remaining != 1 || len(first.Changes) != 1 {
		t.Fatalf("SQLite failure discarded a write or advanced the queue: %+v %+v", first, first.Progress)
	}
	if err := db.Exec("DROP TRIGGER reject_distillation_cursor").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_distillation_resume
		BEFORE UPDATE OF state ON project_distillations
		WHEN OLD.state='partial' AND NEW.state='running'
		BEGIN SELECT RAISE(ABORT,'fixture resume failure'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{ResumeReviewID: start.ReviewID}); err == nil {
		t.Fatal("storage refused the resume claim, but the engine accepted it")
	}
	saved, err := e.ProjectDistillationStatus(t.Context(), p.ID)
	if err != nil || saved.State != "partial" || saved.Progress.Processed != 0 || calls.Load() != 1 {
		t.Fatalf("failed resume consumed the queue: %+v %v", saved, err)
	}
	if err := db.Exec("DROP TRIGGER reject_distillation_resume").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{ResumeReviewID: start.ReviewID}); err != nil {
		t.Fatal(err)
	}
	final := waitDistill(t, e, p.ID)
	mem, err := e.ProjectMemory(p.ID).Read()
	if err != nil || len(mem.Entries) != 1 || final.State != "completed" || final.Uncertain ||
		final.Progress.Processed != 1 || final.Progress.Remaining != 0 || len(final.Changes) != 1 || calls.Load() != 2 {
		t.Fatalf("resuming the pending checkpoint rewrote or lost a prior write: %+v %+v %v", final, final.Progress, err)
	}
}

func TestDistillationCarriesToolContextAndCountsUnavailableInputs(t *testing.T) {
	e, calls := distillWire(t, func(w http.ResponseWriter, req scheduleCompletionRequest, _ int) {
		source := req.Messages[1].Content
		if !strings.Contains(source, "inspect_one()") || !strings.Contains(source, "Visible paired result.") {
			t.Error("the original call must travel with its result")
		}
		for _, text := range []string{"No paired context.", "Failure result.", "Steer lost its caption.", "Steer with an invalid caption."} {
			if strings.Contains(source, text) {
				t.Errorf("an ineligible source reached the reviewer: %q", text)
			}
		}
		scriptedToolReply(w, req, "inspect", distillBatchName, `{"changes":[]}`)
	})
	p, err := e.CreateProject("Evidence pairs", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := distillFixtureThread(t, e, p.ID, "Task")
	turn, _ := distillEvent(t, e, th, store.TurnDone, KindUser, "Review existing evidence.")
	var invalidSteer *store.Event
	for _, ev := range []*store.Event{
		{Kind: "tool_call", AgentID: "worker", ToolCallID: "first", Text: "inspect_one()"},
		{Kind: "tool_result", AgentID: "worker", ToolCallID: "first", Text: "Visible paired result."},
		{Kind: "tool_result", AgentID: "other", ToolCallID: "first", Text: "No paired context."},
		{Kind: "tool_call", AgentID: "worker", ToolCallID: "third", Text: "inspect_two()"},
		{Kind: "tool_result", AgentID: "worker", ToolCallID: "third", Text: "Failure result.", Err: "tool failed"},
		{Kind: KindSteer, Text: "Steer lost its caption."},
		{Kind: KindSteer, Text: "Steer with an invalid caption."},
	} {
		ev.ThreadID, ev.TurnID = th.ID, turn.ID
		if err := e.store.AppendEvent(ev); err != nil {
			t.Fatal(err)
		}
		invalidSteer = ev
	}
	if err := e.store.AppendMessages(th.ID, turn.ID, []store.Message{{Role: "user", EventSeq: invalidSteer.Seq, Content: "Caption lacks its marker."}}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "thread", ThreadID: th.ID}); err != nil {
		t.Fatal(err)
	}
	receipt := waitDistill(t, e, p.ID)
	if receipt.State != "completed" || receipt.Progress.Processed != 6 || receipt.Progress.Skipped != 4 || calls.Load() != 1 {
		t.Fatalf("wrong account of evidence: %+v %+v", receipt, receipt.Progress)
	}
}

func TestDistillationRefusesToCompleteWhenItsSavedSourceQueueIsMissing(t *testing.T) {
	e, calls := distillWire(t, func(w http.ResponseWriter, _ scheduleCompletionRequest, _ int) {
		t.Error("missing queue reached the model")
		http.Error(w, "fixture", http.StatusBadRequest)
	})
	p, err := e.CreateProject("Incomplete database", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	saved := DistillationResult{ReviewID: "saved", State: "interrupted", Scope: "project",
		MemoryEnabled: true, Progress: &DistillationProgress{Total: 2, Processed: 1, Remaining: 1}}
	row, err := saved.row(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.StartProjectDistillation(t.Context(), row); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{ResumeReviewID: saved.ReviewID}); err != nil {
		t.Fatal(err)
	}
	got := waitDistill(t, e, p.ID)
	if got.State != "failed" || got.Progress.Remaining != 1 || calls.Load() != 0 || got.Error == "" {
		t.Fatalf("missing queue reported completion: %+v %+v", got, got.Progress)
	}
	if !e.reviews.stop(time.Second) {
		t.Fatal("the failed review did not release the pool")
	}
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{ResumeReviewID: saved.ReviewID}); !errors.Is(err, ErrRestarting) {
		t.Fatalf("a stopped reviewer pool accepted a resume: %v", err)
	}
}

func TestDistillationKeepsRemainingWorkWhenAnEnabledDestinationIsDisabled(t *testing.T) {
	e, calls := distillWire(t, func(w http.ResponseWriter, req scheduleCompletionRequest, _ int) {
		scriptedToolReply(w, req, "after-enable", distillBatchName, `{"changes":[]}`)
	})
	p, err := e.CreateProject("Changing settings", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := distillFixtureThread(t, e, p.ID, "Task")
	distillEvent(t, e, th, store.TurnDone, KindUser, "Retained requirement.")
	gate := e.reviews.begin(p.ID)
	gate <- struct{}{}
	e.reviews.done()
	var release sync.Once
	unblock := func() { release.Do(func() { <-gate }) }
	defer unblock()

	start, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "project"})
	if err != nil {
		t.Fatal(err)
	}
	update := func(enabled bool) {
		t.Helper()
		if err := e.store.DB().Model(&store.Project{}).Where("id = ?", p.ID).Update("memory_enabled", enabled).Error; err != nil {
			t.Fatal(err)
		}
	}
	update(false)
	unblock()
	stopped := waitDistill(t, e, p.ID)
	if stopped.State != "failed" || stopped.Progress.Remaining != 1 || calls.Load() != 0 {
		t.Fatalf("disabled memory lost the review: %+v %+v", stopped, stopped.Progress)
	}
	update(true)
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{ResumeReviewID: start.ReviewID}); err != nil {
		t.Fatal(err)
	}
	done := waitDistill(t, e, p.ID)
	if done.State != "completed" || done.Progress.Remaining != 0 || calls.Load() != 1 {
		t.Fatalf("enabled memory did not resume: %+v %+v", done, done.Progress)
	}
}
