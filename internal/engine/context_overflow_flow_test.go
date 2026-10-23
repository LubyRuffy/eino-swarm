package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

const contextFlowOldAnswer = "earlier-checked-result"

// These requests go through the real API adapter and manager graph. A helper
// pause test cannot prove that the second rejected request ends the same turn.
func TestScheduledAPIContextOverflowCompactsOnceThenPersistsTheFailure(t *testing.T) {
	for _, recurring := range []bool{true, false} {
		name := "one-shot"
		if recurring {
			name = "recurring"
		}
		t.Run(name, func(t *testing.T) {
			e, endpoint := newContextFlowEngine(t, false)
			clk := newScheduleClock()
			e.now = clk.Now
			th := seedContextFlowHistory(t, e)
			input := ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID,
				Prompt: "check the fixture state", CreatedBy: store.ScheduleCreatedHuman}
			if recurring {
				input.EveryS = 60
			} else {
				input.DelayS = 60
			}
			sch, err := e.CreateSchedule(input)
			if err != nil {
				t.Fatal(err)
			}
			if err := e.Store().UpdateSchedule(sch.ID, map[string]any{"next_run_at": clk.Now()}); err != nil {
				t.Fatal(err)
			}
			other, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID,
				Prompt: "check a separate fixture", EveryS: 1200, CreatedBy: store.ScheduleCreatedHuman})
			if err != nil {
				t.Fatal(err)
			}
			e.fireDueSchedules()
			turn := onlyContextFlowTurn(t, e, th.ID)
			turn = waitForTurn(t, e, turn.ID)
			wantError, wantSchedule := publicContextOverflow, store.ScheduleDone
			if recurring {
				wantError, wantSchedule = publicContextOverflowPaused, store.SchedulePaused
			}
			if turn.Status != store.TurnError || turn.Error != wantError || !turn.ScheduleContinue || turn.ScheduleRunID == "" {
				t.Fatalf("scheduled API failure did not persist the actionable same-turn error: %+v", turn)
			}
			run := waitForScheduleRun(t, e, turn.ScheduleRunID)
			if run.Status != store.ScheduleRunError || run.Summary != wantError || run.ScheduleID != sch.ID || run.TurnID != turn.ID {
				t.Fatalf("scheduled run lost the failed turn: %+v", run)
			}
			got, err := e.GetSchedule(sch.ID)
			if err != nil || got.Status != wantSchedule {
				t.Fatalf("schedule=%+v err=%v, want %s", got, err, wantSchedule)
			}
			got, err = e.GetSchedule(other.ID)
			if err != nil || got.Status != store.ScheduleActive {
				t.Fatalf("an unrelated recurring schedule was paused: %+v, %v", got, err)
			}
			assertContextFlowCompaction(t, e, endpoint, th.ID, turn.ID, 2)
			events, err := e.Store().ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			terminal := 0
			for _, event := range events {
				if event.Kind == swarm.NotifyError.String() {
					terminal++
					if event.Err != turn.Error || event.AgentID != swarm.DefaultManagerID || event.Seq <= 0 {
						t.Fatalf("Trace terminal error disagrees with the stored turn: %+v", event)
					}
				}
			}
			if terminal != 1 {
				t.Fatalf("Trace terminal errors=%d, want one", terminal)
			}
			calls, err := e.Store().ListLLMCalls(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			managerCalls := 0
			for _, call := range calls {
				if call.AgentID != swarm.DefaultManagerID {
					continue
				}
				managerCalls++
				if call.ThreadID != th.ID || call.TurnID != turn.ID || !strings.Contains(call.Err, "status code: 400") || !strings.Contains(call.Err, "maximum context length") {
					t.Fatalf("Trace lost the actual rejected HTTP call: %+v", call)
				}
			}
			if managerCalls != 2 {
				t.Fatalf("Trace manager calls=%d, want exactly two rejected requests", managerCalls)
			}
			waitSettled(t, e, th.ID)
			clk.Advance(time.Minute)
			e.fireDueSchedules()
			if later := onlyContextFlowTurn(t, e, th.ID); later.ID != turn.ID {
				t.Fatalf("a terminal context failure spawned another turn: %+v", later)
			}
			if len(endpoint.managerRequests()) != 2 {
				t.Fatal("the failed schedule retried after its terminal failure")
			}
			t.Logf("same-turn HTTP attempts=2 compact=1 error=1 schedule=%s unrelated=active", wantSchedule)
		})
	}
}

