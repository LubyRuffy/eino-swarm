package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// Recovery must preserve each durable observation once at the actual worker
// model boundary. Checking RestoredWorker.History alone misses a second copy
// delivered through its flattened Seed and the worker's steering injector.
func TestRestoreWorkerContextKeepsTailOnceAndIndependentRecoveryEvidence(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		structured bool
		tailCall   bool
	}{
		{name: "structured-checkpoint", structured: true, tailCall: true},
		{name: "legacy-seed"},
		{name: "structured-orphan-receipt", structured: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			const (
				providerID     = "recovery-context-fixture"
				workerModel    = "worker-context-model"
				workerID       = "retained-worker-1"
				checkpointFact = "checkpoint-finding-marker-86b9"
				tailFact       = "tail-finding-marker-9c4e"
				repeatedFact   = "distinct-tail-events-share-text-marker-34fe"
				tailReceipt    = "tail-receipt-marker-24a7"
				summary        = "independent-summary-marker-d3a2"
				inbox          = "independent-inbox-marker-a936"
				tailCallID     = "completed-before-restart-receipt"
				unknownCallID  = "independent-unknown-effect-call"
			)
			var requests atomic.Int32
			captured := make(chan scheduleCompletionRequest, 4)
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					scheduleCompletionRequest
					Model string `json:"model"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode actual worker model input: %v", err)
					http.Error(w, "invalid fixture input", http.StatusBadRequest)
					return
				}
				if request.Model != workerModel {
					t.Errorf("unexpected model %q: fixture restores only its original worker", request.Model)
				}
				requests.Add(1)
				captured <- request.scheduleCompletionRequest
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The retained evidence was inspected."}, "stop")
			}))
			t.Cleanup(endpoint.Close)
			e := newTestEngine(t)
			e.cfg.Swarm.AutoTitle = false
			e.cfg.Models = config.ModelsConfig{Default: providerID, Providers: []config.Provider{{
				ID: providerID, BaseURL: endpoint.URL, Model: "planner-context-model", Catalog: []string{workerModel},
			}}}
			e.pool = provider.New(e.cfg)
			project, err := e.CreateProject("Recovery context fixture", "", "", false)
			if err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("Explicit recovery context", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			turn := plantUnfinishedTurn(t, e, thread.ID, "Inspect the retained evidence.")
			if err := e.store.FinishTurn(turn.ID, store.TurnDone, "Waiting for the next check.", ""); err != nil {
				t.Fatal(err)
			}
			if _, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: thread.ID,
				Title: "Retained worker owner", Prompt: "Inspect progress", EveryS: 3600, CreatedBy: store.ScheduleCreatedManager}); err != nil {
				t.Fatal(err)
			}
			worker := swarm.WorkerCheckpoint{ID: workerID, Role: "inspector", Generation: 3,
				Task: "Inspect the retained evidence.", Instruction: "Inspect the retained evidence.",
				ModelBinding: &swarm.ModelBinding{Ref: provider.ChoiceID(providerID, workerModel), ProviderID: providerID, Model: workerModel},
				Summary:      summary, Inbox: []string{inbox}, InFlight: []swarm.WorkerToolCheckpoint{{
					Name: "exec", CallID: unknownCallID, Arguments: `{"cmd":"inspect prior operation"}`,
				}}}
			if scenario.structured {
				calls := []schema.ToolCall{{ID: unknownCallID, Type: "function", Function: schema.FunctionCall{Name: "exec", Arguments: `{"cmd":"inspect prior operation"}`}}}
				if scenario.tailCall {
					calls = append([]schema.ToolCall{{ID: tailCallID, Type: "function", Function: schema.FunctionCall{Name: "exec", Arguments: `{"cmd":"inspect completed operation"}`}}}, calls...)
				}
				worker.History = []*schema.Message{schema.UserMessage(worker.Task), schema.AssistantMessage(checkpointFact, nil),
					schema.AssistantMessage("", calls)}
			}
			appendParkedCheckpoint(t, e, thread.ID, turn.ID, restartCheckpoint{Version: 1, Workers: []swarm.WorkerCheckpoint{worker}})
			for _, event := range []store.Event{
				{Kind: swarm.NotifyToolResult.String(), ToolCallID: tailCallID, Text: tailReceipt},
				{Kind: swarm.NotifyAgentMessage.String(), Text: tailFact},
				{Kind: swarm.NotifyAgentMessage.String(), Text: repeatedFact},
				{Kind: swarm.NotifyAgentMessage.String(), Text: repeatedFact},
			} {
				event.ThreadID, event.TurnID, event.AgentID = thread.ID, turn.ID, workerID
				event.Role, event.WorkerGeneration = "inspector", 3
				e.record(event)
			}
			next := restartedTestEngine(t, e)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			if err := next.Restore(ctx, nil); err != nil {
				t.Fatal(err)
			}
			var request scheduleCompletionRequest
			select {
			case request = <-captured:
			case <-ctx.Done():
				t.Fatal("restored worker never reached the actual HTTP model")
			}
			waitRestartState(t, func() bool { return next.ActiveWork() == 0 })
			if requests.Load() != 1 {
				t.Errorf("worker HTTP requests=%d; expected one resumed generation", requests.Load())
			}
			content := joinedContent(request.Messages)
			for _, marker := range []string{tailFact, tailReceipt, summary, inbox, unknownCallID} {
				if count := strings.Count(content, marker); count != 1 {
					t.Errorf("actual worker HTTP input repeats or loses %q: occurrences=%d, want 1", marker, count)
				}
			}
			// Equal text does not make two durable events one observation.
			if count := strings.Count(content, repeatedFact); count != 2 {
				t.Errorf("distinct same-text tail events occurrences=%d, want 2", count)
			}
			if scenario.structured {
				if count := strings.Count(content, checkpointFact); count != 1 {
					t.Errorf("checkpoint finding occurrences=%d, want 1", count)
				}
				results := 0
				for _, message := range request.Messages {
					if !scenario.tailCall && message.Role == schema.Tool && message.ToolCallID == tailCallID {
						t.Error("orphan receipt was sent as a tool result without an original call")
					}
					if message.Role == schema.Tool && message.ToolCallID == tailCallID && message.Content == tailReceipt {
						results++
					}
				}
				if scenario.tailCall && results != 1 {
					t.Errorf("structured receipt with original tool-call identity=%d, want 1", results)
				}
				if !scenario.tailCall {
					// A cropped call cannot be invented merely to keep its receipt
					// valid as a protocol ToolMessage. Preserve it as historical data.
					for _, message := range request.Messages {
						for _, call := range message.ToolCalls {
							if call.ID == tailCallID {
								t.Error("recovery fabricated the cropped tool call")
							}
						}
					}
				}
			}
			if !strings.Contains(content, "unknown outcomes") {
				t.Error("independent unknown-effect warning disappeared")
			}
			running, finished := next.workersFromThread(thread.ID)
			if len(running) != 0 || len(finished) != 1 || finished[0].ID != workerID || finished[0].Generation != 3 || finished[0].Collected {
				t.Errorf("recovery changed worker identity or fabricated collection: running=%+v finished=%+v", running, finished)
			}
		})
	}
}
