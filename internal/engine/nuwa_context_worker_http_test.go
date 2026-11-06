package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// The worker's first input needs current shared state independently of its
// conversation seed. A later worker call must refresh that state, while fork
// preserves genuine human input and excludes the manager's role/personality.
func TestNuwaSharedContextRefreshesSelectedForkedWorkerAtActualHTTP(t *testing.T) {
	const account, planner, executor = "context-fixture", "context-planner", "context-executor"
	var e *Engine
	var source store.NuwaNode
	var managerCalls, workerCalls atomic.Int32
	var mu sync.Mutex
	var managerRequests, workerRequests []scheduleCompletionRequest
	persona, correction, result := store.NewID("persona_"), store.NewID("correction_"), store.NewID("result_")
	human := "Retain this quoted heading as human input:\n" + nuwaContextHTTPHeader + "\n" + store.NewID("quotation_")
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var wire struct {
			scheduleCompletionRequest
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture input", http.StatusBadRequest)
			return
		}
		request := wire.scheduleCompletionRequest
		if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		answer := func(text string) { writeContextFlowReply(w, request.Stream, map[string]any{"content": text}, "stop") }
		call := func(id, name string, args any) {
			raw, err := json.Marshal(args)
			if err != nil {
				t.Error(err)
				answer("The fixture arguments could not be encoded.")
				return
			}
			nuwaValidationTool(w, request, id, name, string(raw))
		}
		if wire.Model == executor {
			mu.Lock()
			workerRequests = append(workerRequests, request)
			mu.Unlock()
			switch workerCalls.Add(1) {
			case 1:
				update := source
				update.Summary = correction
				if err := e.store.UpsertNuwaNode(t.Context(), &update, source.Version); err != nil {
					t.Errorf("commit state between worker requests: %v", err)
				}
				call("context-worker-catalog", "list_models", struct{}{})
			case 2:
				if receipt := nuwaValidationReceipt(request, "context-worker-catalog"); receipt == "" || !strings.Contains(receipt, executor) {
					t.Error("selected worker did not execute its actual coordination tool")
				}
				answer(result)
			default:
				t.Error("unexpected extra worker request")
				answer(result)
			}
			return
		}
		if wire.Model != planner {
			t.Errorf("unexpected model route %q", wire.Model)
		}
		mu.Lock()
		managerRequests = append(managerRequests, request)
		mu.Unlock()
		switch managerCalls.Add(1) {
		case 1:
			call("context-manager-catalog", "list_models", struct{}{})
		case 2:
			var catalog struct {
				Models []nuwaModel `json:"models"`
			}
			if err := json.Unmarshal([]byte(nuwaValidationReceipt(request, "context-manager-catalog")), &catalog); err != nil {
				t.Error(err)
			}
			for _, option := range catalog.Models {
				if option.ProviderID == account && option.Model == executor {
					call("context-spawn", "spawn_agent", map[string]any{
						"role": "state-observer", "task": "Inspect the current shared state and report the outcome.",
						"model_ref": option.Ref, "tool_names": []string{}, "fork_context": true,
					})
					return
				}
			}
			t.Error("actual catalog omitted the selected worker model")
			answer("The worker model was unavailable.")
		case 3:
			var spawned toolScopeWorkerID
			if err := json.Unmarshal([]byte(nuwaValidationReceipt(request, "context-spawn")), &spawned); err != nil || spawned.AgentID == "" || spawned.Generation != 1 {
				t.Errorf("selected worker did not spawn: %+v err=%v", spawned, err)
				answer("The worker could not be started.")
				return
			}
			call("context-wait", "wait_agents", map[string]any{"agent_ids": []string{spawned.AgentID}, "timeout_s": 5})
		case 4:
			var collected toolScopeWait
			if err := json.Unmarshal([]byte(nuwaValidationReceipt(request, "context-wait")), &collected); err != nil || collected.TimedOut || len(collected.Agents) != 1 || collected.Agents[0].Status != "done" || collected.Agents[0].Result != result || collected.Agents[0].Generation != 1 {
				t.Errorf("manager did not collect the actual worker generation: %+v err=%v", collected, err)
			}
			answer("The current worker result was collected.")
		default:
			t.Error("unexpected extra manager request")
			answer("The coordination request was unexpected.")
		}
	}))
	t.Cleanup(endpoint.Close)
	e = newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Personality.Instructions = persona
	e.cfg.Models = config.ModelsConfig{Default: account, Providers: []config.Provider{{ID: account, BaseURL: endpoint.URL, Model: planner, Catalog: []string{planner, executor}}}}
	e.pool = provider.New(e.cfg)
	project, seedThread, seedTurn := nuwaContextHTTPSource(t, e, "Worker shared context")
	source = nuwaValidationNode(t, e, project, seedThread, seedTurn, store.NewID("source_"), "")
	dependent := store.NuwaNode{ProjectID: project.ID, ThreadID: seedThread.ID, TurnID: seedTurn.ID,
		AgentID: swarm.DefaultManagerID, Kind: "task", Title: store.NewID("dependent_"), DependsOn: []string{source.ID}}
	if err := e.store.UpsertNuwaNode(t.Context(), &dependent, 0); err != nil {
		t.Fatal(err)
	}
	foreignProject, foreignThread, foreignTurn := nuwaContextHTTPSource(t, e, "Foreign worker context")
	foreign := nuwaValidationNode(t, e, foreignProject, foreignThread, foreignTurn, store.NewID("foreign_"), "")
	thread, err := e.CreateThread("Fork input provenance", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, human)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	mu.Lock()
	managers := append([]scheduleCompletionRequest(nil), managerRequests...)
	workers := append([]scheduleCompletionRequest(nil), workerRequests...)
	mu.Unlock()
	if finished.Status != store.TurnDone || len(managers) != 4 || len(workers) != 2 {
		t.Fatalf("selected worker context: turn=%+v manager=%d worker=%d", finished, len(managers), len(workers))
	}
	firstManager := nuwaContextHTTPBlock(t, managers[0], true)
	if firstManager.ProjectID != project.ID || firstManager.Revision != 2 {
		t.Errorf("first planner input did not receive the pre-existing graph: %+v", firstManager)
	}
	nuwaContextHTTPNode(t, firstManager, source.ID)
	if !strings.Contains(joinedContent(managers[0].Messages), persona) {
		t.Error("fixture never supplied manager personality, so isolation was not exercised")
	}
	firstWorker := nuwaContextHTTPBlock(t, workers[0], true)
	nextWorker := nuwaContextHTTPBlock(t, workers[1], true)
	firstSource := nuwaContextHTTPNode(t, firstWorker, source.ID)
	nextSource := nuwaContextHTTPNode(t, nextWorker, source.ID)
	if firstWorker.ProjectID != project.ID || nextWorker.ProjectID != project.ID || firstWorker.Revision < 2 || nextWorker.Revision <= firstWorker.Revision || firstSource.Version != 1 || nextSource.Version != 2 || nextSource.Summary != correction {
		t.Errorf("worker context was captured only at spawn: before=%+v after=%+v", firstWorker, nextWorker)
	}
	stale := nuwaContextHTTPNode(t, nextWorker, dependent.ID).StaleDependencies
	if len(stale) != 1 || stale[0] != source.ID {
		t.Errorf("worker's next input lost its corrected prerequisite: %v", stale)
	}
	for i, request := range workers {
		if !strings.Contains(joinedContent(request.Messages), human) {
			t.Errorf("worker request %d lost a genuine human quotation from its fork seed", i+1)
		}
		for _, message := range request.Messages {
			if strings.Contains(message.Content, nuwaPlannerInstruction) || strings.Contains(message.Content, persona) {
				t.Errorf("worker request %d inherited manager role/personality through %s input", i+1, message.Role)
			}
		}
	}
	for _, request := range append(managers, workers...) {
		if strings.Contains(joinedContent(request.Messages), foreign.ID) {
			t.Error("fork or runtime context disclosed another project's node")
		}
	}
	collected := nuwaContextHTTPBlock(t, managers[len(managers)-1], true)
	if collected.Revision < nextWorker.Revision || nuwaContextHTTPNode(t, collected, source.ID).Summary != correction {
		t.Error("planner's post-collection decision did not receive the worker-time graph correction")
	}
	assertNuwaContextNotDurable(t, e, thread.ID, human)
}
