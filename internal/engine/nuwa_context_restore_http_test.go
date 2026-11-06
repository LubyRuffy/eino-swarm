package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// A parked worker restores its original Nuwa configuration, while its board
// must come from current project state rather than the checkpoint's age or
// the latest ordinary manager turn. No model call elects to read shared work.
func TestNuwaSharedContextRestoresOriginalWorkerWithCurrentGraphAtActualHTTP(t *testing.T) {
	const account, planner, replacement, executor = "context-restore", "planner/original", "planner/current", "executor/original"
	const workerID, workerGeneration = "context-observer-4", 3
	const catalogCall = "context-restored-catalog"
	var calls atomic.Int32
	var source, corrected, foreign store.NuwaNode
	var project *store.Project
	var editor *store.Thread
	var editTurn *store.Turn
	var correctedRevision int64
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
		if wire.Model != executor {
			t.Errorf("cold recovery replaced the bound worker with %q", wire.Model)
		}
		view := nuwaContextHTTPBlock(t, request, true)
		current := nuwaContextHTTPNode(t, view, source.ID)
		if view.ProjectID != project.ID || view.Revision < correctedRevision || current.Version != 2 ||
			current.Summary != corrected.Summary || current.Status != "reported" || current.Detail != "" {
			t.Errorf("restored HTTP input used stale or unverified shared state: view=%+v node=%+v", view, current)
		}
		if current.ThreadID != source.ThreadID || current.TurnID != source.TurnID ||
			current.UpdatedThreadID != editor.ID || current.UpdatedTurnID != editTurn.ID {
			t.Errorf("recovered context lost original versus correction provenance: %+v", current)
		}
		if strings.Contains(joinedContent(request.Messages), foreign.ID) || strings.Contains(joinedContent(request.Messages), source.Detail) {
			t.Error("recovery disclosed foreign graph data or an on-demand detail")
		}
		available := map[string]bool{}
		for _, offered := range request.Tools {
			available[offered.Function.Name] = true
			if strings.HasPrefix(offered.Function.Name, "mcp_") {
				t.Error("context-only recovery fixture unexpectedly mounted MCP")
			}
		}
		if !available["list_models"] || !available["spawn_agent"] || !available["record_work"] {
			t.Errorf("latest ordinary turn replaced the original Nuwa worker surface: %+v", available)
		}
		for _, message := range request.Messages {
			for _, call := range message.ToolCalls {
				if call.Function.Name == "read_work" {
					t.Error("recovered graph fixture depended on an explicit read_work call")
				}
			}
		}
		switch calls.Add(1) {
		case 1:
			nuwaValidationTool(w, request, catalogCall, "list_models", `{}`)
			return
		case 2:
			if receipt := nuwaValidationReceipt(request, catalogCall); receipt == "" || !strings.Contains(receipt, executor) {
				t.Error("restored worker did not execute its actual coordination tool")
			}
		default:
			t.Error("unexpected extra cold-recovery model request")
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "Recovered context was inspected."}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle, e.cfg.Memory.AutoReview = false, false
	e.cfg.MCP.Servers = nil
	e.cfg.Models = config.ModelsConfig{Default: account, Providers: []config.Provider{{
		ID: account, BaseURL: endpoint.URL, Model: planner, Catalog: []string{planner, replacement, executor},
	}}}
	e.pool = provider.New(e.cfg)
	var sourceThread *store.Thread
	var sourceTurn *store.Turn
	project, sourceThread, sourceTurn = nuwaContextHTTPSource(t, e, "Cold worker shared state")
	source = nuwaValidationNode(t, e, project, sourceThread, sourceTurn, store.NewID("reported_"), store.NewID("detail_"))
	thread, err := e.CreateThread("Explicit parked context recovery", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	const task = "Inspect the current shared state and report the outcome."
	original := &store.Turn{ThreadID: thread.ID, ProjectID: project.ID, ProviderID: account, Model: planner, NuwaEnabled: true}
	if err := e.store.CreateTurn(original); err != nil {
		t.Fatal(err)
	}
	if err := e.store.AppendMessages(thread.ID, original.ID, []store.Message{{Role: string(schema.User), Content: task, Source: store.MessageSourceHuman}}); err != nil {
		t.Fatal(err)
	}
	if err := e.store.FinishTurn(original.ID, store.TurnDone, "Waiting for the next check.", ""); err != nil {
		t.Fatal(err)
	}
	latest := &store.Turn{ThreadID: thread.ID, ProjectID: project.ID, ProviderID: account, Model: replacement, NuwaEnabled: false}
	if err := e.store.CreateTurn(latest); err != nil {
		t.Fatal(err)
	}
	if err := e.store.FinishTurn(latest.ID, store.TurnDone, "The original worker remains active.", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: thread.ID, Title: "Context recovery ownership", Prompt: "Inspect progress", EveryS: 3600, CreatedBy: store.ScheduleCreatedManager}); err != nil {
		t.Fatal(err)
	}
	enabled := true
	binding := &swarm.ModelBinding{Ref: provider.ChoiceID(account, executor), ProviderID: account, Model: executor}
	appendParkedCheckpoint(t, e, thread.ID, latest.ID, restartCheckpoint{Version: 1, Workers: []swarm.WorkerCheckpoint{{
		ID: workerID, Role: "context-observer", Generation: workerGeneration, ConfigTag: original.ID,
		DelegationEnabled: &enabled, ModelBinding: binding, Task: task, History: []*schema.Message{schema.UserMessage(task)},
	}}})
	disabled := false
	if _, err := e.UpdateProject(project.ID, nil, nil, nil, nil, &disabled); err != nil {
		t.Fatal(err)
	}
	e.cfg.Models.Providers[0].Model = replacement
	e.pool = provider.New(e.cfg)
	// Commit after the checkpoint, under a different trusted writer. Restoring
	// a serialized board or using the latest ordinary turn loses this change.
	editor, err = e.CreateThread("Explicit post-checkpoint editor", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	editTurn = &store.Turn{ThreadID: editor.ID, ProjectID: project.ID, NuwaEnabled: true}
	if err := e.store.CreateTurn(editTurn); err != nil {
		t.Fatal(err)
	}
	if err := e.store.FinishTurn(editTurn.ID, store.TurnDone, "", ""); err != nil {
		t.Fatal(err)
	}
	corrected = source
	corrected.ThreadID, corrected.TurnID, corrected.Summary = editor.ID, editTurn.ID, store.NewID("correction_")
	if err := e.store.UpsertNuwaNode(t.Context(), &corrected, source.Version); err != nil {
		t.Fatal(err)
	}
	if corrected.Version != 2 {
		t.Fatal("fixture did not commit the post-checkpoint correction")
	}
	correctedRevision, _, err = e.store.NuwaSnapshot(t.Context(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	foreignProject, foreignThread, foreignTurn := nuwaContextHTTPSource(t, e, "Unrelated recovery state")
	foreign = nuwaValidationNode(t, e, foreignProject, foreignThread, foreignTurn, store.NewID("foreign_"), "")
	next := restartedTestEngine(t, e)
	if count, err := next.ResumeParkedWorkers(); err != nil || count != 1 {
		t.Fatalf("cold worker recovery=%d err=%v", count, err)
	}
	waitRestartState(t, func() bool { return next.ActiveWork() == 0 })
	_, finished := next.workersFromThread(thread.ID)
	if calls.Load() != 2 || len(finished) != 1 || finished[0].ID != workerID || finished[0].Generation != workerGeneration ||
		finished[0].Err != nil || finished[0].ConfigTag != original.ID || finished[0].ModelBinding == nil || *finished[0].ModelBinding != *binding {
		t.Fatalf("recovered graph lost worker identity or selected model: calls=%d workers=%+v", calls.Load(), finished)
	}
	assertNuwaContextNotDurable(t, next, thread.ID, task)
}
