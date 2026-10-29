package engine

import (
	"encoding/json"
	"fmt"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A worker first created after reusing a Registry must keep dynamic accounting
// when that same Registry is claimed by another real StartTurn. Existing tests
// which only retain an older worker do not cover a factory wrapper overwritten
// after occupancy. Both worker requests here cross the real HTTP adapter.
func TestFreshWorkerSpawnedByReusedRegistryBillsItsSuccessor(t *testing.T) {
	var managerCalls, workerCalls atomic.Int32
	var workerID atomic.Value
	workerID.Store("")
	workerReady, releaseWorker, collecting := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseWorker) }) }
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
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
			if workerCalls.Add(1) == 1 {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"id\":\"billing-worker\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"Waiting for the successor.\"},\"finish_reason\":null}]}\n\n")
				w.(http.Flusher).Flush()
				close(workerReady)
				select {
				case <-releaseWorker:
				case <-r.Context().Done():
					return
				}
				writeHistoryTool(w, request, "billing-phase", "send_message", `{"agent_id":"manager","text":"The next phase is available."}`)
			} else {
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The existing worker completed."}, "stop")
			}
			return
		}
		switch managerCalls.Add(1) {
		case 1:
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The first check is complete."}, "stop")
		case 2:
			writeHistoryTool(w, request, "new-in-b", "spawn_agent", `{"role":"fresh-reader","task":"Keep the assigned work active until the next check."}`)
		case 3:
			for _, m := range request.Messages {
				if m.Role == "tool" && m.ToolCallID == "new-in-b" {
					var receipt struct {
						AgentID string `json:"agent_id"`
					}
					if json.Unmarshal([]byte(m.Content), &receipt) == nil {
						workerID.Store(receipt.AgentID)
					}
				}
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The worker remains active."}, "stop")
		case 4:
			args, _ := json.Marshal(map[string]any{"agent_ids": []string{workerID.Load().(string)}, "timeout_s": 10})
			close(collecting)
			writeHistoryTool(w, request, "collect-in-c", "wait_agents", string(args))
		default:
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The collected work is complete."}, "stop")
		}
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "offline-successor-accounting"
	e.pool = provider.New(e.cfg)
	t.Cleanup(release)
	th, err := e.CreateThread("Fresh-worker successor accounting", "", "")
	if err != nil {
		t.Fatal(err)
	}
	wake, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID, EveryS: 3600, Title: "Actual Registry reuse", Prompt: "Inspect the current work.", CreatedBy: store.ScheduleCreatedHuman})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.CancelSchedule(wake.ID) })
	a, err := e.StartTurn(th.ID, "Complete the first check.")
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, a.ID); got.Status != store.TurnDone {
		t.Fatalf("A failed: %+v", got)
	}
	waitSettled(t, e, th.ID)
	b, err := e.StartTurn(th.ID, "Spawn the new assigned worker.")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-workerReady:
	case <-time.After(5 * time.Second):
		t.Fatal("B did not spawn its actual fresh HTTP worker")
	}
	if got := waitForTurn(t, e, b.ID); got.Status != store.TurnDone {
		t.Fatalf("B failed: %+v", got)
	}
	waitSettled(t, e, th.ID)
	c, err := e.StartTurn(th.ID, "Collect the existing worker.")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-collecting:
	case <-time.After(5 * time.Second):
		t.Fatal("C did not reach actual worker collection")
	}
	release()
	got := waitForTurn(t, e, c.ID)
	waitSettled(t, e, th.ID)
	if got.Status != store.TurnDone || workerCalls.Load() != 2 || managerCalls.Load() != 5 {
		t.Fatalf("fixture missed new B worker across C: final=%+v worker=%d manager=%d", got, workerCalls.Load(), managerCalls.Load())
	}
	count := func(id string) int {
		calls, err := e.store.ListLLMCalls(id)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, call := range calls {
			if call.AgentID == workerID.Load().(string) {
				n++
			}
		}
		return n
	}
	if onB, onC := count(b.ID), count(c.ID); onB != 1 || onC != 1 {
		t.Fatalf("actual worker spawned under reused B retained stale accounting after C: B=%d C=%d worker=%s", onB, onC, workerID.Load())
	}
}
