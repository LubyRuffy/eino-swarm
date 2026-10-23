package engine

import (
	"encoding/json"
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
	"github.com/cloudwego/eino/schema"
)

// An ordinary turn owns unfinished delegated work too. A manager's premature
// final must not publish success and cancel the worker that would prove it.
func TestOrdinaryTurnCollectsItsRealWorkerBeforePublishingCompletion(t *testing.T) {
	const premature = "The delegated operation is complete."
	const workerResult = "The assigned inputs were verified by the original worker."
	const collected = "The delegated result was collected and checked."
	const cue = "The manager's completion attempt was withheld because worker results have not been collected"
	var managers, workers atomic.Int32
	var workerID atomic.Value
	workerEntered, candidateOffered, waitOffered := make(chan struct{}), make(chan struct{}), make(chan struct{})
	releaseWorker, workerCancelled := make(chan struct{}), make(chan struct{})
	var enterOnce, candidateOnce, waitOnce, releaseOnce, cancelOnce sync.Once
	var released atomic.Bool
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.URL.Path != "/chat/completions" {
			t.Error("ordinary completion fixture must remain keyless and loopback")
		}
		var input struct {
			Messages []*schema.Message `json:"messages"`
			Stream   bool              `json:"stream"`
			Tools    []struct {
				Function struct{ Name string } `json:"function"`
			} `json:"tools"`
			ToolChoice string `json:"tool_choice"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture input", http.StatusBadRequest)
			return
		}
		if len(input.Messages) > 0 && input.Messages[0].Role == schema.System && input.Messages[0].Content == scheduleAnswerPrompt {
			if input.Stream || len(input.Tools) != 0 || input.ToolChoice != "none" || len(input.Messages) != 2 || input.Messages[1].Role != schema.User {
				t.Error("schedule policy escaped its separate tool-free request")
			}
			writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
			return
		}
		manager := false
		for _, tool := range input.Tools {
			manager = manager || tool.Function.Name == "spawn_agent"
		}
		if !manager {
			if workers.Add(1) != 1 {
				t.Error("ordinary correction restarted the worker instead of collecting it")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"id\":\"ordinary-worker\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"Inspecting the assigned inputs.\"},\"finish_reason\":null}]}\n\n")
			w.(http.Flusher).Flush()
			enterOnce.Do(func() { close(workerEntered) })
			select {
			case <-releaseWorker:
				writeContextFlowReply(w, true, map[string]any{"content": workerResult}, "stop")
			case <-r.Context().Done():
				if !released.Load() {
					cancelOnce.Do(func() { close(workerCancelled) })
				}
			}
			return
		}
		call := func(id, name string, args any) {
			body, _ := json.Marshal(args)
			writeContextFlowReply(w, true, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]any{"name": name, "arguments": string(body)}}}}, "tool_calls")
		}
		switch step := managers.Add(1); step {
		case 1:
			call("ordinary-spawn", "spawn_agent", map[string]any{"role": "verifier", "task": "Inspect the assigned inputs and return the verified result."})
		case 2:
			for _, message := range input.Messages {
				if message.Role != schema.Tool || message.ToolCallID != "ordinary-spawn" {
					continue
				}
				var receipt struct {
					AgentID string `json:"agent_id"`
				}
				if err := json.Unmarshal([]byte(message.Content), &receipt); err != nil || receipt.AgentID == "" {
					t.Errorf("real spawn receipt missing identity: %v", err)
					return
				}
				workerID.Store(receipt.AgentID)
			}
			select {
			case <-workerEntered:
			case <-r.Context().Done():
				return
			}
			candidateOnce.Do(func() { close(candidateOffered) })
			writeContextFlowReply(w, true, map[string]any{"content": premature}, "stop")
		case 3:
			foundCue := false
			for _, message := range input.Messages {
				foundCue = foundCue || message.Role == schema.User && strings.HasPrefix(message.Content, cue)
				if message.Role == schema.Assistant && strings.Contains(message.Content, premature) {
					t.Error("rejected ordinary completion survived in the next model input")
				}
			}
			if !foundCue {
				t.Error("ordinary manager was reentered without the runtime collection cue")
			}
			id, _ := workerID.Load().(string)
			call("ordinary-wait", "wait_agents", map[string]any{"agent_ids": []string{id}, "timeout_s": 30})
			waitOnce.Do(func() { close(waitOffered) })
		case 4:
			found := false
			for _, message := range input.Messages {
				if message.Role != schema.Tool || message.ToolCallID != "ordinary-wait" {
					continue
				}
				var receipt struct {
					Agents []struct {
						AgentID string `json:"agent_id"`
						Status  string `json:"status"`
						Result  string `json:"result"`
					} `json:"agents"`
				}
				if err := json.Unmarshal([]byte(message.Content), &receipt); err != nil {
					t.Error(err)
					continue
				}
				id, _ := workerID.Load().(string)
				found = len(receipt.Agents) == 1 && receipt.Agents[0].AgentID == id && receipt.Agents[0].Status == "done" && receipt.Agents[0].Result == workerResult
			}
			if !found {
				t.Error("manager completed without receiving the actual original worker result")
			}
			writeContextFlowReply(w, true, map[string]any{"content": collected}, "stop")
		default:
			t.Errorf("ordinary completion correction was unbounded: manager requests=%d", step)
			writeContextFlowReply(w, true, map[string]any{"content": collected}, "stop")
		}
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].APIKey = ""
	e.cfg.Models.Providers[0].Model = "offline-ordinary-completion"
	e.pool = provider.New(e.cfg)
	th, err := e.CreateThread("ordinary worker completion", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	turn, err := e.StartTurn(th.ID, "Inspect the assigned inputs.")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = e.Interrupt(th.ID)
		releaseOnce.Do(func() { released.Store(true); close(releaseWorker) })
	})
	select {
	case <-candidateOffered:
	case <-time.After(5 * time.Second):
		t.Fatal("manager never offered its premature final answer")
	}
	select {
	case <-waitOffered:
	case <-workerCancelled:
		got := waitForTurn(t, e, turn.ID)
		t.Fatalf("ordinary final cancelled its still-running worker instead of collecting it: status=%s manager_requests=%d final=%q", got.Status, managers.Load(), got.Final)
	case <-time.After(5 * time.Second):
		t.Fatal("ordinary final never requested the real worker result")
	}
	got, err := e.Store().GetTurn(turn.ID)
	if err != nil || got.Status != store.TurnRunning || e.Status(th.ID).Workers != 1 {
		t.Fatalf("collection did not keep the original turn and worker running: turn=%+v status=%+v err=%v", got, e.Status(th.ID), err)
	}
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	corrected := false
	for _, ev := range events {
		if ev.Kind == KindCleanup || ev.Kind == "done" || strings.Contains(ev.Text, premature) {
			t.Fatalf("uncollected completion became visible before the result: kind=%s seq=%d", ev.Kind, ev.Seq)
		}
		var notice struct {
			Reason    string   `json:"reason"`
			WorkerIDs []string `json:"worker_ids"`
		}
		if ev.Kind == KindModelRetry && json.Unmarshal([]byte(ev.Text), &notice) == nil && notice.Reason == "workers_uncollected" {
			id, _ := workerID.Load().(string)
			corrected = len(notice.WorkerIDs) == 1 && notice.WorkerIDs[0] == id
		}
	}
	if !corrected {
		t.Fatal("ordinary correction lacks a durable notice naming its exact original worker")
	}
	for {
		select {
		case ev := <-sub.C:
			if (ev.Kind == "agent_message" || ev.Kind == "delta" || ev.Kind == "done") && strings.Contains(ev.Text, premature) {
				t.Fatal("premature completion reached the actual live subscriber")
			}
		default:
			goto withheld
		}
	}
withheld:
	releaseOnce.Do(func() { released.Store(true); close(releaseWorker) })
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnDone || finished.Final != collected || workers.Load() != 1 || managers.Load() != 4 {
		t.Fatalf("real collection failed: status=%s final=%q worker_requests=%d manager_requests=%d", finished.Status, finished.Final, workers.Load(), managers.Load())
	}
	select {
	case <-workerCancelled:
		t.Fatal("worker was cancelled before delivering its collected result")
	default:
	}
	events, err = e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	spawned, doneWait := 0, false
	for _, ev := range events {
		if ev.Kind == "spawned" {
			spawned++
		}
		if ev.Kind == "cleanup" || (ev.Kind == "agent_message" || ev.Kind == "done") && strings.Contains(ev.Text, premature) {
			t.Fatalf("completed collection kept an invalid cleanup/answer: kind=%s seq=%d", ev.Kind, ev.Seq)
		}
		if ev.Kind == "tool_result" && ev.ToolCallID == "ordinary-wait" && strings.Contains(ev.Text, workerResult) {
			doneWait = true
		}
	}
	if spawned != 1 || !doneWait || strings.Contains(replayDump(t, e, th.ID), premature) {
		t.Fatalf("ordinary completion lost identity, collection receipt or replay safety: spawned=%d done_wait=%v", spawned, doneWait)
	}
}
