package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

func writeHistoryTool(w http.ResponseWriter, request scheduleCompletionRequest, id, name, args string) {
	writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}, "tool_calls")
}

func replyHistorySessionMemoryFixture(t *testing.T, w http.ResponseWriter, request scheduleCompletionRequest) bool {
	if len(request.Messages) == 0 || request.Messages[0].Content != sessionMemoryPrompt() {
		return false
	}
	if !request.Stream || len(request.Tools) != 0 || len(request.Messages) != 2 || request.Messages[1].Role != "user" {
		t.Error("session briefing lost its separate tool-free protocol")
	}
	writeContextFlowReply(w, request.Stream, map[string]any{"content": "The original source records and unfinished worker remain available for verification."}, "stop")
	return true
}

// A retained worker keeps owner A while its actual notifications reach check B.
// Losing only B's best-effort call row must not make a real read invisible to
// B's answer gate. No current-turn fallback or thread-wide sticky flag is used.
func TestHistoryReadByRetainedWorkerCannotEscapeASuccessorAfterItsCallWriteFails(t *testing.T) {
	const original = "The original record explicitly withholds approval."
	const unsafe = "An unsupported historical approval must not be published."
	queryRelease, workerReady, collecting := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var managers, workers, failedWrites atomic.Int32
	var readProved atomic.Bool
	var secondID atomic.Value
	secondID.Store("")
	var peerID, workerID string
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid request", 400)
			return
		}
		if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		if replyHistorySessionMemoryFixture(t, w, request) {
			return
		}
		if len(request.Messages) > 0 && strings.HasPrefix(request.Messages[0].Content, "## Thread wake authorization classification") {
			writeContextFlowReply(w, false, map[string]any{"content": `{"decision":"unchanged","quote":""}`}, "stop")
			return
		}
		manager := false
		for _, tool := range request.Tools {
			manager = manager || tool.Function.Name == "spawn_agent"
		}
		if !manager {
			if workers.Add(1) == 1 {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"id\":\"retained-source\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"Waiting for the next check.\"},\"finish_reason\":null}]}\n\n")
				w.(http.Flusher).Flush()
				close(workerReady)
				select {
				case <-queryRelease:
				case <-r.Context().Done():
					return
				}
				args, _ := json.Marshal(map[string]any{"sql": "SELECT content FROM messages WHERE thread_id = ?", "params": []string{peerID}})
				writeHistoryTool(w, request, "retained-source-call", ToolQueryHistory, string(args))
				return
			}
			for _, message := range request.Messages {
				if message.Role == "tool" && strings.Contains(message.Content, original) && strings.Contains(message.Content, `"ok":true`) {
					readProved.Store(true)
				}
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Observed original: " + original}, "stop")
			return
		}
		switch step := managers.Add(1); step {
		case 1:
			writeHistoryTool(w, request, "retained-spawn", "spawn_agent", `{"role":"retained-reader","task":"Wait for the next check before reading original history."}`)
		case 2:
			for _, message := range request.Messages {
				if message.Role == "tool" && message.ToolCallID == "retained-spawn" {
					var receipt struct {
						AgentID string `json:"agent_id"`
					}
					if json.Unmarshal([]byte(message.Content), &receipt) == nil {
						workerID = receipt.AgentID
					}
				}
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The worker remains active under the existing wake."}, "stop")
		case 3:
			if workerID == "" {
				t.Error("missing original worker identity")
			}
			close(collecting)
			args, _ := json.Marshal(map[string]any{"agent_ids": []string{workerID}, "timeout_s": 10})
			writeHistoryTool(w, request, "retained-collect", "wait_agents", string(args))
		default:
			writeContextFlowReply(w, request.Stream, map[string]any{"content": unsafe}, "stop")
		}
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "history-retained-fixture"
	e.pool = provider.New(e.cfg)
	project, err := e.CreateProject("Retained history source", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := e.CreateThread("Original disapproval", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	peerID = peer.ID
	if err := e.store.AppendMessages(peerID, "", []store.Message{{Role: "user", Source: store.MessageSourceHuman, Content: original}}); err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Retained source checks", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	wake, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: thread.ID, EveryS: 3600, Title: "Retained source ownership", Prompt: "Inspect the ongoing work.", CreatedBy: store.ScheduleCreatedHuman})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.CancelSchedule(wake.ID) })
	first, err := e.StartTurn(thread.ID, "Start the assigned background check.")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-workerReady:
	case <-time.After(5 * time.Second):
		t.Fatal("original worker did not reach its real streaming request")
	}
	if got := waitForTurn(t, e, first.ID); got.Status != store.TurnDone {
		t.Fatalf("first check did not retain its worker: %+v", got)
	}
	waitSettled(t, e, thread.ID)
	if err := e.store.DB().Callback().Create().Before("gorm:create").Register("retained_history_call_outage", func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*store.Event); ok && event.TurnID == secondID.Load().(string) && event.Kind == "tool_call" && toolCallName(event.Text) == ToolQueryHistory {
			failedWrites.Add(1)
			_ = tx.AddError(fmt.Errorf("injected successor history call write failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	second, err := e.StartTurn(thread.ID, "Collect the existing worker's original evidence.")
	if err != nil {
		t.Fatal(err)
	}
	secondID.Store(second.ID)
	select {
	case <-collecting:
	case <-time.After(5 * time.Second):
		t.Fatal("successor did not reach real worker collection")
	}
	close(queryRelease)
	got := waitForTurn(t, e, second.ID)
	waitSettled(t, e, thread.ID)
	if failedWrites.Load() == 0 || !readProved.Load() || workers.Load() != 2 {
		t.Fatalf("counterexample missed actual call failure or source read: writes=%d read=%t worker=%d final=%+v", failedWrites.Load(), readProved.Load(), workers.Load(), got)
	}
	firstEvents, _ := e.store.ListTurnEvents(first.ID)
	ownerMarker := false
	for _, event := range firstEvents {
		ownerMarker = ownerMarker || event.Kind == "history_read_started" && event.ToolCallID == "retained-source-call"
	}
	if !ownerMarker {
		t.Fatal("retained worker was rebound away from its actual original owner")
	}
	if got.Status == store.TurnDone && strings.Contains(got.Final, unsafe) {
		t.Fatalf("successor published free history prose after losing its actual worker call: %+v", got)
	}
}

// A manager's own calls are insufficient to detect delegated history reads.
// The real worker must read, finish and be collected before host publication.
func TestDelegatedHistoryReadRequiresTheManagerToPublishACanonicalReport(t *testing.T) {
	var managers, workers atomic.Int32
	var peerID string
	const original = "This observed result remains unverified; it is not an approved human decision."
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid request", 400)
			return
		}
		if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		if replyHistorySessionMemoryFixture(t, w, request) {
			return
		}
		manager := false
		for _, tool := range request.Tools {
			manager = manager || tool.Function.Name == "spawn_agent"
		}
		if !manager {
			if workers.Add(1) == 1 {
				writeHistoryTool(w, request, "worker-source-call", ToolQueryHistory, `{}`)
			} else {
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "Collected source reference: " + peerID}, "stop")
			}
			return
		}
		switch step := managers.Add(1); step {
		case 1:
			writeHistoryTool(w, request, "history-spawn", "spawn_agent", `{"role":"source-reader","task":"Inspect original records and return their source references."}`)
		case 2:
			var id string
			for _, message := range request.Messages {
				if message.Role == "tool" && message.ToolCallID == "history-spawn" {
					var receipt struct {
						AgentID string `json:"agent_id"`
					}
					if json.Unmarshal([]byte(message.Content), &receipt) == nil {
						id = receipt.AgentID
					}
				}
			}
			if id == "" {
				t.Error("real spawn returned no worker identity")
			}
			args, _ := json.Marshal(map[string]any{"agent_ids": []string{id}, "timeout_s": 10})
			writeHistoryTool(w, request, "history-collect", "wait_agents", string(args))
		case 3:
			collected := false
			for _, message := range request.Messages {
				collected = collected || message.Role == "tool" && message.ToolCallID == "history-collect" && strings.Contains(message.Content, `"status":"done"`)
			}
			if !collected {
				t.Error("manager tried to publish before actual worker collection")
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": historyAnswerJSON(peerID)}, "stop")
		default:
			t.Errorf("unexpected manager request %d", step)
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Unexpected request."}, "stop")
		}
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "history-worker-fixture"
	e.pool = provider.New(e.cfg)
	project, err := e.CreateProject("Delegated source report", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := e.CreateThread("Original assistant result", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	peerID = peer.ID
	if err := e.store.AppendMessages(peerID, "", []store.Message{{Role: "assistant", Content: original}}); err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Manager report", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Delegate reading original records, collect the worker, and report the sources.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || !strings.Contains(got.Final, original) || !strings.Contains(got.Final, "assistant") {
		t.Fatalf("delegated source report failed: %+v", got)
	}
	if managers.Load() != 3 || workers.Load() != 2 {
		t.Fatalf("manager/worker requests=%d/%d", managers.Load(), workers.Load())
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	for _, event := range events {
		if event.Kind == "tool_call" && toolCallName(event.Text) == ToolQueryHistory {
			reads++
			if isManagerAgent(event.AgentID) {
				t.Fatal("fixture failed to exercise worker-only history mode")
			}
		}
	}
	if reads != 1 {
		t.Fatalf("actual delegated query calls=%d", reads)
	}
}
