package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

const timingFixtureAnswer = "The current state has been inspected."

// Classification uses the same configured provider as the manager. A valid
// non-streaming response after thirty seconds must not rerun the manager.
func TestScheduleAnswerAllowsTheConfiguredProviderWaitBeyondThirtySeconds(t *testing.T) {
	entered := make(chan struct{})
	e, managers, checks := newScheduleAnswerTimingFixture(t, 40, 1, func(w http.ResponseWriter, r *http.Request, n int32) {
		if n == 1 {
			close(entered)
			timer := time.NewTimer(31 * time.Second)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-r.Context().Done():
				return
			}
		}
		writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
	})
	th, err := e.CreateThread("configured classification wait", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "Inspect the current state.")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("classification did not reach the actual HTTP adapter")
	}
	deadline := time.Now().Add(45 * time.Second)
	var got *store.Turn
	for time.Now().Before(deadline) {
		got, err = e.store.GetTurn(turn.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != store.TurnRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got.Status != store.TurnDone || got.Final != timingFixtureAnswer || managers.Load() != 1 || checks.Load() != 1 {
		t.Fatalf("valid classification caused a manager replay: turn=%+v managers=%d checks=%d", got, managers.Load(), checks.Load())
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == KindModelRetry {
			t.Fatalf("configured wait consumed a correction allowance: %+v", ev)
		}
	}
	calls, err := e.store.ListLLMCalls(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	var policyCalls int
	for _, call := range calls {
		if call.AgentID == scheduleAnswerAgentID {
			policyCalls++
			if call.TurnID != turn.ID || call.Err != "" || call.DurationMS < 30_000 {
				t.Fatalf("slow successful classification lost its original Trace: %+v", call)
			}
		}
	}
	if policyCalls != 1 {
		t.Fatalf("policy Trace calls=%d", policyCalls)
	}
}

func TestStopDuringProviderBudgetWithholdsThePendingAnswerEverywhere(t *testing.T) {
	entered, cancelled := make(chan struct{}), make(chan struct{})
	e, managers, checks := newScheduleAnswerTimingFixture(t, 40, 1, func(_ http.ResponseWriter, r *http.Request, _ int32) {
		close(entered)
		<-r.Context().Done()
		close(cancelled)
	})
	th, err := e.CreateThread("cancel classification wait", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	turn, err := e.StartTurn(th.ID, "Inspect the current state.")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("classification did not enter")
	}
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	if got.Status != store.TurnCancelled || got.Final != "" || managers.Load() != 1 || checks.Load() != 1 {
		t.Fatalf("Stop did not preserve the original turn: turn=%+v managers=%d checks=%d", got, managers.Load(), checks.Load())
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop left the classifier HTTP request alive")
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if strings.Contains(ev.Text, timingFixtureAnswer) {
			t.Fatalf("pending answer reached storage: %+v", ev)
		}
	}
	for {
		select {
		case ev := <-sub.C:
			if strings.Contains(ev.Text, timingFixtureAnswer) {
				t.Fatalf("pending answer reached the live bus: %+v", ev)
			}
		default:
			if strings.Contains(replayDump(t, e, th.ID), timingFixtureAnswer) {
				t.Fatal("pending answer reached replay")
			}
			return
		}
	}
}

func TestScheduleAnswerRetainsConfiguredFirstByteTimeoutAndItsTrace(t *testing.T) {
	e, _, checks := newScheduleAnswerTimingFixture(t, 1, 1, func(_ http.ResponseWriter, r *http.Request, _ int32) {
		<-r.Context().Done()
	})
	th, err := e.CreateThread("bounded classification wait", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, failure := e.verifyScheduleAnswer(ctx, turn, []string{timingFixtureAnswer})
	var timeout provider.FirstByteTimeoutError
	if failure == nil || failure.Code != "classification_unavailable" || !errors.As(failure, &timeout) || timeout.Wait != time.Second {
		t.Fatalf("configured provider timeout was lost: failure=%+v timeout=%+v", failure, timeout)
	}
	if checks.Load() != 2 {
		t.Fatalf("provider reconnect count=%d, want its bounded two attempts", checks.Load())
	}
	calls, err := e.store.ListLLMCalls(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].TurnID != turn.ID || calls[0].AgentID != scheduleAnswerAgentID || !strings.Contains(calls[0].Err, "no first byte within 1s") {
		t.Fatalf("configured failure lost its original Trace: %+v", calls)
	}
}

func TestScheduleAnswerRetainsConfiguredBodyIdleTimeoutAndItsTrace(t *testing.T) {
	e, _, checks := newScheduleAnswerTimingFixture(t, 40, 1, func(w http.ResponseWriter, r *http.Request, _ int32) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})
	th, err := e.CreateThread("bounded classification body", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, failure := e.verifyScheduleAnswer(ctx, turn, []string{timingFixtureAnswer})
	var timeout provider.IdleTimeoutError
	if failure == nil || failure.Code != "classification_unavailable" || !errors.As(failure, &timeout) || timeout.Idle != time.Second {
		t.Fatalf("configured provider body timeout was lost: failure=%+v timeout=%+v", failure, timeout)
	}
	if checks.Load() != 1 {
		t.Fatalf("body silence retried the completed request: attempts=%d", checks.Load())
	}
	calls, err := e.store.ListLLMCalls(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].TurnID != turn.ID || calls[0].AgentID != scheduleAnswerAgentID || !strings.Contains(calls[0].Err, "stream idle timeout after 1s") {
		t.Fatalf("configured body failure lost its original Trace: %+v", calls)
	}
}

func TestScheduleAnswerTimingFixtureRefusesUnknownAndMalformedPolicyInput(t *testing.T) {
	e, managers, checks := newScheduleAnswerTimingFixture(t, 1, 1, func(w http.ResponseWriter, _ *http.Request, _ int32) {
		writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
	})
	valid := func() map[string]any {
		body, err := json.Marshal(map[string]any{"messages": []string{timingFixtureAnswer}, "now": time.Now().Format(time.RFC3339)})
		if err != nil {
			t.Fatal(err)
		}
		return map[string]any{"messages": []*schema.Message{schema.SystemMessage(scheduleAnswerPrompt), schema.UserMessage(string(body))}, "stream": false, "tool_choice": "none"}
	}
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"unknown-candidate", func(req map[string]any) {
			req["messages"].([]*schema.Message)[1].Content = `{"messages":["An unknown candidate."],"now":"2026-10-25T01:00:00Z"}`
		}},
		{"malformed-candidate", func(req map[string]any) { req["messages"].([]*schema.Message)[1].Content = `{` }},
		{"missing-time", func(req map[string]any) {
			req["messages"].([]*schema.Message)[1].Content = `{"messages":["` + timingFixtureAnswer + `"]}`
		}},
		{"wrong-system-role", func(req map[string]any) { req["messages"].([]*schema.Message)[0].Role = schema.User }},
		{"wrong-data-role", func(req map[string]any) { req["messages"].([]*schema.Message)[1].Role = schema.Assistant }},
		{"streaming", func(req map[string]any) { req["stream"] = true }},
		{"tools", func(req map[string]any) { req["tools"] = []any{map[string]string{"type": "function"}} }},
		{"tool-choice", func(req map[string]any) { req["tool_choice"] = "auto" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := valid()
			tc.mutate(req)
			body, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.Post(e.cfg.Models.Providers[0].BaseURL, "application/json", strings.NewReader(string(body)))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("unknown or malformed policy request was admitted: status=%d", resp.StatusCode)
			}
		})
	}
	if managers.Load() != 0 || checks.Load() != 0 {
		t.Fatalf("refused input reached a model fixture: manager=%d policy=%d", managers.Load(), checks.Load())
	}
}

