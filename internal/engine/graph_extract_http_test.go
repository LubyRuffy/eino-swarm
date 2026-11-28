package engine

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

func newGraphReplayHTTPEngine(t *testing.T, reply func(http.ResponseWriter, int, scheduleCompletionRequest)) (*Engine, *scheduleCompletionHTTP) {
	t.Helper()
	requests := &scheduleCompletionHTTP{}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode offline request: %v", err)
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		requests.mu.Lock()
		requests.requests = append(requests.requests, request)
		step := len(requests.requests)
		requests.mu.Unlock()
		if len(request.Tools) != 2 || !requestHasTool(request, ToolReadGraph) || !requestHasTool(request, ToolUpdateGraph) {
			t.Errorf("replay exposed tools outside the graph: %s", requestToolNames(request))
		}
		reply(w, step, request)
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "offline-graph-replay"
	e.pool = provider.New(e.cfg)
	return e, requests
}

func saveGraphReplaySource(t *testing.T, e *Engine, threadID, userText, status string, messages ...store.Message) *store.Turn {
	t.Helper()
	turn := &store.Turn{ThreadID: threadID, UserText: userText}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.store.AppendMessages(threadID, turn.ID, messages); err != nil {
		t.Fatal(err)
	}
	if status != store.TurnRunning {
		if err := e.store.FinishTurn(turn.ID, status, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	return turn
}

// Each stored round starts a fresh model conversation. A provider may reuse a
// tool call ID there, so both rounds must get their own graph write receipt.
func TestGraphReplayKeepsIndependentReceiptsForEachSourceRound(t *testing.T) {
	const repeatedID = "first-call"
	const initialBatch = `{"todos":[{"title":"Review initial report","status":"done","result":"Verified"}],"evidence":[{"url":"https://reports.example.test/first"}]}`
	const revisedBatch = `{"todos":[{"title":"Review revised report"}]}`
	e, requests := newGraphReplayHTTPEngine(t, func(w http.ResponseWriter, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			if text := requestText(request); !strings.Contains(text, "First stored result") || strings.Contains(text, "Second stored result") {
				t.Errorf("first source round: %q", text)
			}
			graphToolCall(w, request, repeatedID, ToolUpdateGraph, initialBatch)
		case 2, 6:
			if receipt := scriptedToolReceipt(request, repeatedID); !strings.Contains(receipt, `"ok":true`) || strings.Contains(receipt, `"replayed":true`) {
				t.Errorf("step %d did not get an independent receipt: %s", step, receipt)
			}
			if step == 2 {
				graphToolCall(w, request, repeatedID, ToolUpdateGraph, initialBatch)
				return
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Recorded."}, "stop")
		case 3:
			if receipt := scriptedToolReceipt(request, repeatedID); !strings.Contains(receipt, `"ok":true`) || !strings.Contains(receipt, `"replayed":true`) {
				t.Errorf("the same call in one source round did not get its frozen receipt: %s", receipt)
			}
			graphToolCall(w, request, repeatedID, ToolUpdateGraph, revisedBatch)
		case 4:
			if receipt := scriptedToolReceipt(request, repeatedID); !strings.Contains(receipt, `"ok":false`) || !strings.Contains(receipt, "different body") {
				t.Errorf("the same call in one source round accepted a changed body: %s", receipt)
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Recorded."}, "stop")
		case 5:
			if text := requestText(request); !strings.Contains(text, "Second stored result") || strings.Contains(text, "First stored result") {
				t.Errorf("second source round: %q", text)
			}
			if digest := graphDigestOf(request); !strings.Contains(digest, "revision 1") || !strings.Contains(digest, "Review initial report") {
				t.Errorf("fresh round did not receive the committed graph digest: %q", digest)
			}
			graphToolCall(w, request, repeatedID, ToolUpdateGraph, revisedBatch)
		default:
			t.Errorf("unexpected replay request %d", step)
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Recorded."}, "stop")
		}
	})
	thread, err := e.CreateThread("Replay fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	saveGraphReplaySource(t, e, thread.ID, "Read the initial report", store.TurnDone, store.Message{Role: string(schema.Assistant), Content: "First stored result"})
	saveGraphReplaySource(t, e, thread.ID, "Read the revised report", store.TurnDone, store.Message{Role: string(schema.Assistant), Content: "Second stored result"})
	turn, err := e.ExtractGraph(thread.ID, false, "")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || len(requests.snapshot()) != 6 {
		t.Fatalf("replay status=%s error=%s requests=%d", got.Status, got.Error, len(requests.snapshot()))
	}
	view, err := e.ReadThreadGraph(t.Context(), thread.ID, store.GraphQuery{All: true, Limit: -1})
	if err != nil || view.Revision != 2 || len(view.Todos) != 2 {
		t.Fatalf("both rounds must be visible: revision=%d todos=%+v err=%v", view.Revision, view.Todos, err)
	}
	if view.Todos[0].Title != "Review initial report" || view.Todos[0].Status != graph.TodoOpen || view.Todos[0].Result != "" || view.Todos[1].Title != "Review revised report" {
		t.Fatalf("replay changed source order or claimed an unverified result: %+v", view.Todos)
	}
}

// A response can already be in the transcript when a turn fails, pauses or is
// cancelled. Its partial output cannot become a completed source round.
func TestGraphReplayUsesOnlyCompletedConversationRounds(t *testing.T) {
	e, requests := newGraphReplayHTTPEngine(t, func(w http.ResponseWriter, step int, request scheduleCompletionRequest) {
		if text := requestText(request); step != 1 || !strings.Contains(text, "Completed report was available") {
			t.Errorf("unexpected replay source at step %d: %q", step, text)
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "Nothing to add."}, "stop")
	})
	thread, err := e.CreateThread("Replay source fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	saveGraphReplaySource(t, e, thread.ID, "Verify the first report", store.TurnDone, store.Message{Role: string(schema.Assistant), Content: "Completed report was available"})
	for _, status := range []string{store.TurnError, store.TurnCancelled, store.TurnRunning} {
		saveGraphReplaySource(t, e, thread.ID, "Investigate again", status, store.Message{Role: string(schema.Assistant), Content: "An incomplete assertion from " + status})
	}
	turn, err := e.ExtractGraph(thread.ID, false, "")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || len(requests.snapshot()) != 1 {
		t.Fatalf("replay status=%s error=%s final=%q requests=%d", got.Status, got.Error, got.Final, len(requests.snapshot()))
	}
}

// The replay caption is generated by the host. It must never be delivered as a
// person's answer to a different, already running turn.
func TestExtractGraphWhileWaitingForHumanLeavesTheQuestionAndGraphIntact(t *testing.T) {
	const callID = "human-question"
	const questionID = "decision"
	args := restartReplayAskArguments(t, questionID)
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		if step == 1 {
			graphToolCall(w, request, callID, ToolAskUser, args)
			return
		}
		if receipt := scriptedToolReceipt(request, callID); !strings.Contains(receipt, `"one"`) || strings.Contains(receipt, GraphExtractText()) {
			t.Errorf("ask received a reply other than the actual human choice: %s", receipt)
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "Received your choice."}, "stop")
	})
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	thread, err := e.CreateThread("Replay admission fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	source := saveGraphReplaySource(t, e, thread.ID, "Inspect the report", store.TurnDone, store.Message{Role: string(schema.Assistant), Content: "An inspection already finished"})
	if _, _, err := e.store.ApplyGraph(t.Context(), store.GraphCaller{ThreadID: thread.ID, TurnID: source.ID, AgentID: "manager"}, graph.Batch{
		Todos: []graph.TodoInput{{Title: "Retain this review"}},
	}); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Ask me which way to proceed")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if e.Status(thread.ID).Running {
			_ = e.Interrupt(thread.ID)
			waitSettled(t, e, thread.ID)
		}
	}()
	waitAskArmed(t, e, thread.ID)
	if extracted, err := e.ExtractGraph(thread.ID, true, thread.ID); !errors.Is(err, ErrBusy) || extracted != nil {
		t.Fatalf("extraction must refuse the active question: turn=%+v err=%v", extracted, err)
	}
	if !e.Status(thread.ID).AwaitingAnswer {
		t.Fatal("the original ask was consumed by extraction")
	}
	view, err := e.ReadThreadGraph(t.Context(), thread.ID, store.GraphQuery{All: true, Limit: -1})
	if err != nil || view.Revision != 1 || len(view.Todos) != 1 || view.Todos[0].Title != "Retain this review" {
		t.Fatalf("the refused replay changed the existing graph: %+v err=%v", view, err)
	}
	if err := e.AnswerTurn(thread.ID, callID, AskAnswers{questionID: {Answers: []string{"one"}}}); err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone {
		t.Fatalf("the answered original turn did not complete: %+v", got)
	}
}

