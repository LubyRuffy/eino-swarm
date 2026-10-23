package server_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/app"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/server"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A durable human pause must survive the real manager/tool middleware's restart
// cancellation. Hand-planting an ask event misses the synthetic unknown result
// that used to let the replacement call the manager before the human answered.
func TestHTTPRestartKeepsTheOriginalAskUntilOneHumanAnswer(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "fresh_handoff"
		if legacy {
			name = "retained_legacy_unknown"
		}
		t.Run(name, func(t *testing.T) { testHTTPRestartAsk(t, legacy) })
	}
}

func testHTTPRestartAsk(t *testing.T, legacy bool) {
	const callID = "restart-human-choice"
	const arguments = `{"questions":[{"id":"decision","prompt":"Which approach should be used?","options":[{"id":"inspect","label":"Inspect first"},{"id":"verify","label":"Verify first"}]}]}`
	type message struct {
		Role, Content string
		ToolCallID    string `json:"tool_call_id"`
		ToolCalls     []struct {
			ID       string
			Function struct{ Name, Arguments string }
		} `json:"tool_calls"`
	}
	var managerCalls atomic.Int32
	var mu sync.Mutex
	var answeredInput []message
	managerEntered := make(chan struct{}, 4)
	continuationEntered := make(chan struct{}, 4)
	finishContinuation := make(chan struct{})
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("offline model fixture received credentials")
		}
		var req struct {
			Stream   bool
			Messages []message
			Tools    []struct{ Function struct{ Name string } }
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			http.Error(w, "invalid offline request", http.StatusBadRequest)
			return
		}
		if len(req.Messages) > 0 && strings.HasPrefix(req.Messages[0].Content, "## Schedule answer claims") {
			slashReply(w, req.Stream, map[string]any{"content": `{"claims":[]}`}, "stop")
			return
		}
		manager := false
		for _, tool := range req.Tools {
			manager = manager || tool.Function.Name == engine.ToolAskUser
		}
		if !manager {
			slashReply(w, req.Stream, map[string]any{"content": "No additional work is needed."}, "stop")
			return
		}
		step := managerCalls.Add(1)
		select {
		case managerEntered <- struct{}{}:
		default:
		}
		if step == 1 {
			slashReply(w, req.Stream, map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": callID, "type": "function",
				"function": map[string]any{"name": engine.ToolAskUser, "arguments": arguments},
			}}}, "tool_calls")
			return
		}
		mu.Lock()
		answeredInput = append([]message(nil), req.Messages...)
		mu.Unlock()
		select {
		case continuationEntered <- struct{}{}:
		default:
		}
		// Hold the real continuation request while a duplicate HTTP answer is
		// submitted. Waiting until done alone would only exercise ErrIdle.
		select {
		case <-finishContinuation:
		case <-r.Context().Done():
			return
		}
		slashReply(w, req.Stream, map[string]any{"content": "The selected approach is recorded."}, "stop")
	}))
	t.Cleanup(fixture.Close)
	for _, key := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL"} {
		t.Setenv(key, "")
	}
	dir := t.TempDir()
	first, closeFirst := newRestartAskHTTPHarness(t, dir, fixture.URL)
	thread := first.json(http.MethodPost, "/api/threads", map[string]any{"title": "Restarted human decision"}, http.StatusCreated)["thread"].(map[string]any)["id"].(string)
	started := first.json(http.MethodPost, "/api/threads/"+thread+"/turns", map[string]any{"text": "Inspect the assigned input."}, http.StatusAccepted)
	turnID := started["turn"].(map[string]any)["id"].(string)
	waitHTTPRestartAsk(t, first, thread, turnID)
	var originalAsk store.Event
	initialEvents, err := first.app.Store.ListTurnEvents(turnID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range initialEvents {
		if event.Kind == "tool_call" && event.ToolCallID == callID {
			originalAsk = event
		}
	}
	if originalAsk.Seq == 0 || originalAsk.AgentID != "manager" {
		t.Fatalf("the real manager did not persist its original ask: %+v", initialEvents)
	}
	select {
	case <-managerEntered:
	default:
		t.Fatal("the initial question did not come from the real model adapter")
	}
	handoff := first.json(http.MethodPost, "/api/engine/restart", nil, http.StatusOK)
	if handoff["ready"] != true || handoff["threads_checkpointed"] != float64(1) {
		t.Fatalf("the pending question was not durably checkpointed: %+v", handoff)
	}
	handoffEvents, err := first.app.Store.ListTurnEvents(turnID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range handoffEvents {
		if event.ToolCallID == callID {
			t.Logf("original ask at handoff: seq=%d kind=%s text=%q", event.Seq, event.Kind, event.Text)
			if !legacy && event.Kind == "tool_result" && event.AgentID == originalAsk.AgentID {
				t.Fatalf("current handoff settled an unanswered ask with a synthetic result: %+v", event)
			}
		}
	}
	closeFirst()

	second, _ := newRestartAskHTTPHarness(t, dir, fixture.URL)
	if legacy {
		// Simulate an older release's synthetic receipt in the reopened store,
		// not a fabricated question or a bypass of the real HTTP handoff above.
		unknown := store.Event{ThreadID: thread, TurnID: turnID, AgentID: originalAsk.AgentID,
			Role: originalAsk.Role, Kind: "tool_result", ToolCallID: callID, Text: swarm.HandoffUnknownToolResult}
		if err := second.app.Store.AppendEvent(&unknown); err != nil {
			t.Fatal(err)
		}
		handoffEvents = append(handoffEvents, unknown)
	}
	// Subscribe before restoration: a history-only repair would leave an
	// already-connected PC waiting, even if a later refresh masks the problem.
	stream, readySeq := liveHTTPRestartAskStream(t, second, thread)
	if n, err := second.app.Engine.ResumeOrphanedTurns(); err != nil || n != 1 {
		t.Fatalf("restore original turn: count=%d err=%v", n, err)
	}
	waitHTTPRestartAsk(t, second, thread, turnID)
	var recovered store.Event
	select {
	case frame := <-stream:
		if frame.name != "ask_recovered" || json.Unmarshal(frame.data, &recovered) != nil {
			t.Fatalf("the live PC did not receive the named ask recovery: %s %s", frame.name, frame.data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the existing SSE listener did not receive ask_recovered")
	}
	if recovered.Kind != "ask_recovered" || recovered.ThreadID != thread || recovered.TurnID != turnID ||
		recovered.ToolCallID != callID || recovered.AgentID != originalAsk.AgentID || recovered.Role != originalAsk.Role || recovered.Seq <= readySeq {
		t.Fatalf("ask recovery changed identity or arrived only as old history: %+v ready_seq=%d", recovered, readySeq)
	}
	var originalQuestions, recoveredQuestions map[string]any
	if json.Unmarshal([]byte(arguments), &originalQuestions) != nil || json.Unmarshal([]byte(recovered.Text), &recoveredQuestions) != nil ||
		!reflect.DeepEqual(originalQuestions, recoveredQuestions) {
		t.Fatalf("recovery must expose only the original questions, not private checkpoint/model state: %q", recovered.Text)
	}
	select {
	case <-managerEntered:
		t.Fatal("replacement called the manager before the original question was answered")
	case <-time.After(100 * time.Millisecond):
	}
	if managerCalls.Load() != 1 {
		t.Fatalf("manager calls before the human answer=%d, want only the initial ask", managerCalls.Load())
	}

	answer := map[string]any{"call_id": callID, "answers": engine.AskAnswers{"decision": {Answers: []string{"Inspect first"}}}}
	second.json(http.MethodPost, "/api/threads/"+thread+"/answers", answer, http.StatusAccepted)
	select {
	case <-continuationEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("the accepted human answer did not reach the real model continuation")
	}
	duplicate := second.json(http.MethodPost, "/api/threads/"+thread+"/answers", answer, http.StatusConflict)
	if duplicate["code"] != "ask_mismatch" {
		t.Fatalf("the active original turn did not reject the consumed call: %+v", duplicate)
	}
	close(finishContinuation)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		turn, err := second.app.Store.GetTurn(turnID)
		if err != nil {
			t.Fatal(err)
		}
		if turn.Status != store.TurnRunning {
			if turn.Status != store.TurnDone {
				t.Fatalf("the answered original turn did not finish: %+v", turn)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	finished, err := second.app.Store.GetTurn(turnID)
	if err != nil || finished.Status != store.TurnDone {
		t.Fatalf("the original turn did not finish after the answer: %+v err=%v", finished, err)
	}
	second.json(http.MethodPost, "/api/threads/"+thread+"/answers", answer, http.StatusConflict)
	if managerCalls.Load() != 2 {
		t.Fatalf("answer/duplicate answer caused %d manager calls, want initial ask plus one continuation", managerCalls.Load())
	}
	mu.Lock()
	input := append([]message(nil), answeredInput...)
	mu.Unlock()
	calls, results := 0, 0
	for _, m := range input {
		for _, call := range m.ToolCalls {
			if call.ID == callID && call.Function.Name == engine.ToolAskUser {
				calls++
			}
		}
		if m.Role == "tool" && m.ToolCallID == callID {
			results++
			var result struct{ Answers engine.AskAnswers }
			if json.Unmarshal([]byte(m.Content), &result) != nil || len(result.Answers) != 1 ||
				!reflect.DeepEqual(result.Answers["decision"].Answers, []string{"Inspect first"}) {
				t.Fatalf("the real model received a synthetic or wrong answer: %s", m.Content)
			}
		}
	}
	if calls != 1 || results != 1 {
		t.Fatalf("original model tool-call/result pair=%d/%d, want exactly one of each: %+v", calls, results, input)
	}
	events, err := second.app.Store.ListTurnEvents(turnID)
	if err != nil {
		t.Fatal(err)
	}
	for _, before := range handoffEvents {
		found := false
		for _, after := range events {
			if before.Seq == after.Seq {
				found = before.Kind == after.Kind && before.Text == after.Text && before.ToolCallID == after.ToolCallID
				break
			}
		}
		if !found {
			t.Fatalf("recovery rewrote the original handoff evidence: %+v", before)
		}
	}
	storedCalls, storedAnswers, storedRecoveries := 0, 0, 0
	for _, event := range events {
		if event.ToolCallID != callID {
			continue
		}
		if event.Kind == "tool_call" {
			storedCalls++
		}
		if event.Kind == "tool_result" && strings.Contains(event.Text, `"Inspect first"`) {
			storedAnswers++
		}
		if event.Kind == "ask_recovered" {
			storedRecoveries++
		}
	}
	if storedCalls != 1 || storedAnswers != 1 || storedRecoveries != 1 {
		t.Fatalf("durable original ask/answer/recovery=%d/%d/%d, want one of each", storedCalls, storedAnswers, storedRecoveries)
	}
	trace := second.json(http.MethodGet, "/api/trace/"+turnID, nil, http.StatusOK)
	traceRecovery := 0
	for _, raw := range trace["events"].([]any) {
		event := raw.(map[string]any)
		if event["kind"] == "ask_recovered" && event["tool_call_id"] == callID {
			traceRecovery++
			if event["text"] != recovered.Text {
				t.Fatal("Trace changed the public recovered-question payload")
			}
		}
	}
	if traceRecovery != 1 {
		t.Fatalf("the original turn Trace lost or duplicated ask recovery: %d", traceRecovery)
	}
	turns, err := second.app.Store.ListTurns(thread)
	if err != nil || len(turns) != 1 || turns[0].ID != turnID {
		t.Fatalf("answer created or replaced a turn: %+v err=%v", turns, err)
	}
}

type restartAskSSEFrame struct {
	name string
	data json.RawMessage
}

// The request context also bounds a quiet socket; a deadline checked only
// after Scanner.Scan cannot time out when the missing event never arrives.
func liveHTTPRestartAskStream(t *testing.T, h *harness, thread string) (<-chan restartAskSSEFrame, int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.srv.URL+"/api/threads/"+thread+"/events", nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE status=%d", resp.StatusCode)
	}
	frames := make(chan restartAskSSEFrame, 64)
	go func() {
		defer close(frames)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 4096), 4*1024*1024)
		var frame restartAskSSEFrame
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				frame.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				frame.data = json.RawMessage(strings.TrimPrefix(line, "data: "))
			case line == "" && frame.name != "":
				if frame.name == "ready" || frame.name == "ask_recovered" {
					select {
					case frames <- frame:
					case <-ctx.Done():
						return
					}
				}
				frame = restartAskSSEFrame{}
			}
		}
	}()
	select {
	case frame := <-frames:
		var ready struct{ Seq int64 }
		if frame.name != "ready" || json.Unmarshal(frame.data, &ready) != nil {
			t.Fatalf("SSE did not establish its pre-restoration cursor: %s %s", frame.name, frame.data)
		}
		return frames, ready.Seq
	case <-ctx.Done():
		t.Fatal("SSE did not become ready before restoration")
	}
	return nil, 0
}