func newScheduleAnswerTimingFixture(t *testing.T, firstByteSeconds, idleSeconds int, policy func(http.ResponseWriter, *http.Request, int32)) (*Engine, *atomic.Int32, *atomic.Int32) {
	t.Helper()
	managers, checks := &atomic.Int32{}, &atomic.Int32{}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages   []*schema.Message `json:"messages"`
			Stream     bool              `json:"stream"`
			Tools      []any             `json:"tools"`
			ToolChoice string            `json:"tool_choice"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "malformed model request", http.StatusBadRequest)
			return
		}
		if len(req.Messages) == 0 || req.Messages[0] == nil {
			http.Error(w, "missing model messages", http.StatusBadRequest)
			return
		}
		if req.Messages[0].Content == scheduleAnswerPrompt {
			if req.Stream || len(req.Tools) != 0 || len(req.Messages) != 2 || req.ToolChoice != "none" || req.Messages[0].Role != schema.System || req.Messages[1] == nil || req.Messages[1].Role != schema.User {
				http.Error(w, "invalid classifier protocol", http.StatusBadRequest)
				return
			}
			var candidate struct {
				Messages []string `json:"messages"`
				Now      string   `json:"now"`
			}
			dec := json.NewDecoder(strings.NewReader(req.Messages[1].Content))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&candidate); err != nil || dec.Decode(new(any)) != io.EOF || len(candidate.Messages) != 1 || candidate.Messages[0] != timingFixtureAnswer {
				http.Error(w, "unknown classifier candidate", http.StatusBadRequest)
				return
			}
			if _, err := time.Parse(time.RFC3339, candidate.Now); err != nil {
				http.Error(w, "invalid classifier time", http.StatusBadRequest)
				return
			}
			policy(w, r, checks.Add(1))
			return
		}
		if strings.Contains(req.Messages[0].Content, "## Waiting") {
			managers.Add(1)
		}
		writeContextFlowReply(w, req.Stream, map[string]any{"content": timingFixtureAnswer}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].APIKey = ""
	e.cfg.Models.Providers[0].Model = "offline-classification-timing"
	e.cfg.Models.Providers[0].FirstByteTimeoutSeconds = firstByteSeconds
	e.cfg.Models.Providers[0].TimeoutSeconds = idleSeconds
	e.pool = provider.New(e.cfg)
	return e, managers, checks
}
