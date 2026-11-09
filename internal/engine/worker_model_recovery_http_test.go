package engine

import (
	"encoding/json"
	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestExplicitWorkerRecoveryReachesHTTPWithOriginalIdentityAndPermissions(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "shared-write", true: "read-only"}[readOnly], func(t *testing.T) {
			var calls atomic.Int32
			var e *Engine
			externalReceipt := filepath.Join(t.TempDir(), "mcp-command-started")
			const readID = "restored-source-read"
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
				if request.Model != "executor/replacement" {
					t.Errorf("recovery silently changed binding to %q", request.Model)
				}
				available := map[string]bool{}
				for _, item := range request.Tools {
					available[item.Function.Name] = true
				}
				external := false
				for name := range available {
					external = external || strings.HasPrefix(name, "mcp_")
				}
				if external == readOnly {
					t.Errorf("MCP worker schema lost original read-only mode: external=%v readOnly=%v", external, readOnly)
				}
				if !available["list_models"] || !available["spawn_agent"] || !available["read_work"] || available["record_work"] == readOnly {
					t.Errorf("original worker mode lost across recovery: %+v", available)
				}
				step := calls.Add(1)
				if !readOnly {
					switch step {
					case 1:
						nuwaValidationTool(w, request.scheduleCompletionRequest, readID, "read", `{"file_path":"source.txt"}`)
						return
					case 2:
						if !strings.Contains(nuwaValidationReceipt(request.scheduleCompletionRequest, readID), "Original source retained.") {
							t.Error("recovered worker did not receive the source")
						}
						if _, err := awaitNuwaToolResult(r.Context(), e.store, readID); err != nil {
							t.Error(err)
							http.Error(w, "missing durable receipt", http.StatusInternalServerError)
							return
						}
						nuwaValidationTool(w, request.scheduleCompletionRequest, "restored-record", "record_work",
							`{"kind":"fact","title":"Recovered observation","own_tool_call_ids":["restored-source-read"]}`)
						return
					case 3:
						if !strings.Contains(nuwaValidationReceipt(request.scheduleCompletionRequest, "restored-record"), `"ok":true`) {
							t.Error("recovered worker could not cite its new source")
						}
					}
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "Recovered work completed."}, "stop")
			}))
			t.Cleanup(endpoint.Close)
			e = newTestEngine(t)
			e.cfg.MCP.Servers = []config.MCPServer{{ID: "external", Transport: "stdio", Command: os.Args[0], Args: []string{"-test.run=^TestPlanningMCPCommandFixture$"}, Env: map[string]string{"ZWAI_MCP_PLANNING_RECEIPT": externalReceipt}, Enabled: true}}
			e.cfg.Swarm.AutoTitle = false
			e.cfg.Models.Default = "restore-fixture"
			e.cfg.Models.Providers = []config.Provider{{ID: "restore-fixture", BaseURL: endpoint.URL,
				Model: "planner/current", Catalog: []string{"planner/original", "planner/current", "executor/original", "executor/replacement"}}}
			e.pool = provider.New(e.cfg)
			workspace := t.TempDir()
			if err := os.WriteFile(filepath.Join(workspace, "source.txt"), []byte("Original source retained.\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			project, err := e.CreateProject("Original project", "Use the supplied evidence.", workspace, false, true)
			if err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("Explicit recovery fixture", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			turn := &store.Turn{ThreadID: thread.ID, ProjectID: project.ID, ProviderID: "removed-provider", Model: "planner/original", NuwaEnabled: true, NuwaReadOnly: readOnly, NuwaExecutorProviderID: "removed-provider", NuwaExecutorModel: "executor/original"}
			if err := e.store.CreateTurn(turn); err != nil {
				t.Fatal(err)
			}
			if err := e.store.FinishTurn(turn.ID, store.TurnDone, "Waiting for the next check.", ""); err != nil {
				t.Fatal(err)
			}
			latest := &store.Turn{ThreadID: thread.ID, ProjectID: project.ID, ProviderID: "removed-provider", Model: "planner/current", NuwaEnabled: true}
			if err := e.store.CreateTurn(latest); err != nil {
				t.Fatal(err)
			}
			if err := e.store.FinishTurn(latest.ID, store.TurnDone, "The worker stayed active.", ""); err != nil {
				t.Fatal(err)
			}
			if _, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: thread.ID, Title: "Recovery ownership", Prompt: "Inspect progress", EveryS: 3600, CreatedBy: store.ScheduleCreatedManager}); err != nil {
				t.Fatal(err)
			}
			enabled := true
			binding := &swarm.ModelBinding{Ref: provider.ChoiceID("restore-fixture", "executor/original"), ProviderID: "removed-provider", Model: "executor/original"}
			appendParkedCheckpoint(t, e, thread.ID, latest.ID, restartCheckpoint{Version: 1, Workers: []swarm.WorkerCheckpoint{{
				ID: "observer-4", Role: "observer", Generation: 3, ConfigTag: turn.ID, DelegationEnabled: &enabled, ModelBinding: binding,
				Task: "Inspect the supplied evidence.", History: []*schema.Message{schema.UserMessage("Inspect the supplied evidence.")},
			}, {ID: "completed-5", Role: "completed", Generation: 2, ConfigTag: turn.ID, DelegationEnabled: &enabled, ModelBinding: binding, Finished: true, Result: "Original completed result"}}})
			disabled := false
			if _, err := e.UpdateProject(project.ID, nil, nil, nil, nil, &disabled); err != nil {
				t.Fatal(err)
			}
			next := restartedTestEngine(t, e)
			if _, err := next.workerConfigurationForTag(t.Context(), thread.ID, turn.ID); err == nil || !strings.Contains(err.Error(), "unknown provider") {
				t.Fatalf("ordinary model switch changed an old worker: %v", err)
			}
			if n, err := next.RecoverWorkerModels(t.Context(), thread.ID, "restore-fixture", "executor/replacement"); err != nil || n != 2 {
				t.Fatalf("explicit recovery=%d %v", n, err)
			}
			original, err := next.store.GetTurn(turn.ID)
			if err != nil || original.ProviderID != "removed-provider" || original.Model != "planner/original" {
				t.Fatalf("historical selection rewritten: %+v %v", original, err)
			}
			*binding = original.WorkerModelRecovery["removed-provider"]
			if original.NuwaExecutorProviderID != "removed-provider" {
				t.Fatal("historical executor rewritten")
			}
			restoredConfig, err := next.workerConfigurationForTag(t.Context(), thread.ID, turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			selected, _, err := restoredConfig.SelectWorkerModel(t.Context(), "")
			if err != nil || selected != *binding {
				t.Fatalf("new role kept a missing frozen executor: %+v %v", selected, err)
			}
			// Reopen storage after the human action before restoring the worker.
			next = restartedTestEngine(t, next)
			if n, err := next.ResumeParkedWorkers(); err != nil || n != 1 {
				t.Fatalf("cold worker recovery=%d err=%v", n, err)
			}
			waitRestartState(t, func() bool { return next.ActiveWork() == 0 })
			if _, err := os.Stat(externalReceipt); !os.IsNotExist(err) {
				t.Fatalf("restoration eagerly launched an MCP subprocess: %v", err)
			}
			_, allFinished := next.workersFromThread(thread.ID)
			var finished []swarm.FinishedWorker
			for _, worker := range allFinished {
				if worker.ID == "observer-4" {
					finished = append(finished, worker)
				}
			}
			if len(finished) != 1 || finished[0].ID != "observer-4" || finished[0].Generation != 3 || finished[0].Err != nil || finished[0].ConfigTag != turn.ID || finished[0].ModelBinding == nil || *finished[0].ModelBinding != *binding {
				t.Fatalf("restored identity/binding=%+v", finished)
			}
			_, nodes, err := e.store.NuwaSnapshot(t.Context(), project.ID)
			if err != nil {
				t.Fatal(err)
			}
			if readOnly {
				if len(nodes) != 0 || calls.Load() != 1 {
					t.Fatalf("read-only recovery wrote shared work: nodes=%+v calls=%d", nodes, calls.Load())
				}
			} else {
				read, err := awaitNuwaToolResult(t.Context(), e.store, readID)
				if err != nil || len(nodes) != 1 || nodes[0].TurnID != turn.ID || nodes[0].AgentID != "observer-4" ||
					nodes[0].Generation != 3 || calls.Load() != 3 || len(nodes[0].Evidence) != 1 ||
					nodes[0].Evidence[0].TurnID != latest.ID || nodes[0].Evidence[0].Seq != read.Seq ||
					read.TurnID != latest.ID || read.ToolReceiptScope == "" {
					t.Fatalf("recovered write lost original provenance or actual observation: nodes=%+v calls=%d read=%+v err=%v",
						nodes, calls.Load(), read, err)
				}
			}
			rt := next.runtimeFor(thread.ID)
			rt.mu.Lock()
			reg := rt.parked
			rt.mu.Unlock()
			if reg == nil {
				t.Fatal("owned Registry was lost")
			}
			before := reg.SnapshotWorkers()
			for _, worker := range before {
				if worker.ID == "completed-5" && (worker.ModelBinding.ProviderID != "removed-provider" || worker.Result != "Original completed result") {
					t.Fatalf("completed result history rewritten: %+v", worker)
				}
			}
			h, err := reg.Resume(withHistoryReadOwner(t.Context(), thread.ID, latest.ID), "completed-5", "Continue the retained task", nil)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-h.Done():
			case <-time.After(3 * time.Second):
				t.Fatal("same-ID resumption did not finish")
			}
			for _, worker := range reg.SnapshotWorkers() {
				if worker.ID == "completed-5" && (worker.ModelBinding.Model != "executor/replacement" || worker.Generation != 3) {
					t.Fatalf("resumption lost actual model or identity: %+v", worker)
				}
			}

		})
	}
}
