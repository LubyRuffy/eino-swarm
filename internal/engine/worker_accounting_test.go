package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// A retained worker keeps its original model instance. Rebinding only the
// Registry's factory must not leave that worker's later requests on an old bill.
func TestRetainedWorkerBillsItsNextHTTPRequestToTheCurrentTurn(t *testing.T) {
	for _, parked := range []bool{false, true} {
		t.Run(fmt.Sprintf("parked=%t", parked), func(t *testing.T) {
			var requests atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if requests.Add(1) == 1 {
					writeAccountingStream(w, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "phase-call", "type": "function", "function": map[string]any{"name": "await_phase", "arguments": `{}`}}}}, "tool_calls")
					return
				}
				writeAccountingStream(w, map[string]any{"content": "The check completed."}, "stop")
			}))
			t.Cleanup(endpoint.Close)
			e, th := newAccountingEngine(t, endpoint.URL)
			first := plantUnfinishedTurn(t, e, th.ID, "Inspect the current evidence.")
			second := plantUnfinishedTurn(t, e, th.ID, "Check progress.")
			reg := accountingRegistry(t, e, first)
			rt := e.runtimeFor(th.ID)
			_, release := ownManagerTestFixture(context.Background(), rt, first.ID, reg)
			defer release()
			phase := &accountingPhaseTool{entered: make(chan struct{}), release: make(chan struct{})}
			h, err := reg.Spawn(context.Background(), "inspector", "Inspect the current evidence.", reg.ModelBuilder, phase)
			if err != nil {
				t.Fatal(err)
			}
			awaitAccounting(t, phase.entered)
			rt.parkRegistry(reg)
			release()
			wantTurn := first.ID
			if !parked {
				if rt.takeParkedRegistry() != reg {
					t.Fatal("next turn lost the retained Registry")
				}
				// Production refreshes this factory, while h keeps the model it
				// was given before the first request and the parked boundary.
				reg.ModelBuilder = accountingBuilder(t, e, second)
				_, releaseNext := ownManagerTestFixture(context.Background(), rt, second.ID, reg)
				defer releaseNext()
				attachHostNotify(reg, newAccumulator(e, th.ID, second.ID, 0))
				wantTurn = second.ID
			}
			close(phase.release)
			awaitAccounting(t, h.Done())
			firstCalls, _ := e.store.ListLLMCalls(first.ID)
			secondCalls, _ := e.store.ListLLMCalls(second.ID)
			wantFirst, wantSecond := 1, 1
			if parked {
				wantFirst, wantSecond = 2, 0
			}
			if len(firstCalls) != wantFirst || len(secondCalls) != wantSecond || requests.Load() != 2 {
				t.Fatalf("retained HTTP billing: first=%+v second=%+v requests=%d", firstCalls, secondCalls, requests.Load())
			}
			calls, err := e.store.ListLLMCalls(wantTurn)
			if err != nil || calls[len(calls)-1].AgentID != h.ID || calls[len(calls)-1].TotalTokens != 11 {
				t.Fatalf("Trace lost original worker identity or usage: calls=%+v err=%v", calls, err)
			}
			events, err := e.store.ListTurnEvents(wantTurn)
			if err != nil {
				t.Fatal(err)
			}
			finished := false
			for _, event := range events {
				finished = finished || event.Kind == "finished" && event.AgentID == h.ID
			}
			if !finished {
				t.Fatalf("Trace timeline and model calls disagree: events=%+v calls=%+v", events, calls)
			}
		})
	}
}

