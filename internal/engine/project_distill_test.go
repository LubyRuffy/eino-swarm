package engine

import (
	"context"
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

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	openai "github.com/meguminnnnnnnnn/go-openai"
)

func distillWire(t *testing.T, serve func(http.ResponseWriter, scheduleCompletionRequest, int)) (*Engine, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		serve(w, request, int(calls.Add(1)))
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "fixture"
	e.pool = provider.New(e.cfg)
	return e, &calls
}

func distillFixtureThread(t *testing.T, e *Engine, projectID, title string) *store.Thread {
	t.Helper()
	thread, err := e.CreateThread(title, "", projectID)
	if err != nil {
		t.Fatal(err)
	}
	return thread
}

func distillEvent(t *testing.T, e *Engine, th *store.Thread, status, kind, text string) (store.Turn, store.Event) {
	t.Helper()
	turn := store.Turn{ThreadID: th.ID, ProjectID: th.ProjectID, ProviderID: th.ProviderID, Status: status}
	if err := e.store.CreateTurn(&turn); err != nil {
		t.Fatal(err)
	}
	event := store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: kind, Text: text}
	if err := e.store.AppendEvent(&event); err != nil {
		t.Fatal(err)
	}
	return turn, event
}

func waitDistill(t *testing.T, e *Engine, id string) DistillationResult {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		got, err := e.ProjectDistillationStatus(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if got.State != "running" && got.State != "pausing" {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("distillation never left running")
	return DistillationResult{}
}

// Two conversations can both hold an event with the same per-thread seq. The
// review keys sources by its own seq and still shows each original event_seq,
// and a conversation outside the project never enters the input.
func TestDistillationKeepsPeerSequenceIdentityWithinTheProject(t *testing.T) {
	const first, second, foreign = "Preserve the working convention.", "Peer observed distinct material.", "Private unrelated evidence."
	e, calls := distillWire(t, func(w http.ResponseWriter, req scheduleCompletionRequest, step int) {
		source := ""
		for _, message := range req.Messages {
			if message.Role == "user" {
				source += message.Content
			}
		}
		if !strings.Contains(source, first) || !strings.Contains(source, second) ||
			strings.Contains(source, foreign) || strings.Count(source, "event_seq=1 ") != 2 {
			t.Errorf("wrong captured scope: %s", source)
		}
		scriptedToolReply(w, req, "distill", distillBatchName, `{
		  "changes":[{"target":"memory","action":"add","content":"Preserve the working convention.","evidence":[{"seq":1,"quote":"Preserve the working convention."}]}]
		}`)
	})
	project, err := e.CreateProject("Scoped review", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	a := distillFixtureThread(t, e, project.ID, "First")
	b := distillFixtureThread(t, e, project.ID, "Peer")
	other := distillFixtureThread(t, e, "", "Foreign")
	distillEvent(t, e, a, store.TurnDone, KindUser, first)
	distillEvent(t, e, b, store.TurnDone, KindUser, second)
	outside := store.Turn{ThreadID: other.ID, Status: store.TurnDone}
	if err := e.store.CreateTurn(&outside); err != nil {
		t.Fatal(err)
	}
	if err := e.store.AppendEvent(&store.Event{ThreadID: other.ID, TurnID: outside.ID, Kind: KindUser, Text: foreign}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartProjectDistillation(t.Context(), project.ID, DistillationRequest{Scope: "project"}); err != nil {
		t.Fatal(err)
	}
	result := waitDistill(t, e, project.ID)
	if result.State != "completed" || len(result.Changes) != 1 || result.Progress.Processed != 2 || calls.Load() != 1 {
		t.Fatalf("wrong outcome: %+v / requests %d", result, calls.Load())
	}
}

func TestDistillationRevalidatesEditedSteerBeforeAnyWrite(t *testing.T) {
	ready, proceed := make(chan struct{}), make(chan struct{})
	var proceedOnce sync.Once
	unblock := func() { proceedOnce.Do(func() { close(proceed) }) }
	defer unblock()
	e, _ := distillWire(t, func(w http.ResponseWriter, req scheduleCompletionRequest, step int) {
		if step == 1 {
			close(ready)
			<-proceed
			scriptedToolReply(w, req, "stale", distillBatchName, `{
				"changes":[{"target":"memory","action":"add","content":"Preserve the previous wording.","evidence":[{"seq":1,"quote":"Preserve the previous wording."}]}]
			}`)
			return
		}
		if receipt := scriptedToolReceipt(req, "stale"); !strings.Contains(receipt, "steer source 1 changed") {
			t.Errorf("the actual tool accepted modified evidence: %s", receipt)
		}
		writeContextFlowReply(w, req.Stream, map[string]any{"content": "Evidence changed."}, "stop")
	})
	p, err := e.CreateProject("Mutable source", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := distillFixtureThread(t, e, p.ID, "Mutable")
	turn, event := distillEvent(t, e, th, store.TurnDone, KindSteer, "Preserve the previous wording.")
	if err := e.store.AppendMessages(th.ID, turn.ID, []store.Message{
		{Role: "user", EventSeq: event.Seq, Content: steerModelCaption(event.Text)},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "thread", ThreadID: th.ID}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ready:
	case <-time.After(4 * time.Second):
		t.Fatal("reviewer never reached HTTP")
	}
	if err := e.store.ReviseSteerMessage(th.ID, event.Seq, steerModelCaption("Replace the earlier instruction.")); err != nil {
		t.Fatal(err)
	}
	unblock()
	result := waitDistill(t, e, p.ID)
	if result.State != "failed" || !strings.Contains(result.Error, "steer source 1 changed") || len(result.Changes) != 0 {
		t.Fatalf("modified source resulted in a write: %+v", result)
	}
}

func TestDistillationMissingCompletionIsInterruptedWithoutReplay(t *testing.T) {
	e := newTestEngine(t)
	p, err := e.CreateProject("Interrupted review", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	saved := DistillationResult{ReviewID: "old-process", Scope: "project", State: "running"}
	row, err := saved.row(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.StartProjectDistillation(ctx, row); err != nil {
		t.Fatal(err)
	}
	status, err := e.ProjectDistillationStatus(ctx, p.ID)
	if err != nil || status.State != "interrupted" || !status.Uncertain || status.ReviewID != saved.ReviewID {
		t.Fatalf("dead process was left running: %+v, %v", status, err)
	}
	if err := e.DeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	if persisted, err := e.store.ProjectDistillationReceipt(ctx, p.ID); err != nil || persisted != nil {
		t.Fatalf("project left behind a receipt: %+v, %v", persisted, err)
	}
}

func TestDistillationScopesKeepRetainedSourcesAndHumanOnlyFailedTurns(t *testing.T) {
	e := newTestEngine(t)
	project, err := e.CreateProject("Subject", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	current := distillFixtureThread(t, e, project.ID, "Current")
	peer := distillFixtureThread(t, e, project.ID, "Peer")
	older, _ := distillEvent(t, e, current, store.TurnDone, KindUser, "First working rule.")
	latest, _ := distillEvent(t, e, current, store.TurnDone, KindUser, "Latest working rule.")
	distillEvent(t, e, current, store.TurnRunning, KindUser, "Running text.")
	failure, _ := distillEvent(t, e, peer, store.TurnError, KindUser, "Retain this requirement even after failure.")
	if err := e.store.AppendEvent(&store.Event{ThreadID: peer.ID, TurnID: failure.ID, Kind: swarm.NotifyAgentMessage.String(), Text: "Unsuccessful guess."}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		thread string
		latest bool
		want   int
	}{
		{current.ID, true, 1},
		{current.ID, false, 2},
		{"", false, 3},
	} {
		t.Run(fmt.Sprintf("sources-%d", tc.want), func(t *testing.T) {
			turns, omitted, err := e.store.ProjectDistillationHistory(t.Context(), project.ID, tc.thread, tc.latest, distillEventKinds)
			input := selectDistillationSources(turns, omitted)
			if err != nil || input.omitted || len(input.memory) != tc.want {
				t.Fatalf("wrong bounded history: %+v, %v", input, err)
			}
			for _, source := range input.sources {
				if source.failed && source.event.Kind != KindUser || source.event.TurnID != latest.ID && tc.latest ||
					strings.Contains(source.event.Text, "Running") || strings.Contains(source.event.Text, "guess") ||
					(source.event.TurnID == older.ID && tc.latest) {
					t.Fatalf("wrong evidence authority: %+v", source)
				}
			}
		})
	}
}

func TestDistillationFollowsCurrentOwnershipBeforeWriteAndTrace(t *testing.T) {
	ready, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	e, _ := distillWire(t, func(w http.ResponseWriter, req scheduleCompletionRequest, step int) {
		if step == 1 {
			close(ready)
			<-release
			scriptedToolReply(w, req, "old-owner", distillBatchName, `{
				"changes":[{"target":"memory","action":"add","content":"Keep the old project rule.","evidence":[{"seq":1,"quote":"Keep the old project rule."}]}]
			}`)
			return
		}
		writeContextFlowReply(w, req.Stream, map[string]any{"content": "The project scope changed."}, "stop")
	})
	old, err := e.CreateProject("Original owner", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	next, err := e.CreateProject("New owner", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := distillFixtureThread(t, e, old.ID, "Moved")
	turn, _ := distillEvent(t, e, th, store.TurnDone, KindUser, "Keep the old project rule.")
	if _, err := e.StartProjectDistillation(t.Context(), old.ID, DistillationRequest{Scope: "thread", ThreadID: th.ID}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ready:
	case <-time.After(4 * time.Second):
		t.Fatal("reviewer never reached HTTP")
	}
	if err := e.MoveThread(th.ID, next.ID); err != nil {
		t.Fatal(err)
	}
	unblock()
	result := waitDistill(t, e, old.ID)
	if result.State != "failed" || result.ThreadID != "" || result.TurnID != "" || len(result.Changes) != 0 {
		t.Fatalf("stale review still claimed ownership: %+v", result)
	}
	trace, err := e.store.ListLLMCalls(turn.ID)
	if err != nil || len(trace) != 0 {
		t.Fatalf("late project review was attributed across ownership: %+v, %v", trace, err)
	}
}

func TestDistillationToolEvidenceRequiresARealNonDerivedReceipt(t *testing.T) {
	evs := []store.Event{
		{Seq: 1, Kind: "tool_call", ToolCallID: "actual", Text: "inspect()"},
		{Seq: 2, Kind: "tool_result", ToolCallID: "actual", Text: "Observed material."},
		{Seq: 3, Kind: "tool_call", ToolCallID: "fail", Text: "inspect()"},
		{Seq: 4, Kind: "tool_result", ToolCallID: "fail", Text: `{"ok":false,"error":"rejected"}`},
		{Seq: 5, Kind: "tool_call", ToolCallID: "derived", Text: "update_graph()"},
		{Seq: 6, Kind: "tool_result", ToolCallID: "derived", Text: "Previous notes"},
		{Seq: 7, Kind: "tool_result", ToolCallID: "unpaired", Text: "Unpaired output"},
	}
	got := selectDistillationSources([]store.DistillationTurn{{Turn: store.Turn{Status: store.TurnDone}, Events: evs}}, false)
	if len(got.sources) != 1 || len(got.memory) != 1 || got.sources[2].event.Seq != 2 ||
		strings.Contains(got.transcript, "rejected") || strings.Contains(got.transcript, "Previous notes") {
		t.Fatalf("unexpected tool authority: %+v", got)
	}
}

func TestDistillationRefusesForeignAndMalformedProposalsBeforeAcceptingCanonicalSteer(t *testing.T) {
	const quote = "Preserve the current observation."
	proposals := []struct{ args, refusal string }{
		{`{}`, "provide the changes array"},
		{`{"changes":[{"target":"memory","action":"add","content":"Sample","evidence":[{"seq":71,"quote":"Source from elsewhere."}]}]}`, "outside this target's evidence"},
		{`{"changes":[{"target":"memory","action":"add","content":"Sample","evidence":[{"seq":1,"quote":"Not in the source."}]}]}`, "evidence validation failed"},
		{`{"changes":[{"target":"memory","action":"add","content":"There were 19 observations.","evidence":[{"seq":1,"quote":"Preserve the current observation."}]}]}`, "numeric"},
		{`{"changes":[{"target":"memory","action":"add","content":"Preserve the current observation.","evidence":[{"seq":1,"quote":"Preserve the current observation."}]}]}`, ""},
	}
	e, requests := distillWire(t, func(w http.ResponseWriter, req scheduleCompletionRequest, step int) {
		if step > 1 {
			receipt := scriptedToolReceipt(req, fmt.Sprintf("proposal-%d", step-1))
			if !strings.Contains(receipt, proposals[step-2].refusal) ||
				!strings.Contains(receipt, `"success":false`) {
				t.Errorf("guard did not refuse proposal %d: %s", step-1, receipt)
			}
			if step == 4 {
				for _, field := range []string{`"index":0`, `"reason":"quote_mismatch"`, `"source_text":"` + quote + `"`} {
					if !strings.Contains(receipt, field) {
						t.Errorf("refusal omitted %q from the model's repair instructions: %s", field, receipt)
					}
				}
			}
		}
		if step <= len(proposals) {
			scriptedToolReply(w, req, fmt.Sprintf("proposal-%d", step), distillBatchName, proposals[step-1].args)
		} else {
			writeContextFlowReply(w, req.Stream, map[string]any{"content": "Finished."}, "stop")
		}
	})
	e.cfg.Memory.ReviewMaxIterations = 10
	p, err := e.CreateProject("Protected writes", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := distillFixtureThread(t, e, p.ID, "Steer evidence")
	turn, ev := distillEvent(t, e, th, store.TurnDone, KindSteer, quote)
	if err := e.store.AppendMessages(th.ID, turn.ID, []store.Message{{Role: "user", EventSeq: ev.Seq, Content: steerModelCaption(quote)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "thread", ThreadID: th.ID}); err != nil {
		t.Fatal(err)
	}
	result := waitDistill(t, e, p.ID)
	if result.State != "completed" || len(result.Changes) != 1 || int(requests.Load()) != len(proposals) {
		t.Fatalf("malformed proposals changed storage or exhausted the run: %+v, %d calls", result, requests.Load())
	}
}

func TestDistillationRetriesTheUnwrittenBatchOnce(t *testing.T) {
	const source = "Keep the original project convention."
	const peerSource = "A peer discussion has an independent convention."
	for _, scenario := range []string{"citation", "provider outage", "exhausted", "first byte"} {
		t.Run(scenario, func(t *testing.T) {
			e, calls := distillWire(t, func(w http.ResponseWriter, req scheduleCompletionRequest, step int) {
				if !strings.Contains(req.Messages[1].Content, source) {
					t.Error("retry lost the original batch input")
				}
				if strings.Contains(req.Messages[1].Content, peerSource) {
					t.Error("retry or explicit resume widened the task to the project")
				}
				if scenario == "first byte" {
					if strings.Contains(req.Messages[0].Content, "fresh attempt at the same batch") {
						t.Error("a configured first-byte deadline started a fresh batch retry")
					}
					time.Sleep(1250 * time.Millisecond)
					return
				}
				if step == 2 && !strings.Contains(req.Messages[0].Content, "fresh attempt at the same batch") {
					t.Error("the retry did not start a new reviewer on the frozen batch")
				}
				if step == 1 && scenario == "provider outage" {
					http.Error(w, "temporary service outage", http.StatusServiceUnavailable)
					return
				}
				quote := source
				if step == 1 || (scenario == "exhausted" && step == 2) {
					quote = "A quote absent from the original input."
				}
				scriptedToolReply(w, req, fmt.Sprintf("batch-%d", step), distillBatchName, fmt.Sprintf(
					`{"changes":[{"target":"memory","action":"add","content":%q,"evidence":[{"seq":1,"quote":%q}]}]}`,
					source, quote))
			})
			e.cfg.Memory.ReviewMaxIterations = 1
			if scenario == "first byte" {
				e.cfg.Models.Providers[0].FirstByteTimeoutSeconds = 1
			}
			p, err := e.CreateProject("Bounded retry", "", "", true)
			if err != nil {
				t.Fatal(err)
			}
			th := distillFixtureThread(t, e, p.ID, "Original task")
			distillEvent(t, e, th, store.TurnDone, KindUser, source)
			peer := distillFixtureThread(t, e, p.ID, "Peer discussion")
			distillEvent(t, e, peer, store.TurnDone, KindUser, peerSource)
			_, err = e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "thread", ThreadID: th.ID})
			if err != nil {
				t.Fatal(err)
			}
			result := waitDistill(t, e, p.ID)
			if scenario == "first byte" {
				// Provider discovery can make its own request inside one reviewer.
				if result.State != "failed" || result.ErrorCode != "first_byte_timeout" ||
					result.ErrorProviderID != th.ProviderID || calls.Load() > 2 {
					t.Fatalf("provider deadline was hidden or blindly retried: %+v (%d calls)", result, calls.Load())
				}
				return
			}
			if scenario == "exhausted" {
				if result.State != "failed" || result.ErrorCode != "evidence" ||
					result.Progress.Processed != 0 || len(result.Changes) != 0 || calls.Load() != 2 {
					t.Fatalf("failed retry moved the cursor or was not bounded: %+v (%d calls)", result, calls.Load())
				}
				resumed, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{ResumeReviewID: result.ReviewID})
				if err != nil || resumed.Scope != "thread" || resumed.ThreadID != th.ID {
					t.Fatalf("resume changed the saved task scope: %+v %v", resumed, err)
				}
				done := waitDistill(t, e, p.ID)
				if done.State != "completed" || done.Scope != "thread" || done.ThreadID != th.ID ||
					done.Progress.Total != 1 || done.Progress.Processed != 1 || calls.Load() != 3 {
					t.Fatalf("resume processed outside the original task: %+v (%d calls)", done, calls.Load())
				}
			} else if result.State != "completed" || result.Progress.Processed != 1 || len(result.Changes) != 1 || calls.Load() != 2 {
				t.Fatalf("retry did not save one note and accept one batch: %+v (%d calls)", result, calls.Load())
			}
		})
	}
}

func TestDistillationFailureSuggestsConfigurationWithoutAutomaticallyRepeatingIt(t *testing.T) {
	for _, tc := range []struct {
		err  error
		stop string
		code string
	}{
		{err: errors.New("maximum context length"), code: "context_overflow"},
		{err: provider.IdleTimeoutError{Idle: time.Second}, code: "idle_timeout"},
		{err: context.DeadlineExceeded, code: "agent_timeout"},
		{err: &openai.APIError{HTTPStatusCode: http.StatusTooManyRequests}, code: "rate_limited"},
		{err: &openai.APIError{HTTPStatusCode: http.StatusUnauthorized}, code: "model_settings"},
		{stop: "iteration_limit", code: "iteration_limit"},
		{err: errors.New("unknown storage failure")},
	} {
		if code, retry := distillationFailure(tc.err, tc.stop, false); code != tc.code || retry {
			t.Errorf("error=%v stop=%q: got %q retry=%v, want %q without an automatic retry", tc.err, tc.stop, code, retry, tc.code)
		}
	}
}

func TestDistillationCancelsAQueuedReviewAtShutdown(t *testing.T) {
	e := newTestEngine(t)
	p, err := e.CreateProject("Queued review", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := distillFixtureThread(t, e, p.ID, "Queued evidence")
	distillEvent(t, e, th, store.TurnDone, KindUser, "Keep the existing observations.")
	gate := e.reviews.begin(p.ID)
	gate <- struct{}{}
	e.reviews.done()
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "project"}); err != nil {
		t.Fatal(err)
	}
	if !e.reviews.stop(time.Second) {
		t.Fatal("shutdown did not cancel the queued review")
	}
	<-gate
	result := waitDistill(t, e, p.ID)
	if result.State != "interrupted" || result.Uncertain {
		t.Fatalf("queued review never wrote and must report a known interruption: %+v", result)
	}
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "project"}); err != ErrRestarting {
		t.Fatalf("stopped reviewer pool accepted a new review: %v", err)
	}
}

func TestDistillationDoesNotRebaseNotesAfterAnIndependentEdit(t *testing.T) {
	ready, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	e, calls := distillWire(t, func(w http.ResponseWriter, req scheduleCompletionRequest, step int) {
		if step == 1 {
			close(ready)
			<-release
		}
		scriptedToolReply(w, req, "notes-conflict", distillBatchName, `{
			"changes":[{"target":"memory","action":"add","content":"Retain the observed preference.","evidence":[{"seq":1,"quote":"Retain the observed preference."}]}]
		}`)
	})
	p, err := e.CreateProject("Concurrent notes", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := distillFixtureThread(t, e, p.ID, "Notes")
	distillEvent(t, e, th, store.TurnDone, KindUser, "Retain the observed preference.")
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "latest_turn", ThreadID: th.ID}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ready:
	case <-time.After(4 * time.Second):
		t.Fatal("model never observed the notes snapshot")
	}
	if _, _, err := e.ProjectMemory(p.ID).Add("A human saved different notes."); err != nil {
		t.Fatal(err)
	}
	unblock()
	result := waitDistill(t, e, p.ID)
	notes, err := e.ProjectMemory(p.ID).Read()
	if err != nil || result.State != "failed" || result.Error == "" || len(result.Changes) != 0 ||
		calls.Load() != 1 || len(notes.Entries) != 1 || notes.Entries[0] != "A human saved different notes." {
		t.Fatalf("conflicted notes were rebased: %+v notes=%+v calls=%d err=%v", result, notes, calls.Load(), err)
	}
}

func TestDistillationDropsADeletedTraceAnchorBeforeModelInput(t *testing.T) {
	e, calls := distillWire(t, func(w http.ResponseWriter, req scheduleCompletionRequest, _ int) {
		t.Errorf("a deleted turn reached the model: %+v", req)
		http.Error(w, "unexpected request", http.StatusBadRequest)
	})
	p, err := e.CreateProject("Deleted anchor", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th := distillFixtureThread(t, e, p.ID, "Evidence to delete")
	distillEvent(t, e, th, store.TurnDone, KindUser, "Keep the earlier requirement.")
	gate := e.reviews.begin(p.ID)
	gate <- struct{}{}
	e.reviews.done()
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{Scope: "project"}); err != nil {
		<-gate
		t.Fatal(err)
	}
	err = e.DeleteThread(th.ID)
	<-gate
	if err != nil {
		t.Fatal(err)
	}
	result := waitDistill(t, e, p.ID)
	if result.State != "failed" || result.TurnID != "" || calls.Load() != 0 {
		t.Fatalf("deleted turn still has reviewer work or a Trace: %+v requests=%d", result, calls.Load())
	}
	if _, err := e.StartProjectDistillation(t.Context(), p.ID, DistillationRequest{ResumeReviewID: result.ReviewID}); err != nil {
		t.Fatal(err)
	}
	finished := waitDistill(t, e, p.ID)
	if finished.State != "completed" || finished.Progress.Remaining != 0 || finished.Progress.Skipped != 1 || calls.Load() != 0 {
		t.Fatalf("resume did not account for the vanished source: %+v %+v", finished, finished.Progress)
	}
}