// A successful recovery still enters later tool rounds. Those calls must not
// keep folding merely because the first request set the forced-recovery flag.
func TestScheduledAPIContextRecoveryConsumesForceBeforeLaterToolRounds(t *testing.T) {
	e, endpoint := newContextFlowEngine(t, true)
	th := seedContextFlowHistory(t, e)
	sch, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: "check the fixture state", EveryS: 60, CreatedBy: store.ScheduleCreatedHuman})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.RunScheduleNow(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn = waitForTurn(t, e, turn.ID)
	if turn.Status != store.TurnDone || turn.Error != "" {
		t.Fatalf("the compressed request did not recover: %+v", turn)
	}
	assertContextFlowCompaction(t, e, endpoint, th.ID, turn.ID, 5)
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	reports := 0
	for _, event := range events {
		if event.Kind == KindScheduleReport {
			reports++
		}
		if event.Kind == swarm.NotifyError.String() {
			t.Fatalf("successful recovery left a terminal error: %+v", event)
		}
	}
	if reports != 3 {
		t.Fatalf("real scheduled tool reports=%d, want three post-recovery rounds", reports)
	}
	run := waitForScheduleRun(t, e, turn.ScheduleRunID)
	if run.Status != store.ScheduleRunFindings || !strings.Contains(run.Summary, "fixture checked") {
		t.Fatalf("the recovered schedule has no real tool evidence: %+v", run)
	}
	got, err := e.GetSchedule(sch.ID)
	if err != nil || got.Status != store.ScheduleActive {
		t.Fatalf("a recovered schedule was incorrectly paused: %+v, %v", got, err)
	}
	requests := endpoint.managerRequests()
	if !strings.Contains(requests[4], "flow-report-3") {
		t.Fatal("the final API request lost the latest tool call/result pair")
	}
	t.Log("same-turn HTTP attempts=5 rejection=1 compact=1 scheduled reports=3 schedule=active")
}

type contextFlowEndpoint struct {
	mu       sync.Mutex
	requests []string
	recover  bool
}