// Attribution is frozen before HTTP starts, rather than selected by the final
// recorder callback: a retained stream may end after another manager takes over.
func TestWorkerHTTPRequestKeepsItsStartingTurnAcrossASuccessor(t *testing.T) {
	for _, outcome := range []string{"success", "http-error", "cancel"} {
		t.Run(outcome, func(t *testing.T) {
			arrived, finish := make(chan struct{}), make(chan struct{})
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if outcome != "http-error" {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"id\":\"held\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"checking\"},\"finish_reason\":null}]}\n\n")
					w.(http.Flusher).Flush()
				}
				close(arrived)
				select {
				case <-r.Context().Done():
					return
				case <-finish:
				}
				if outcome == "http-error" {
					http.Error(w, `{"error":{"message":"request refused","type":"invalid_request_error"}}`, http.StatusUnauthorized)
					return
				}
				writeAccountingStream(w, map[string]any{"content": " completed"}, "stop")
			}))
			t.Cleanup(endpoint.Close)
			e, th := newAccountingEngine(t, endpoint.URL)
			first := plantUnfinishedTurn(t, e, th.ID, "Inspect the current evidence.")
			second := plantUnfinishedTurn(t, e, th.ID, "Check progress.")
			reg := accountingRegistry(t, e, first)
			rt := e.runtimeFor(th.ID)
			_, release := ownManagerTestFixture(context.Background(), rt, first.ID, reg)
			defer release()
			h, err := reg.Spawn(context.Background(), "inspector", "Inspect the current evidence.", reg.ModelBuilder)
			if err != nil {
				t.Fatal(err)
			}
			awaitAccounting(t, arrived)
			rt.parkRegistry(reg)
			release()
			if rt.takeParkedRegistry() != reg {
				t.Fatal("next turn lost the retained Registry")
			}
			reg.ModelBuilder = accountingBuilder(t, e, second)
			_, releaseNext := ownManagerTestFixture(context.Background(), rt, second.ID, reg)
			defer releaseNext()
			if outcome == "cancel" {
				h.Cancel()
			} else {
				close(finish)
			}
			awaitAccounting(t, h.Done())
			firstCalls, err := e.store.ListLLMCalls(first.ID)
			secondCalls, _ := e.store.ListLLMCalls(second.ID)
			if err != nil || len(firstCalls) != 1 || len(secondCalls) != 0 || firstCalls[0].AgentID != h.ID {
				t.Fatalf("in-flight request moved bills: first=%+v second=%+v err=%v", firstCalls, secondCalls, err)
			}
			if (firstCalls[0].Err != "") != (outcome != "success") {
				t.Fatalf("request outcome was changed: %+v", firstCalls[0])
			}
		})
	}
}

// Waiting is observed on its actual owner, while the request starts only after
// admission. Cancelling another queued call must not create a billed request.
func TestRetainedWorkerBackoffTracksItsOwnerBeforeTheNextHTTPRequest(t *testing.T) {
	var requests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, `{"error":{"message":"temporarily unavailable","type":"server_error"}}`, http.StatusServiceUnavailable)
			return
		}
		writeAccountingCompletion(w)
	}))
	t.Cleanup(endpoint.Close)
	e, th := newAccountingEngine(t, endpoint.URL)
	first := plantUnfinishedTurn(t, e, th.ID, "Inspect the current evidence.")
	second := plantUnfinishedTurn(t, e, th.ID, "Check progress.")
	reg := accountingRegistry(t, e, first)
	rt := e.runtimeFor(th.ID)
	_, release := ownManagerTestFixture(context.Background(), rt, first.ID, reg)
	defer release()
	worker := reg.ModelBuilder("inspector", "inspector-1")
	if _, err := worker.Generate(context.Background(), []*schema.Message{schema.UserMessage("Inspect the evidence.")}); err == nil {
		t.Fatal("fixture HTTP failure did not reach the caller")
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := worker.Generate(ctx, []*schema.Message{schema.UserMessage("Retry the check.")})
		result <- err
	}()
	firstWait := waitLive(t, sub, KindModelBackoff, 5*time.Second)
	if firstWait.TurnID != first.ID {
		t.Fatalf("initial wait belongs to another turn: %+v", firstWait)
	}
	rt.parkRegistry(reg)
	release()
	if rt.takeParkedRegistry() != reg {
		t.Fatal("next turn lost the retained Registry")
	}
	reg.ModelBuilder = accountingBuilder(t, e, second)
	_, releaseNext := ownManagerTestFixture(ctx, rt, second.ID, reg)
	defer releaseNext()
	queuedCtx, cancelQueued := context.WithCancel(ctx)
	defer cancelQueued()
	queued := make(chan error, 1)
	go func() {
		_, err := worker.Generate(queuedCtx, []*schema.Message{schema.UserMessage("Inspect the next source.")})
		queued <- err
	}()
	secondWait := waitLive(t, sub, KindModelBackoff, 5*time.Second)
	if secondWait.TurnID != second.ID {
		t.Fatalf("wait after successor admission kept the old owner: %+v", secondWait)
	}
	cancelQueued()
	select {
	case err := <-queued:
		if err == nil {
			t.Fatal("cancelled admission succeeded")
		}
	case <-ctx.Done():
		t.Fatal("cancelled admission was not joined")
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("retained request did not leave cooldown")
	}
	firstCalls, _ := e.store.ListLLMCalls(first.ID)
	secondCalls, _ := e.store.ListLLMCalls(second.ID)
	if len(firstCalls) != 1 || len(secondCalls) != 1 || requests.Load() != 2 || secondCalls[0].Err != "" {
		t.Fatalf("waits became calls or admitted request kept old owner: first=%+v second=%+v requests=%d", firstCalls, secondCalls, requests.Load())
	}
}