func newRestartAskHTTPHarness(t *testing.T, dir, endpoint string) (*harness, func()) {
	t.Helper()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Providers[0].BaseURL = endpoint
	cfg.Models.Providers[0].APIKey = ""
	cfg.Models.Providers[0].Model = "offline-restart-question"
	cfg.Swarm.AutoTitle = false
	cfg.Memory.AutoReview = false
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	e := engine.New(cfg, st, provider.New(cfg), logger)
	s, err := server.New(server.Options{Engine: e, Logger: logger, Mode: server.ModeWeb})
	if err != nil {
		e.Shutdown()
		_ = st.Close()
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	var once sync.Once
	closeHarness := func() { once.Do(func() { ts.Close(); e.Shutdown(); _ = st.Close() }) }
	t.Cleanup(closeHarness)
	return &harness{t: t, app: &app.App{Config: cfg, Store: st, Engine: e}, srv: ts}, closeHarness
}

func waitHTTPRestartAsk(t *testing.T, h *harness, thread, turn string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got := h.json(http.MethodGet, "/api/threads/"+thread, nil, http.StatusOK)
		status, _ := got["status"].(map[string]any)
		if status["awaiting_answer"] == true && status["turn_id"] == turn && status["running"] == true {
			return
		}
		if status["running"] != true {
			t.Fatalf("original turn stopped instead of retaining its unanswered ask: %+v", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("original turn did not retain its unanswered ask after restart")
}