func (f *contextFlowEndpoint) managerRequests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func newContextFlowEngine(t *testing.T, recover bool) (*Engine, *contextFlowEndpoint) {
	t.Helper()
	f := &contextFlowEndpoint{recover: recover}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected fixture request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var request struct {
			Tools    []json.RawMessage `json:"tools"`
			Stream   bool              `json:"stream"`
			Messages []struct{ Role, Content string }
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Error(err)
			return
		}
		if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		if len(request.Tools) == 0 {
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Fixture state and earlier checked results are retained."}, "stop")
			return
		}
		f.mu.Lock()
		f.requests = append(f.requests, string(body))
		n := len(f.requests)
		f.mu.Unlock()
		if n == 1 || !f.recover {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"maximum context length is 128000 tokens; requested 64000 output tokens","type":"invalid_request_error","code":"context_length_exceeded"}}`)
			return
		}
		if n <= 4 {
			args, _ := json.Marshal(map[string]any{"findings": fmt.Sprintf("fixture checked %d", n-1), "keep": true})
			writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": fmt.Sprintf("flow-report-%d", n-1), "type": "function",
				"function": map[string]any{"name": ToolReportSchedule, "arguments": string(args)},
			}}}, "tool_calls")
			return
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "The fixture check is complete."}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Default = config.DefaultProviderID
	cfg.Models.Providers = []config.Provider{{ID: config.DefaultProviderID,
		BaseURL: endpoint.URL, Model: "fixture-model", ContextWindow: 128000}}
	cfg.Swarm.AutoTitle = false
	cfg.Swarm.CompactKeepMessages = 1
	cfg.Swarm.MaxCompletionTokens = 64000
	cfg.Swarm.AgentTimeoutSeconds = 30
	cfg.Memory.Enabled, cfg.Memory.AutoReview = false, false
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := New(cfg, st, provider.New(cfg), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(e.Shutdown)
	return e, f
}

func writeContextFlowReply(w http.ResponseWriter, stream bool, message map[string]any, finish string) {
	if !stream {
		message["role"] = "assistant"
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "fixture", "object": "chat.completion",
			"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	message["role"] = "assistant"
	chunk, _ := json.Marshal(map[string]any{"id": "fixture", "object": "chat.completion.chunk",
		"choices": []any{map[string]any{"index": 0, "delta": message, "finish_reason": finish}}})
	_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
}

func seedContextFlowHistory(t *testing.T, e *Engine) *store.Thread {
	t.Helper()
	th, err := e.CreateThread("context recovery fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendMessages(th.ID, "completed-fixture-turn", []store.Message{
		{Role: string(schema.User), Content: "inspect the earlier fixture"},
		{Role: string(schema.Assistant), Content: contextFlowOldAnswer + strings.Repeat(" fixture-result", 150)},
		{Role: string(schema.User), Content: "retain the check result"},
		{Role: string(schema.Assistant), Content: "The fixture state was checked."},
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{"session_memory": "Earlier fixture results were checked and should be retained."}); err != nil {
		t.Fatal(err)
	}
	return th
}

func onlyContextFlowTurn(t *testing.T, e *Engine, threadID string) *store.Turn {
	t.Helper()
	turns, err := e.Store().ListTurns(threadID)
	if err != nil || len(turns) != 1 {
		t.Fatalf("scheduled recovery must retain one turn, got %+v, %v", turns, err)
	}
	return &turns[0]
}

func assertContextFlowCompaction(t *testing.T, e *Engine, endpoint *contextFlowEndpoint, threadID, turnID string, wantRequests int) {
	t.Helper()
	requests := endpoint.managerRequests()
	if len(requests) != wantRequests {
		t.Fatalf("real manager HTTP requests=%d, want %d", len(requests), wantRequests)
	}
	if !strings.Contains(requests[0], contextFlowOldAnswer) || strings.Contains(requests[1], contextFlowOldAnswer) {
		t.Fatal("the rejected request was retried without folding the replayed answer")
	}
	if !strings.Contains(requests[1], "Earlier fixture results were checked and should be retained.") || len(requests[1]) >= len(requests[0]) {
		t.Fatalf("the retried request lost its briefing or did not shrink (bytes %d -> %d)", len(requests[0]), len(requests[1]))
	}
	for _, body := range requests {
		var request struct {
			MaxTokens int `json:"max_tokens"`
		}
		if err := json.Unmarshal([]byte(body), &request); err != nil || request.MaxTokens != 64000 {
			t.Fatalf("the API request lost the configured output cap: %+v, %v", request, err)
		}
	}
	events, err := e.Store().ListTurnEvents(turnID)
	if err != nil {
		t.Fatal(err)
	}
	compacts, retries := 0, 0
	for _, event := range events {
		if event.ThreadID != threadID || event.TurnID != turnID || event.Seq <= 0 {
			t.Fatalf("Trace event escaped the recovered turn: %+v", event)
		}
		switch event.Kind {
		case KindCompacted:
			if event.Err != "" {
				t.Fatalf("forced compaction failed: %+v", event)
			}
			compacts++
			var payload compactPayload
			if err := json.Unmarshal([]byte(event.Text), &payload); err != nil || !payload.Auto || payload.ThroughSeq <= 0 || payload.TokensBefore <= payload.TokensAfter {
				t.Fatalf("compaction has no persisted reduction/watermark: %+v, %v", payload, err)
			}
			if payload.TokensBefore >= e.compactThreshold(threadID) {
				t.Fatal("fixture crossed the normal token trigger, so it did not prove forced recovery")
			}
		case KindModelRetry:
			retries++
			var payload struct {
				Attempt int    `json:"attempt"`
				Cap     int    `json:"cap"`
				Reason  string `json:"reason"`
			}
			if err := json.Unmarshal([]byte(event.Text), &payload); err != nil || payload.Attempt != 1 || payload.Cap != 1 || payload.Reason != modelRetryReasonContextOverflow {
				t.Fatalf("context recovery became a generic retry: %+v, %v", payload, err)
			}
		}
	}
	if compacts != 1 || retries != 1 {
		t.Fatalf("Trace compact=%d retry=%d, want one forced recovery only", compacts, retries)
	}
	rows, err := e.Store().ListMessages(threadID)
	if err != nil {
		t.Fatal(err)
	}
	kept := false
	for _, row := range rows {
		kept = kept || strings.Contains(row.Content, contextFlowOldAnswer)
	}
	if !kept {
		t.Fatal("compaction erased the human transcript instead of rewriting the model state")
	}
}
