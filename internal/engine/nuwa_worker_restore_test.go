package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// Recovery runs through the real parked-registry, ADK and HTTP path. Changing
// the project toggle/default must not change an existing worker's chosen model,
// original write provenance, or its planning-only tool restriction.
func TestNuwaParkedWorkerRestoresOriginalModelAndModeAtHTTPBoundary(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "shared-write", true: "read-only"}[readOnly], func(t *testing.T) {
			var calls atomic.Int32
			var e *Engine
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
				if request.Model != "executor/original" {
					t.Errorf("recovery silently changed binding to %q", request.Model)
				}
				available := map[string]bool{}
				for _, item := range request.Tools {
					available[item.Function.Name] = true
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
			e.cfg.Swarm.AutoTitle = false
			e.cfg.Models.Default = "restore-fixture"
			e.cfg.Models.Providers = []config.Provider{{ID: "restore-fixture", BaseURL: endpoint.URL,
				Model: "planner/current", Catalog: []string{"planner/original", "planner/current", "executor/original"}}}
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
			turn := &store.Turn{ThreadID: thread.ID, ProjectID: project.ID, ProviderID: "restore-fixture", Model: "planner/original", NuwaEnabled: true, NuwaReadOnly: readOnly}
			if err := e.store.CreateTurn(turn); err != nil {
				t.Fatal(err)
			}
			if err := e.store.FinishTurn(turn.ID, store.TurnDone, "Waiting for the next check.", ""); err != nil {
				t.Fatal(err)
			}
			latest := &store.Turn{ThreadID: thread.ID, ProjectID: project.ID, ProviderID: "restore-fixture", Model: "planner/current", NuwaEnabled: true}
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
			binding := &swarm.ModelBinding{Ref: provider.ChoiceID("restore-fixture", "executor/original"), ProviderID: "restore-fixture", Model: "executor/original"}
			appendParkedCheckpoint(t, e, thread.ID, latest.ID, restartCheckpoint{Version: 1, Workers: []swarm.WorkerCheckpoint{{
				ID: "observer-4", Role: "observer", Generation: 3, ConfigTag: turn.ID, DelegationEnabled: &enabled, ModelBinding: binding,
				Task: "Inspect the supplied evidence.", History: []*schema.Message{schema.UserMessage("Inspect the supplied evidence.")},
			}}})
			disabled := false
			if _, err := e.UpdateProject(project.ID, nil, nil, nil, nil, &disabled); err != nil {
				t.Fatal(err)
			}
			next := restartedTestEngine(t, e)
			if n, err := next.ResumeParkedWorkers(); err != nil || n != 1 {
				t.Fatalf("cold worker recovery=%d err=%v", n, err)
			}
			waitRestartState(t, func() bool { return next.ActiveWork() == 0 })
			_, finished := next.workersFromThread(thread.ID)
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
		})
	}
}

func TestNuwaTaggedConfigurationRejectsMissingOrMovedOwner(t *testing.T) {
	e := newTestEngine(t)
	project, thread, turn := nuwaValidationSource(t, e, "Original owner")
	turn.ProjectID = project.ID
	if err := e.store.DB().Model(turn).Update("project_id", project.ID).Error; err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ thread, tag, reason string }{
		{thread.ID, "missing-turn", "not found"},
		{"different-thread", turn.ID, "another conversation"},
	} {
		if _, err := e.workerConfigurationForTag(t.Context(), item.thread, item.tag); err == nil || !strings.Contains(err.Error(), item.reason) {
			t.Fatalf("owner refusal=%v want %q", err, item.reason)
		}
	}
	if err := e.store.DB().Model(thread).Update("project_id", "different-project").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := e.workerConfigurationForTag(t.Context(), thread.ID, turn.ID); err == nil || !strings.Contains(err.Error(), "owner changed") {
		t.Fatalf("moved original source accepted: %v", err)
	}
	if err := e.store.DB().Model(thread).Update("project_id", project.ID).Error; err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
	defer cancel()
	<-cancelled.Done()
	if _, err := e.workerConfigurationForTag(cancelled, thread.ID, turn.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled configuration recovery=%v", err)
	}
}

func TestNuwaManagerRecoveryKeepsOriginalPlanningRestriction(t *testing.T) {
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, request scheduleCompletionRequest) {
		available := map[string]bool{}
		for _, item := range request.Tools {
			available[item.Function.Name] = true
		}
		if !available["list_models"] || !available["read_work"] || available["record_work"] || available["exec"] || available["write_file"] {
			t.Errorf("resumed planning manager gained mutation tools: %+v", available)
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "The proposed work is ready for review."}, "stop")
	})
	project, thread, turn := nuwaValidationSource(t, e, "Planning recovery")
	if err := e.store.DB().Model(turn).Updates(map[string]any{"project_id": project.ID, "nuwa_read_only": true, "model": "offline-scheduled-completion"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := e.store.AppendMessages(thread.ID, turn.ID, []store.Message{{Role: "user", Content: "Inspect and propose the next work."}}); err != nil {
		t.Fatal(err)
	}
	if n, err := e.ResumeOrphanedTurns(); n != 1 || err != nil {
		t.Fatalf("planning manager resume=%d %v", n, err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("planning manager recovery failed: %+v", got)
	}
	waitSettled(t, e, thread.ID)
}
