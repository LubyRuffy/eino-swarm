package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestNuwaRetainedWorkerCitesEvidenceObservedByNextWake(t *testing.T) {
	const (
		providerID = "wake-evidence-fixture"
		planner    = "fixture/planner"
		executor   = "fixture/executor"
		source     = "wake-observation"
		readID     = "adopted-read"
	)
	workerEntered, adopted := make(chan struct{}), make(chan struct{})
	readReceipt := make(chan store.Event, 1)
	recordReply := make(chan string, 1)
	var managerCalls, workerCalls atomic.Int32
	var worker atomic.Pointer[toolScopeWorkerID]
	var e *Engine
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			scheduleCompletionRequest
			Model string `json:"model"`
		}
		fail := func(reason string) {
			t.Error(reason)
			http.Error(w, "unexpected wake evidence fixture input", http.StatusBadRequest)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			fail(err.Error())
			return
		}
		if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		result := func(id string) string { return nuwaValidationReceipt(request.scheduleCompletionRequest, id) }
		call := func(id, name, args string) { nuwaValidationTool(w, request.scheduleCompletionRequest, id, name, args) }
		answer := func(s string) { writeContextFlowReply(w, request.Stream, map[string]any{"content": s}, "stop") }
		if request.Model == executor {
			switch workerCalls.Add(1) {
			case 1:
				close(workerEntered)
				select {
				case <-adopted:
					call(readID, "read", `{"file_path":"source.txt"}`)
				case <-r.Context().Done():
					fail("worker was cancelled before the next wake adopted it")
				}
			case 2:
				if !strings.Contains(result(readID), source) {
					fail("worker did not receive the real source contents")
					return
				}
				// The worker can receive its tool reply before the event consumer
				// commits it. Wait for that real receipt to isolate turn ownership.
				read, err := awaitNuwaToolResult(r.Context(), e.store, readID)
				if err != nil {
					fail(err.Error())
					return
				}
				readReceipt <- read
				call("adopted-record", "record_work", `{"kind":"fact","title":"Source observation","own_tool_call_ids":["adopted-read"]}`)
			case 3:
				recordReply <- result("adopted-record")
				answer("Source inspection ended.")
			default:
				fail("unexpected worker request")
			}
			return
		}
		if request.Model != planner || len(request.Tools) == 0 {
			fail("unexpected manager route or missing tools")
			return
		}
		switch managerCalls.Add(1) {
		case 1:
			call("adopted-spawn", "spawn_agent", fmt.Sprintf(`{"role":"source-reader","task":"Inspect the assigned source.","model_ref":%q,"tool_names":["read"]}`, nuwaModelRef(providerID, executor)))
		case 2:
			var id toolScopeWorkerID
			if err := json.Unmarshal([]byte(result("adopted-spawn")), &id); err != nil || id.AgentID == "" || id.Generation <= 0 {
				fail("spawn did not return a durable worker identity")
				return
			}
			worker.Store(&id)
			call("adopted-report-first", ToolReportSchedule, `{"findings":"The inspection is still running."}`)
		case 3, 6:
			answer("")
		case 4:
			// This request occurs after the new accumulator is attached. Releasing
			// here keeps the test from citing a receipt emitted under the first turn.
			close(adopted)
			call("adopted-wait", "wait_agents", fmt.Sprintf(`{"agent_ids":[%q],"timeout_s":5}`, worker.Load().AgentID))
		case 5:
			var wait toolScopeWait
			if err := json.Unmarshal([]byte(result("adopted-wait")), &wait); err != nil || wait.TimedOut ||
				len(wait.Agents) != 1 || wait.Agents[0].toolScopeWorkerID != *worker.Load() || wait.Agents[0].Status != "done" {
				fail("manager did not collect the original worker generation")
				return
			}
			call("adopted-report-next", ToolReportSchedule, `{"findings":"The worker finished the inspection."}`)
		default:
			fail("unexpected manager request")
		}
	}))
	t.Cleanup(endpoint.Close)
	e = newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Models = config.ModelsConfig{Default: providerID, Providers: []config.Provider{
		{ID: providerID, BaseURL: endpoint.URL, Model: planner, Catalog: []string{planner, executor}},
	}}
	e.pool = provider.New(e.cfg)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "source.txt"), []byte(source+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := e.CreateProject("Evidence continuity", "", workspace, false, true)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Recurring source inspection", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	wake, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: thread.ID, Title: "Inspection wake",
		Prompt: "Check the inspection progress.", EveryS: 3600, CreatedBy: store.ScheduleCreatedHuman})
	if err != nil {
		t.Fatal(err)
	}
	runWake := func() *store.Turn {
		t.Helper()
		turn, err := e.RunScheduleNow(wake.ID)
		if err != nil {
			t.Fatal(err)
		}
		got := waitForTurn(t, e, turn.ID)
		waitSettled(t, e, thread.ID)
		if got.Status != store.TurnDone {
			t.Fatalf("wake status=%s err=%s manager=%d worker=%d", got.Status, got.Error, managerCalls.Load(), workerCalls.Load())
		}
		return got
	}
	first := runWake()
	select {
	case <-workerEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker never reached its actual model endpoint")
	}
	second := runWake()
	if len(readReceipt) != 1 || len(recordReply) != 1 {
		t.Fatalf("missing worker receipts: model calls=%d", workerCalls.Load())
	}
	read, reply := <-readReceipt, <-recordReply
	id := worker.Load()
	if read.TurnID != second.ID || read.ThreadID != thread.ID || read.AgentID != id.AgentID || read.WorkerGeneration != id.Generation {
		t.Fatalf("read was not persisted under the adopting wake and original worker: %+v", read)
	}
	_, nodes, err := e.store.NuwaSnapshot(t.Context(), project.ID)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("retained worker could not cite its adopted-turn receipt: nodes=%+v err=%v model reply=%s", nodes, err, reply)
	}
	node := nodes[0]
	if node.TurnID != first.ID || node.AgentID != id.AgentID || node.Generation != id.Generation ||
		len(node.Evidence) != 1 || node.Evidence[0].ThreadID != thread.ID || node.Evidence[0].TurnID != second.ID || node.Evidence[0].Seq != read.Seq {
		t.Fatalf("fact lost original owner or actual observation: %+v, read seq=%d", node, read.Seq)
	}
	var calls []store.Event
	err = e.store.DB().Where("kind = ? AND tool_call_id = ?", "tool_call", readID).Find(&calls).Error
	if err != nil || len(calls) != 1 || calls[0].TurnID != second.ID || calls[0].AgentID != id.AgentID ||
		calls[0].WorkerGeneration != id.Generation || read.ToolReceiptScope == "" || calls[0].ToolReceiptScope != read.ToolReceiptScope {
		t.Fatalf("actual call and result lost their common execution scope: calls=%+v result=%+v err=%v", calls, read, err)
	}
}

func awaitNuwaToolResult(ctx context.Context, st *store.Store, callID string) (store.Event, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	for {
		var rows []store.Event
		err := st.DB().WithContext(ctx).Where("kind = ? AND tool_call_id = ?", "tool_result", callID).Limit(2).Find(&rows).Error
		switch {
		case err != nil:
			return store.Event{}, fmt.Errorf("read durable tool receipt: %w", err)
		case len(rows) == 1:
			return rows[0], nil
		case len(rows) > 1:
			return store.Event{}, fmt.Errorf("multiple durable tool receipts for %q", callID)
		}
		time.Sleep(time.Millisecond)
	}
}