func TestExtractGraphRefusesPlanningBeforeClearingOrCallingTheModel(t *testing.T) {
	e, requests := newGraphReplayHTTPEngine(t, func(w http.ResponseWriter, step int, request scheduleCompletionRequest) {
		if step == 1 {
			graphToolCall(w, request, "unexpected-write", ToolUpdateGraph, `{"todos":[{"title":"Record a historical result"}]}`)
			return
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "Tried to record history."}, "stop")
	})
	thread, err := e.CreateThread("Planning replay fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	source := saveGraphReplaySource(t, e, thread.ID, "Inspect the report", store.TurnDone, store.Message{Role: string(schema.Assistant), Content: "The inspection already finished"})
	if _, _, err := e.store.ApplyGraph(t.Context(), store.GraphCaller{ThreadID: thread.ID, TurnID: source.ID, AgentID: "manager"}, graph.Batch{
		Todos: []graph.TodoInput{{Title: "Retain the earlier review"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.SetPlanMode(thread.ID, true); err != nil {
		t.Fatal(err)
	}
	turn, err := e.ExtractGraph(thread.ID, true, thread.ID)
	if turn != nil {
		waitForTurn(t, e, turn.ID)
		waitSettled(t, e, thread.ID)
	}
	if !errors.Is(err, graph.ErrInvalid) || turn != nil || len(requests.snapshot()) != 0 {
		t.Errorf("planning admitted replay: turn=%+v err=%v requests=%d", turn, err, len(requests.snapshot()))
	}
	view, err := e.ReadThreadGraph(t.Context(), thread.ID, store.GraphQuery{All: true, Limit: -1})
	if err != nil || view.Revision != 1 || len(view.Todos) != 1 || view.Todos[0].Title != "Retain the earlier review" {
		t.Fatalf("planning lost the existing graph: %+v err=%v", view, err)
	}
}
