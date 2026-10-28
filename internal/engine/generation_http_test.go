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
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// The real adapter must report both a request rejected before a stream exists
// and a reasoning stream whose HTTP body ends before its terminal frame.
func TestGenerationFailureAndRetryThroughActualHTTPAdapter(t *testing.T) {
	for _, kind := range []string{"before first byte", "reasoning truncated", "partial tool truncated", "partial tool output limit", "output limit"} {
		t.Run(kind, func(t *testing.T) {
			var requests atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if requests.Add(1) == 1 && kind == "before first byte" {
					http.Error(w, `api_key=private-fixture cannot authorize request`, http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				send := func(delta any, finish any) {
					raw, _ := json.Marshal(map[string]any{"id": "fixture", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
					fmt.Fprintf(w, "data: %s\n\n", raw)
					w.(http.Flusher).Flush()
				}
				if requests.Load() == 1 {
					send(map[string]any{"role": "assistant", "reasoning_content": "Checking the current evidence."}, nil)
					if strings.HasPrefix(kind, "partial tool") {
						send(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "partial-call", "type": "function", "function": map[string]any{"name": "inspect", "arguments": `{"path":"`}}}}, nil)
					}
					if kind == "output limit" || kind == "partial tool output limit" {
						send(map[string]any{}, "length")
						fmt.Fprint(w, "data: [DONE]\n\n")
					}
					return
				}
				send(map[string]any{"role": "assistant", "content": "The check completed."}, nil)
				send(map[string]any{}, "stop")
				fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer endpoint.Close()
			e := newTestEngine(t)
			e.cfg.Models.Default = "generation-fixture"
			e.cfg.Models.Providers = []config.Provider{{ID: "generation-fixture", BaseURL: endpoint.URL, Model: "fixture"}}
			useEngineBackoffClock(t, e)
			th, _ := e.CreateThread("", "", "")
			turn := plantUnfinishedTurn(t, e, th.ID, "Check the current evidence.")
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			builder, err := e.pool.ModelBuilder(ctx, "generation-fixture", "fixture", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			reg := swarm.NewRegistry()
			defer reg.Close()
			reg.ModelBuilder = builder
			acc := newAccumulator(e, th.ID, turn.ID, 0)
			executor := &generationHTTPTool{}
			res, firstErr := reg.RunWith(ctx, swarm.RunConfig{Task: "Check the current evidence.", ManagerTools: []tool.BaseTool{executor}}, swarm.Callback(acc.onNotify))
			if firstErr == nil {
				t.Fatalf("broken generation silently succeeded: %+v", res)
			}
			if executor.executed.Load() != 0 {
				t.Fatal("incomplete HTTP tool arguments reached the actual executor")
			}
			if _, err := reg.RunWith(ctx, swarm.RunConfig{Task: "Continue the check."}, swarm.Callback(acc.onNotify)); err != nil {
				t.Fatal(err)
			}
			acc.flushAll()
			events, _ := e.store.ListTurnEvents(turn.ID)
			interrupted, recovered := 0, 0
			old := ""
			for _, ev := range events {
				if strings.Contains(ev.Text, "private-fixture") || strings.Contains(ev.Err, "private-fixture") {
					t.Fatalf("generation event leaked a credential: %+v", ev)
				}
				if ev.Kind == "tool_call" && ev.ToolCallID == "partial-call" {
					t.Fatalf("incomplete streamed tool was executed: %+v", ev)
				}
				if ev.Kind == "generation_interrupted" {
					interrupted++
					old = ev.MessageID
					if kind == "output limit" && !strings.Contains(ev.Text, `"reason":"output_limit"`) {
						t.Fatalf("output cap cause lost: %+v", ev)
					}
				}
				if ev.Kind == "generation_recovered" {
					recovered++
					if ev.MessageID != old {
						t.Fatalf("recovery matched a different output: %+v", ev)
					}
				}
			}
			if interrupted != 1 || recovered != 1 || old == "" || requests.Load() != 2 {
				t.Fatalf("HTTP lifecycle missing: interruption=%d recovery=%d requests=%d events=%+v", interrupted, recovered, requests.Load(), events)
			}
		})
	}
}

func TestWorkerGenerationHandoffAcrossManagerChecksRetainsOriginalTraceThroughColdRestore(t *testing.T) {
	var requests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"id\":\"worker\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"checking\"},\"finish_reason\":null}]}\n\n")
		w.(http.Flusher).Flush()
		if n == 1 {
			<-r.Context().Done()
			return
		}
		fmt.Fprintf(w, "data: {\"id\":\"worker\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"verified\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer endpoint.Close()
	e := newTestEngine(t)
	e.cfg.Models.Default = "held-worker"
	e.cfg.Models.Providers = []config.Provider{{ID: "held-worker", BaseURL: endpoint.URL, Model: "fixture"}}
	e.pool = provider.New(e.cfg)
	th, _ := e.CreateThread("", "", "")
	first := plantUnfinishedTurn(t, e, th.ID, "Inspect the current evidence.")
	second := plantUnfinishedTurn(t, e, th.ID, "Check progress.")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	builder, err := e.pool.ModelBuilder(ctx, "held-worker", "fixture", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	reg := swarm.NewRegistry()
	defer reg.Close()
	reg.ModelBuilder = builder
	acc := newAccumulator(e, th.ID, first.ID, 0)
	reg.SetHostNotify(acc.onNotify)
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	h, err := reg.Spawn(ctx, "inspector", "Inspect the current evidence.", builder)
	if err != nil {
		t.Fatal(err)
	}
	firstDelta := waitLive(t, sub, "reasoning_delta", 5*time.Second)
	acc.flushAll()
	for range 2 {
		acc = newAccumulator(e, th.ID, second.ID, 0)
		reg.SetHostNotify(acc.onNotify)
		if _, err := reg.RunWith(ctx, swarm.RunConfig{Task: "Check progress.", Model: &outputIdentityModel{}}, swarm.Callback(acc.onNotify)); err != nil {
			t.Fatal(err)
		}
		acc.flushAll()
	}
	stopped, stop := context.WithCancel(ctx)
	stop()
	workers := reg.PrepareRestart(stopped)
	select {
	case <-h.Done():
	case <-ctx.Done():
		t.Fatal("handoff did not stop the first request")
	}
	events, _ := e.store.ListTurnEvents(first.ID)
	interrupted := 0
	for _, ev := range events {
		if ev.Kind == "generation_interrupted" && ev.AgentID == h.ID {
			interrupted++
			if ev.MessageID != firstDelta.MessageID || !strings.Contains(ev.Text, `"reason":"engine_restart"`) {
				t.Fatalf("handoff changed output identity or cause: %+v", ev)
			}
		}
		if ev.Kind == "finished" && ev.AgentID == h.ID {
			t.Fatalf("handoff fabricated a worker failure: %+v", ev)
		}
	}
	if interrupted != 1 {
		t.Fatalf("original turn lost the parked generation interruption: %+v", events)
	}
	next := New(e.cfg, e.store, provider.New(e.cfg), e.log)
	resumed := swarm.NewRegistry()
	defer resumed.Close()
	resumed.ModelBuilder = builder
	resumed.SetHostNotify(newAccumulator(next, th.ID, second.ID, 0).onNotify)
	var restored *swarm.Handle
	for _, w := range workers {
		if w.ID == h.ID {
			restored, err = resumed.Restore(ctx, swarm.RestoredWorker{ID: w.ID, Role: w.Role, Task: w.Task, Instruction: w.Instruction, History: w.History}, builder)
		}
	}
	if err != nil || restored == nil || restored.ID != h.ID {
		t.Fatalf("worker identity did not restore: %+v %v", restored, err)
	}
	select {
	case <-restored.Done():
	case <-ctx.Done():
		t.Fatal("restored generation did not finish")
	}
	events, _ = e.store.ListTurnEvents(first.ID)
	recovered := 0
	for _, ev := range events {
		if ev.Kind == "generation_recovered" && ev.AgentID == h.ID {
			recovered++
			if ev.MessageID != firstDelta.MessageID {
				t.Fatalf("cold recovery changed identity: %+v", ev)
			}
		}
	}
	if recovered != 1 || requests.Load() != 2 {
		t.Fatalf("cold lifecycle did not match original output: recovered=%d requests=%d events=%+v", recovered, requests.Load(), events)
	}
}

func TestWorkerHTTPGenerationDistinguishesNormalShutdownFromPlannedHandoff(t *testing.T) {
	for _, planned := range []bool{false, true} {
		t.Run(fmt.Sprintf("planned=%v", planned), func(t *testing.T) {
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"id\":\"worker\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"checking\"},\"finish_reason\":null}]}\n\n")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer endpoint.Close()
			e := newTestEngine(t)
			e.cfg.Models.Default = "shutdown-fixture"
			e.cfg.Models.Providers = []config.Provider{{ID: "shutdown-fixture", BaseURL: endpoint.URL, Model: "fixture"}}
			e.pool = provider.New(e.cfg)
			th, _ := e.CreateThread("", "", "")
			turn := plantUnfinishedTurn(t, e, th.ID, "Check the current evidence.")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			builder, err := e.pool.ModelBuilder(ctx, "shutdown-fixture", "fixture", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			reg := swarm.NewRegistry()
			reg.ModelBuilder = builder
			defer reg.Close()
			rt := e.runtimeFor(th.ID)
			rt.mu.Lock()
			rt.parked = reg
			rt.turnID = turn.ID
			rt.mu.Unlock()
			acc := newAccumulator(e, th.ID, turn.ID, 0)
			reg.SetHostNotify(acc.onNotify)
			sub := e.Subscribe(th.ID)
			defer sub.Close()
			h, err := reg.Spawn(ctx, "inspector", "Inspect inputs.", builder)
			if err != nil {
				t.Fatal(err)
			}
			delta := waitLive(t, sub, "reasoning_delta", 5*time.Second)
			if planned {
				rt.abandon()
				stopped, stop := context.WithCancel(ctx)
				stop()
				reg.PrepareRestart(stopped)
			} else {
				e.Shutdown()
			}
			select {
			case <-h.Done():
			case <-ctx.Done():
				t.Fatal("HTTP generation did not stop")
			}
			events, _ := e.store.ListGenerationEvents(th.ID)
			found := false
			for _, ev := range events {
				if ev.Kind == "generation_interrupted" && ev.MessageID == delta.MessageID {
					found = true
					var body swarm.GenerationInterruption
					_ = json.Unmarshal([]byte(ev.Text), &body)
					want := "cancelled"
					if planned {
						want = "engine_restart"
					}
					if body.Reason != want || body.Recovery != "pending" || !planned && !strings.Contains(body.Detail, "shutting down") {
						t.Fatalf("shutdown invented a restart: planned=%v %+v", planned, body)
					}
				}
			}
			if !found {
				t.Fatalf("shutdown lost the actual worker generation: %+v", events)
			}
		})
	}
}

type generationHTTPTool struct{ executed atomic.Int32 }

func (*generationHTTPTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "inspect", Desc: "Inspect the current evidence.", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{})}, nil
}
func (m *generationHTTPTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	m.executed.Add(1)
	return "must not execute incomplete arguments", nil
}

func TestDeletedConversationCannotBeRevivedByALateActualWorkerGenerationCallback(t *testing.T) {
	for _, projectDelete := range []bool{false, true} {
		t.Run(fmt.Sprintf("project=%v", projectDelete), func(t *testing.T) {
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"id\":\"worker\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"checking\"},\"finish_reason\":null}]}\n\n")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer endpoint.Close()
			e := newTestEngine(t)
			e.cfg.Models.Default = "delete-fixture"
			e.cfg.Models.Providers = []config.Provider{{ID: "delete-fixture", BaseURL: endpoint.URL, Model: "fixture"}}
			e.pool = provider.New(e.cfg)
			projectID := ""
			if projectDelete {
				p, err := e.CreateProject("callback project", "", "", false)
				if err != nil {
					t.Fatal(err)
				}
				projectID = p.ID
			}
			th, err := e.CreateThread("", "", projectID)
			if err != nil {
				t.Fatal(err)
			}
			turn := plantUnfinishedTurn(t, e, th.ID, "Check inputs.")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			builder, err := e.pool.ModelBuilder(ctx, "delete-fixture", "fixture", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			reg := swarm.NewRegistry()
			defer reg.Close()
			reg.ModelBuilder = builder
			rt := e.runtimeFor(th.ID)
			rt.mu.Lock()
			rt.parked, rt.turnID = reg, turn.ID
			rt.mu.Unlock()
			acc := newAccumulator(e, th.ID, turn.ID, 0)
			callback := make(chan struct{})
			released := make(chan struct{})
			var release sync.Once
			defer release.Do(func() { close(released) })
			reg.SetHostNotify(func(n swarm.Notification) {
				if n.Kind == swarm.NotifyGenerationInterrupted {
					close(callback)
					<-released
				}
				acc.onNotify(n)
			})
			sub := e.Subscribe(th.ID)
			defer sub.Close()
			h, err := reg.Spawn(ctx, "inspector", "Inspect inputs.", builder)
			if err != nil {
				t.Fatal(err)
			}
			waitLive(t, sub, "reasoning_delta", 5*time.Second)
			if projectDelete {
				err = e.DeleteProject(projectID)
			} else {
				err = e.DeleteThread(th.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.store.GetThread(th.ID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("test did not delete the conversation: %v", err)
			}
			select {
			case <-callback:
			case <-ctx.Done():
				t.Fatal("actual worker cancellation callback never arrived")
			}
			release.Do(func() { close(released) })
			select {
			case <-h.Done():
			case <-ctx.Done():
				t.Fatal("late worker callback did not finish")
			}
			events, _ := e.store.ListGenerationEvents(th.ID)
			e.generations.Lock()
			_, cached := e.generations.threads[th.ID]
			e.generations.Unlock()
			if len(events) != 0 || cached {
				t.Fatalf("deleted conversation resurrected from late generation: cached=%v events=%+v", cached, events)
			}
			if rows, err := e.store.ListEvents(th.ID, 0, 0); err != nil || len(rows) != 0 {
				t.Fatalf("late worker rows outlived the deletion: err=%v rows=%+v", err, rows)
			}
		})
	}
}
