package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-tools/read"
	"github.com/cloudwego/eino/schema"
)

const (
	recoveryProjectProvider = "ownership-fixture"
	recoveryProjectModel    = "original-model"
	recoveryWorkerID        = "auditor-4"
	recoveryWorkerTask      = "Inspect the project evidence."
)

func ownershipRecoveryConfig(t *testing.T, endpoint string) *config.Config {
	t.Helper()
	clearTestEngineSeed(t)
	cfg := testEngineConfig(t, t.TempDir())
	cfg.Swarm.AutoTitle = false
	cfg.Models.Default = recoveryProjectProvider
	cfg.Models.Providers = []config.Provider{{
		ID: recoveryProjectProvider, BaseURL: endpoint,
		Model: "current-model", Catalog: []string{"current-model", recoveryProjectModel},
	}}
	return cfg
}

func openOwnershipRecoveryEngine(t *testing.T, cfg *config.Config) (*Engine, func()) {
	t.Helper()
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	e := New(cfg, st, provider.New(cfg), slog.New(slog.NewTextHandler(io.Discard, nil)))
	stop := sync.OnceFunc(func() {
		e.Shutdown()
		auditEventLog(t, st)
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	t.Cleanup(stop)
	return e, stop
}

func ownershipRecoveryTurn(t *testing.T, e *Engine, projectID string, nuwa, legacy bool) (*store.Thread, *store.Turn) {
	t.Helper()
	thread, err := e.CreateThread("Project ownership recovery", recoveryProjectProvider, projectID)
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{
		ThreadID: thread.ID, ProjectID: projectID, UserText: recoveryWorkerTask,
		ProviderID: recoveryProjectProvider, Model: recoveryProjectModel,
		NuwaEnabled: nuwa, NuwaReadOnly: nuwa,
	}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if legacy {
		// An older engine omitted the nullable column, leaving SQL NULL.
		if err := e.store.DB().Exec("UPDATE turns SET project_id = NULL WHERE id = ?", turn.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	return thread, turn
}

func parkOwnershipRecoveryWorker(t *testing.T, e *Engine, turn *store.Turn, tag string) {
	t.Helper()
	if err := e.store.FinishTurn(turn.ID, store.TurnDone, "Awaiting the next check.", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: turn.ThreadID, Title: "Worker ownership", Prompt: "Inspect progress.",
		EveryS: 3600, CreatedBy: store.ScheduleCreatedManager,
	}); err != nil {
		t.Fatal(err)
	}
	appendOwnershipRecoveryWorker(t, e, turn, tag)
}

func appendOwnershipRecoveryWorker(t *testing.T, e *Engine, turn *store.Turn, tag string) {
	t.Helper()
	appendParkedCheckpoint(t, e, turn.ThreadID, turn.ID, restartCheckpoint{
		Version: 1, Workers: []swarm.WorkerCheckpoint{{
			ID: recoveryWorkerID, Role: "auditor", Generation: 2, ConfigTag: tag,
			Task: recoveryWorkerTask, History: []*schema.Message{schema.UserMessage(recoveryWorkerTask)},
		}},
	})
}

func ownershipToolNames(request scheduleCompletionRequest) []string {
	names := make([]string, 0, len(request.Tools))
	for _, item := range request.Tools {
		names = append(names, item.Function.Name)
	}
	slices.Sort(names)
	return names
}

// The first replacement inherits a legacy configuration and saves a tag for
// it. The second must resolve that exact turn and still execute its tools in
// the original project with the original model.
func TestLegacyProjectWorkerRestoresBeforeAndAfterCheckpointGainsTag(t *testing.T) {
	const firstEvidence = "Original project workspace evidence."
	const secondEvidence = "Updated project workspace evidence."
	var requests atomic.Int32
	firstRead := make(chan []string, 1)
	secondRead := make(chan []string, 1)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			scheduleCompletionRequest
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		if request.Model != recoveryProjectModel {
			t.Errorf("recovered worker model=%q, want %q", request.Model, recoveryProjectModel)
		}
		names := ownershipToolNames(request.scheduleCompletionRequest)
		if !slices.Contains(names, read.ToolName) || slices.Contains(names, "read_work") || slices.Contains(names, "spawn_agent") {
			t.Errorf("worker lost its ordinary tool surface: %v", names)
		}
		step := requests.Add(1)
		switch step {
		case 1, 3:
			writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": fmt.Sprintf("read-project-evidence-%d", step), "type": "function",
				"function": map[string]any{"name": read.ToolName, "arguments": `{"file_path":"evidence.txt"}`},
			}}}, "tool_calls")
		case 2, 4:
			expected := firstEvidence
			if step == 4 {
				expected = secondEvidence
			}
			if !slices.ContainsFunc(request.Messages, func(msg *schema.Message) bool {
				return msg.Role == schema.Tool && strings.Contains(msg.Content, expected)
			}) {
				t.Errorf("worker did not read the project workspace on request %d", step)
			}
			if step == 2 {
				firstRead <- names
				<-r.Context().Done()
				return
			}
			secondRead <- names
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Project evidence inspected."}, "stop")
		default:
			t.Errorf("unexpected model request %d", step)
			http.Error(w, "unexpected fixture request", http.StatusBadRequest)
		}
	}))
	t.Cleanup(endpoint.Close)
	cfg := ownershipRecoveryConfig(t, endpoint.URL)
	old, closeOld := openOwnershipRecoveryEngine(t, cfg)
	project, err := old.CreateProject("Project with legacy work", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	thread, turn := ownershipRecoveryTurn(t, old, project.ID, false, true)
	path := filepath.Join(old.WorkspaceDir(thread.ID), "evidence.txt")
	if err := os.WriteFile(path, []byte(firstEvidence), 0o600); err != nil {
		t.Fatal(err)
	}
	parkOwnershipRecoveryWorker(t, old, turn, "")
	closeOld()

	first, closeFirst := openOwnershipRecoveryEngine(t, cfg)
	if err := first.Restore(t.Context(), nil); err != nil {
		t.Fatalf("first startup with untagged worker: %v", err)
	}
	var firstTools []string
	select {
	case firstTools = <-firstRead:
	case <-time.After(5 * time.Second):
		t.Fatal("first restored worker did not reach the model with project evidence")
	}
	rt := first.runtimeFor(thread.ID)
	rt.mu.Lock()
	reg := rt.parked
	rt.mu.Unlock()
	if reg == nil {
		t.Fatal("first startup did not park the restored worker")
	}
	pending := reg.PendingAgents()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	status, err := first.PrepareRestart(ctx)
	cancel()
	if err != nil || !status.Ready || status.Checkpointed != 1 {
		t.Fatalf("first replacement handoff=%+v err=%v", status, err)
	}
	ctx, cancel = context.WithTimeout(t.Context(), 5*time.Second)
	err = reg.WaitPendingCompletion(ctx, pending)
	cancel()
	if err != nil {
		t.Fatalf("join first worker after handoff: %v", err)
	}
	cp := first.checkpointForTurn(turn.ID)
	if cp == nil || len(cp.Workers) != 1 || cp.Workers[0].Finished || cp.Workers[0].ConfigTag != turn.ID ||
		cp.Workers[0].ID != recoveryWorkerID || cp.Workers[0].Generation != 2 {
		t.Fatalf("handoff did not preserve the tagged live worker: %+v", cp)
	}
	closeFirst()

	if err := os.WriteFile(path, []byte(secondEvidence), 0o600); err != nil {
		t.Fatal(err)
	}
	second, _ := openOwnershipRecoveryEngine(t, cfg)
	if err := second.Restore(t.Context(), nil); err != nil {
		t.Fatalf("second startup with tagged worker: %v", err)
	}
	select {
	case secondTools := <-secondRead:
		if !slices.Equal(firstTools, secondTools) {
			t.Fatalf("tagged worker tool binding changed: first=%v second=%v", firstTools, secondTools)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second restored worker did not reach the model with project evidence")
	}
	waitRestartState(t, func() bool { return second.ActiveWork() == 0 })
	_, finished := second.workersFromThread(thread.ID)
	if len(finished) != 1 || finished[0].ID != recoveryWorkerID || finished[0].Generation != 2 ||
		finished[0].Err != nil || finished[0].ConfigTag != turn.ID || finished[0].Result != "Project evidence inspected." {
		t.Fatalf("second startup did not finish the original worker: %+v", finished)
	}
	turns, err := second.store.ListTurns(thread.ID)
	if err != nil || len(turns) != 1 || turns[0].ProjectID != project.ID || turns[0].Status != store.TurnDone {
		t.Fatalf("project or original manager turn changed: %+v err=%v", turns, err)
	}
}

func TestNuwaResumeRestoresLegacyProjectOwnerAndPlanningTools(t *testing.T) {
	var calls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		calls.Add(1)
		names := ownershipToolNames(request)
		if !slices.Contains(names, "read_work") || !slices.Contains(names, "list_models") ||
			slices.Contains(names, "record_work") || slices.Contains(names, "exec") {
			t.Errorf("legacy Nuwa manager lost its planning-only surface: %v", names)
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "Ready for review."}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	cfg := ownershipRecoveryConfig(t, endpoint.URL)
	old, closeOld := openOwnershipRecoveryEngine(t, cfg)
	project, err := old.CreateProject("Original Nuwa owner", "", "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	thread, turn := ownershipRecoveryTurn(t, old, project.ID, true, true)
	if err := old.store.AppendMessages(thread.ID, turn.ID, []store.Message{{Role: "user", Content: recoveryWorkerTask}}); err != nil {
		t.Fatal(err)
	}
	closeOld()
	next, _ := openOwnershipRecoveryEngine(t, cfg)
	if err := next.Restore(t.Context(), nil); err != nil {
		t.Fatalf("startup refused legacy Nuwa turn in its original project: %v", err)
	}
	if got := waitForTurn(t, next, turn.ID); got.Status != store.TurnDone || got.ProjectID != project.ID || calls.Load() != 1 {
		t.Fatalf("Nuwa recovery did not finish in the original project: %+v calls=%d", got, calls.Load())
	}
	waitSettled(t, next, thread.ID)
}

func TestProjectMovesRefuseRecordedOwnershipBeforeRestoreClaim(t *testing.T) {
	for _, mode := range []string{"parked worker", "running turn with worker", "running Nuwa turn"} {
		for _, fromProject := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/from-project=%t", mode, fromProject), func(t *testing.T) {
				var calls atomic.Int32
				endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					calls.Add(1)
					http.Error(w, "moved project reached the model", http.StatusBadRequest)
				}))
				t.Cleanup(endpoint.Close)
				cfg := ownershipRecoveryConfig(t, endpoint.URL)
				old, closeOld := openOwnershipRecoveryEngine(t, cfg)
				projectA, err := old.CreateProject("Project A", "", "", false, true)
				if err != nil {
					t.Fatal(err)
				}
				projectB, err := old.CreateProject("Project B", "", "", false, true)
				if err != nil {
					t.Fatal(err)
				}
				origin := ""
				if fromProject {
					origin = projectA.ID
				}
				thread, turn := ownershipRecoveryTurn(t, old, origin, mode == "running Nuwa turn", false)
				switch mode {
				case "parked worker":
					parkOwnershipRecoveryWorker(t, old, turn, turn.ID)
				case "running turn with worker":
					appendOwnershipRecoveryWorker(t, old, turn, turn.ID)
				}
				if err := old.MoveThread(thread.ID, projectB.ID); err != nil {
					t.Fatal(err)
				}
				closeOld()

				next, _ := openOwnershipRecoveryEngine(t, cfg)
				err = next.Restore(t.Context(), nil)
				var failures *RestoreFailures
				if !errors.As(err, &failures) || failures.Failed != 1 || !strings.Contains(err.Error(), "project owner changed") {
					t.Fatalf("recorded owner change was not reported: %v", err)
				}
				if st := next.Status(thread.ID); st.Running || st.Workers != 0 || st.TurnID != "" || calls.Load() != 0 {
					t.Fatalf("owner change was refused after claim: %+v calls=%d", st, calls.Load())
				}
				events, err := next.store.ListTurnEvents(turn.ID)
				if err != nil {
					t.Fatal(err)
				}
				for _, ev := range events {
					if ev.Kind == swarm.NotifySpawned.String() || ev.Kind == KindCleanup {
						t.Fatalf("owner change mutated the worker roster: %+v", ev)
					}
				}
			})
		}
	}
}
