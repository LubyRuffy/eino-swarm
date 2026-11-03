package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestNuwaWorkerToolScopeSurvivesColdCheckpointAndEvent(t *testing.T) {
	for _, tc := range []struct {
		name      string
		selection *[]string
		fromEvent bool
	}{{"legacy-omission", nil, false}, {"checkpoint-empty", &[]string{}, false}, {"spawn-event-narrowed", &[]string{"read"}, true}} {
		t.Run(tc.name, func(t *testing.T) {
			const providerID, planner, workerModel = "cold-scope-fixture", "planner/selected", "executor/recovered"
			const workerID, generation = "observer-4", 3
			var calls atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					scheduleCompletionRequest
					Model string `json:"model"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					http.Error(w, "invalid fixture input", http.StatusBadRequest)
					return
				}
				if request.Model != workerModel {
					t.Errorf("cold scope changed model to %q", request.Model)
				}
				available := map[string]bool{}
				for _, item := range request.Tools {
					available[item.Function.Name] = true
				}
				for _, name := range []string{"read", "write", "exec"} {
					want := tc.selection == nil || slices.Contains(*tc.selection, name)
					if available[name] != want {
						t.Errorf("cold scope tool %q advertised=%v, want %v", name, available[name], want)
					}
				}
				for _, name := range []string{"list_worker_tools", "spawn_agent", "wait_agents", "list_models", "record_work", "read_work"} {
					if !available[name] {
						t.Errorf("cold recovery removed required control %q", name)
					}
				}
				if calls.Add(1) == 1 {
					nuwaValidationTool(w, request.scheduleCompletionRequest, "restored-scope-record", "record_work",
						`{"kind":"fact","title":"Restored observation","summary":"The original scope was retained."}`)
					return
				}
				var receipt struct{ OK bool }
				if err := json.Unmarshal([]byte(nuwaValidationReceipt(request.scheduleCompletionRequest, "restored-scope-record")), &receipt); err != nil || !receipt.OK {
					t.Errorf("restored worker could not record scoped evidence: %v", err)
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "Restored work completed."}, "stop")
			}))
			t.Cleanup(endpoint.Close)
			e := newTestEngine(t)
			e.cfg.Swarm.AutoTitle = false
			e.cfg.Models = config.ModelsConfig{Default: providerID, Providers: []config.Provider{
				{ID: providerID, BaseURL: endpoint.URL, Model: planner, Catalog: []string{planner, workerModel}},
			}}
			e.pool = provider.New(e.cfg)
			project, err := e.CreateProject("Original tool surface", "", t.TempDir(), false, true)
			if err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("Explicit scoped recovery", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			turn := &store.Turn{ThreadID: thread.ID, ProjectID: project.ID, ProviderID: providerID, Model: planner, NuwaEnabled: true}
			if err := e.store.CreateTurn(turn); err != nil {
				t.Fatal(err)
			}
			if err := e.store.FinishTurn(turn.ID, store.TurnDone, "Waiting for the next check.", ""); err != nil {
				t.Fatal(err)
			}
			if _, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: thread.ID,
				Title: "Scoped worker recovery", Prompt: "Inspect progress", EveryS: 3600, CreatedBy: store.ScheduleCreatedManager}); err != nil {
				t.Fatal(err)
			}
			enabled := true
			binding := &swarm.ModelBinding{Ref: provider.ChoiceID(providerID, workerModel), ProviderID: providerID, Model: workerModel}
			checkpoint := restartCheckpoint{Version: 1}
			if !tc.fromEvent {
				checkpoint.Workers = []swarm.WorkerCheckpoint{{ID: workerID, Role: "observer", Task: "Inspect assigned work.",
					ConfigTag: turn.ID, DelegationEnabled: &enabled, ModelBinding: binding, Generation: generation, WorkerToolNames: tc.selection}}
			}
			appendParkedCheckpoint(t, e, thread.ID, turn.ID, checkpoint)
			if tc.fromEvent {
				event := &store.Event{ThreadID: thread.ID, TurnID: turn.ID, AgentID: workerID, Role: "observer",
					Kind: swarm.NotifySpawned.String(), Text: "Inspect assigned work.", ConfigTag: turn.ID, DelegationEnabled: &enabled,
					ModelBinding: binding, WorkerGeneration: generation, WorkerToolNames: tc.selection}
				if err := e.store.AppendEvent(event); err != nil {
					t.Fatal(err)
				}
			}
			disabled := false
			if _, err := e.UpdateProject(project.ID, nil, nil, nil, nil, &disabled); err != nil {
				t.Fatal(err)
			}
			next := restartedTestEngine(t, e)
			if n, err := next.ResumeParkedWorkers(); err != nil || n != 1 {
				t.Fatalf("cold scoped recovery=%d err=%v", n, err)
			}
			waitRestartState(t, func() bool { return next.ActiveWork() == 0 })
			_, finished := next.workersFromThread(thread.ID)
			if len(finished) != 1 || calls.Load() != 2 {
				t.Fatalf("recovered result missing: finished=%+v calls=%d", finished, calls.Load())
			}
			worker := finished[0]
			if worker.ID != workerID || worker.Generation != generation || worker.Err != nil ||
				worker.ModelBinding == nil || *worker.ModelBinding != *binding ||
				(worker.WorkerToolNames == nil) != (tc.selection == nil) ||
				(tc.selection != nil && (*worker.WorkerToolNames == nil || !slices.Equal(*worker.WorkerToolNames, *tc.selection))) {
				t.Fatalf("cold recovery lost identity, model or explicit tool scope: %+v", worker)
			}
			_, nodes, err := next.store.NuwaSnapshot(t.Context(), project.ID)
			if err != nil || len(nodes) != 1 || nodes[0].AgentID != workerID ||
				nodes[0].Generation != generation || nodes[0].TurnID != turn.ID {
				t.Fatalf("cold scoped record lost its origin: nodes=%+v err=%v", nodes, err)
			}
		})
	}
}