// These delayed helpers intentionally bill their original turn; sharing a
// conversation with a newly occupied turn does not make them worker work.
func TestWorkerAccountingLeavesManagerAndAuxiliaryCallsOnTheirOriginalTurn(t *testing.T) {
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Effort string `json:"reasoning_effort"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			return
		}
		if input.Effort != "" {
			http.Error(w, `{"error":{"message":"unknown parameter: reasoning_effort","type":"invalid_request_error"}}`, http.StatusBadRequest)
			return
		}
		writeAccountingCompletion(w)
	}))
	t.Cleanup(endpoint.Close)
	e, th := newAccountingEngine(t, endpoint.URL)
	first := plantUnfinishedTurn(t, e, th.ID, "Inspect the current evidence.")
	second := plantUnfinishedTurn(t, e, th.ID, "Check progress.")
	reg := accountingRegistry(t, e, first)
	rt := e.runtimeFor(th.ID)
	_, release := ownManagerTestFixture(context.Background(), rt, first.ID, reg)
	defer release()
	worker := reg.ModelBuilder("inspector", "inspector-1")
	manager := reg.ModelBuilder(swarm.DefaultManagerID, swarm.DefaultManagerID)
	staticBuilder, err := e.pool.AuxiliaryModelBuilder(context.Background(), "", "", config.ReasoningLow, e.callRecorder(th.ID, first.ID))
	if err != nil {
		t.Fatal(err)
	}
	session := staticBuilder(SessionMemoryAgentID, SessionMemoryAgentID)
	reviewer := staticBuilder(ReviewAgentID, ReviewAgentID)
	rt.parkRegistry(reg)
	release()
	if rt.takeParkedRegistry() != reg {
		t.Fatal("next turn lost the retained Registry")
	}
	reg.ModelBuilder = accountingBuilder(t, e, second)
	_, releaseNext := ownManagerTestFixture(context.Background(), rt, second.ID, reg)
	defer releaseNext()
	for _, cm := range []model.BaseChatModel{worker, manager, session, reviewer} {
		if _, err := cm.Generate(context.Background(), []*schema.Message{schema.UserMessage("Inspect the evidence.")}); err != nil {
			t.Fatal(err)
		}
	}
	firstCalls, _ := e.store.ListLLMCalls(first.ID)
	secondCalls, _ := e.store.ListLLMCalls(second.ID)
	refused := 0
	for _, call := range firstCalls {
		if call.Err != "" {
			refused++
		}
	}
	if len(firstCalls) != 5 || refused != 2 || len(secondCalls) != 1 || secondCalls[0].AgentID != "inspector-1" {
		t.Fatalf("auxiliary or manager accounting moved: first=%+v second=%+v", firstCalls, secondCalls)
	}
}

func newAccountingEngine(t *testing.T, url string) (*Engine, *store.Thread) {
	t.Helper()
	e := newTestEngine(t)
	e.cfg.Models.Default = "accounting-fixture"
	e.cfg.Models.Providers = []config.Provider{{ID: "accounting-fixture", BaseURL: url, Model: "fixture"}}
	e.pool = provider.New(e.cfg)
	th, err := e.CreateThread("worker accounting", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return e, th
}

func accountingBuilder(t *testing.T, e *Engine, turn *store.Turn) swarm.ModelBuilder {
	t.Helper()
	builder, err := e.pool.ModelBuilder(context.Background(), "", "", "", e.callRecorder(turn.ThreadID, turn.ID))
	if err != nil {
		t.Fatal(err)
	}
	return builder
}

func accountingRegistry(t *testing.T, e *Engine, turn *store.Turn) *swarm.Registry {
	t.Helper()
	reg := swarm.NewRegistry()
	reg.ModelBuilder = accountingBuilder(t, e, turn)
	attachHostNotify(reg, newAccumulator(e, turn.ThreadID, turn.ID, 0))
	t.Cleanup(reg.Close)
	return reg
}

func awaitAccounting(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatal("accounting fixture did not reach its next boundary")
	}
}

func writeAccountingStream(w http.ResponseWriter, delta map[string]any, finish string) {
	w.Header().Set("Content-Type", "text/event-stream")
	body, _ := json.Marshal(map[string]any{"id": "accounting", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}, "usage": map[string]int{"prompt_tokens": 9, "completion_tokens": 2, "total_tokens": 11}})
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", body)
	w.(http.Flusher).Flush()
}

func writeAccountingCompletion(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"id":"accounting","choices":[{"index":0,"message":{"role":"assistant","content":"The check completed."},"finish_reason":"stop"}],"usage":{"prompt_tokens":9,"completion_tokens":2,"total_tokens":11}}`)
}

type accountingPhaseTool struct{ entered, release chan struct{} }

func (*accountingPhaseTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "await_phase", Desc: "Wait for the current phase to be available."}, nil
}

func (p *accountingPhaseTool) InvokableRun(ctx context.Context, _ string, _ ...tool.Option) (string, error) {
	close(p.entered)
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-p.release:
		return "available", nil
	}
}
