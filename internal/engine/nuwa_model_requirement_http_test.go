package engine

import (
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/cloudwego/eino/components/tool"
)

// Use the engine's real wiring, so a helper-only selector check cannot hide a
// missing Registry/ADK boundary or a default-model HTTP request.
func newModelRequirementRegistry(t *testing.T, endpoint string, nuwa, multiple, unready bool) (*Engine, *swarm.Registry, *store.Turn) {
	t.Helper()
	const account, planner, executor = "require-fixture", "require-planner", "require-executor"
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle, e.cfg.Swarm.ComposerPredictions = false, false
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	catalog := []string{planner}
	if multiple {
		catalog = append(catalog, executor)
	}
	e.cfg.Models = config.ModelsConfig{Default: account, Providers: []config.Provider{{ID: account, BaseURL: endpoint, Model: planner, Catalog: catalog}}}
	if unready {
		e.cfg.Models.Providers = append(e.cfg.Models.Providers, config.Provider{ID: "unavailable-resource", Model: "unavailable-model"})
	}
	e.pool = provider.New(e.cfg)
	project, err := e.CreateProject("Explicit worker model admission", "", t.TempDir(), false, nuwa)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Worker model admission boundary", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: thread.ID, ProjectID: project.ID, UserText: "Inspect the assigned evidence.", ProviderID: account, Model: planner, NuwaEnabled: nuwa}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	set, err := e.conversationTools(t.Context(), thread)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := e.projectContextFor(thread)
	if err != nil {
		t.Fatal(err)
	}
	builder, err := e.pool.ModelBuilder(t.Context(), account, planner, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	reg := e.newTurnRegistry(builder, set, pc, turn)
	rt := e.runtimeFor(thread.ID)
	_, release := ownManagerTestFixture(t.Context(), rt, turn.ID, reg)
	rt.mu.Lock()
	owned := rt.running && rt.turnID == turn.ID && rt.reg == reg
	rt.mu.Unlock()
	if !owned {
		reg.Close()
		release()
		t.Fatal("Registry HTTP fixture does not own the actual turn")
	}
	t.Cleanup(func() {
		reg.Cleanup()
		if err := reg.Wait(context.Background(), 5*time.Second); err != nil {
			t.Error(err)
		}
		reg.Close()
		release()
	})
	return e, reg, turn
}

func invokeModelRequirementTool(t *testing.T, reg *swarm.Registry, name, args string) string {
	t.Helper()
	for _, candidate := range reg.Tools() {
		info, err := candidate.Info(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if info.Name != name {
			continue
		}
		if name == "spawn_agent" {
			params, err := info.ParamsOneOf.ToJSONSchema()
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(params)
			if err != nil {
				t.Fatal(err)
			}
			var shape struct {
				Required []string `json:"required"`
			}
			if err := json.Unmarshal(encoded, &shape); err != nil {
				t.Fatal(err)
			}
			for _, field := range shape.Required {
				if field == "model_ref" {
					t.Error("schema unconditionally requires model_ref and prevents same-role reuse")
				}
			}
		}
		out, err := candidate.(tool.InvokableTool).InvokableRun(t.Context(), args)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	t.Fatalf("missing actual Registry tool %s", name)
	return ""
}

func collectModelRequirementWorker(t *testing.T, reg *swarm.Registry, receipt string) string {
	t.Helper()
	var spawned toolScopeWorkerID
	if err := json.Unmarshal([]byte(receipt), &spawned); err != nil || spawned.AgentID == "" {
		t.Fatalf("worker receipt=%s err=%v", receipt, err)
	}
	out := invokeModelRequirementTool(t, reg, "wait_agents", fmt.Sprintf(`{"agent_ids":[%q],"timeout_s":5}`, spawned.AgentID))
	var waited toolScopeWait
	if err := json.Unmarshal([]byte(out), &waited); err != nil || waited.TimedOut || len(waited.Agents) != 1 || waited.Agents[0].Status != "done" {
		t.Fatalf("actual worker not collected: %s err=%v", out, err)
	}
	return spawned.AgentID
}

func TestNuwaNewRoleRequiresModelRefOnlyWithMultipleReadyModelsAtRegistryHTTP(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		nuwa, multiple, unready, refuse bool
	}{
		{"nuwa-multiple", true, true, false, true},
		{"nuwa-single", true, false, false, false},
		{"ordinary-multiple", false, true, false, false},
		{"nuwa-one-ready-and-one-unavailable", true, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request scheduleCompletionRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					http.Error(w, "invalid fixture request", http.StatusBadRequest)
					return
				}
				calls.Add(1)
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The assigned result is available."}, "stop")
			}))
			t.Cleanup(endpoint.Close)
			e, reg, turn := newModelRequirementRegistry(t, endpoint.URL, tc.nuwa, tc.multiple, tc.unready)
			wantReady := 1
			if tc.multiple {
				wantReady = 2
			}
			if got := len(e.nuwaModels(turn)); got != wantReady {
				t.Fatalf("fixture ready models=%d want=%d", got, wantReady)
			}
			out := invokeModelRequirementTool(t, reg, "spawn_agent", `{"role":"reference-reader","task":"Inspect the assigned evidence.","fork_context":true}`)
			if !tc.refuse {
				collectModelRequirementWorker(t, reg, out)
				if calls.Load() != 1 {
					t.Fatalf("compatible default made %d HTTP requests", calls.Load())
				}
				return
			}
			if !strings.Contains(out, `"error"`) {
				collectModelRequirementWorker(t, reg, out)
				t.Errorf("missing reference created a default worker: %s", out)
			}
			if !strings.Contains(out, "model_ref") || !strings.Contains(out, "list_models") {
				t.Errorf("refusal lacks exact catalog correction guidance: %s", out)
			}
			if len(reg.SnapshotWorkers()) != 0 || calls.Load() != 0 {
				t.Errorf("refusal had worker/HTTP side effects: workers=%d requests=%d", len(reg.SnapshotWorkers()), calls.Load())
				return // Old implementation is already a semantic RED; all actors are joined.
			}
			args := fmt.Sprintf(`{"role":"reference-reader","task":"Inspect the assigned evidence.","model_ref":%q}`, nuwaModelRef(turn.ProviderID, "require-executor"))
			id := collectModelRequirementWorker(t, reg, invokeModelRequirementTool(t, reg, "spawn_agent", args))
			if id != "reference-reader-1" {
				t.Errorf("refusal consumed a worker identity: %s", id)
			}
			reg.Stats() // Existing archived roles must retain their pinned model.
			again := invokeModelRequirementTool(t, reg, "spawn_agent", `{"role":"reference-reader","task":"Inspect the follow-up evidence."}`)
			if got := collectModelRequirementWorker(t, reg, again); got != id {
				t.Errorf("same role changed identity: %s", got)
			}
			for _, cp := range reg.SnapshotWorkers() {
				if cp.ID != id || cp.ModelBinding == nil || cp.ModelBinding.Model != "require-executor" || cp.Generation != 2 {
					t.Errorf("same-role omission changed the actual binding/generation: %+v", cp)
				}
			}
			if calls.Load() != 2 {
				t.Errorf("corrected spawn and reuse made %d HTTP requests", calls.Load())
			}
		})
	}
}

// A restored tagged worker keeps its own binding. Its new child must still
// resolve a model explicitly through the native engine selector rebuilt from
// the actual stored turn. This is not process replacement or native acceptance.
func TestNuwaRestoredWorkerCannotDefaultNewChildBeforeActualHTTP(t *testing.T) {
	parentTask, childTask := store.NewID("retained_task_"), store.NewID("new_child_task_")
	var parentCalls, childCalls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		for _, message := range request.Messages {
			if message.Role == "system" && strings.Contains(message.Content, childTask) {
				childCalls.Add(1)
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The unexpected child finished."}, "stop")
				return
			}
		}
		parentCalls.Add(1)
		receipt := nuwaValidationReceipt(request, "restored-new-child")
		if receipt == "" {
			args, _ := json.Marshal(map[string]any{"role": "new-child", "task": childTask, "fork_context": true})
			nuwaValidationTool(w, request, "restored-new-child", "spawn_agent", string(args))
			return
		}
		var result struct {
			Error   string `json:"error"`
			AgentID string `json:"agent_id"`
		}
		if err := json.Unmarshal([]byte(receipt), &result); err != nil {
			t.Error(err)
		}
		if result.AgentID != "" {
			t.Error("restored worker bypassed explicit selection for a new child")
			if nuwaValidationReceipt(request, "restored-child-wait") == "" {
				nuwaValidationTool(w, request, "restored-child-wait", "wait_agents", fmt.Sprintf(`{"agent_ids":[%q],"timeout_s":5}`, result.AgentID))
				return
			}
		} else if !strings.Contains(result.Error, "model_ref") || !strings.Contains(result.Error, "list_models") {
			t.Errorf("restored worker did not receive actionable model selection refusal: %s", receipt)
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "The retained worker reported its result."}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	_, reg, turn := newModelRequirementRegistry(t, endpoint.URL, true, true, false)
	legacy := reg.WorkerConfiguration()
	if legacy.Tag != turn.ID || reg.ResolveWorkerConfig == nil {
		t.Fatal("restoration must use the real stored turn and native engine resolver")
	}
	binding, _, err := legacy.SelectWorkerModel(t.Context(), nuwaModelRef(turn.ProviderID, turn.Model))
	if err != nil {
		t.Fatal(err)
	}
	retained, err := reg.Restore(t.Context(), swarm.RestoredWorker{ID: "retained-parent-7", Role: "retained-parent", Task: parentTask,
		ConfigTag: legacy.Tag, ModelBinding: &binding, Generation: 3}, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-retained.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("restored parent did not finish")
	}
	if err := reg.Wait(t.Context(), 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err, done := retained.Result(); !done || err != nil {
		t.Fatalf("retained worker result done=%v err=%v", done, err)
	}
	if parentCalls.Load() != 2 || childCalls.Load() != 0 || len(reg.SnapshotWorkers()) != 1 {
		t.Fatalf("restored admission side effects: parent=%d child=%d workers=%d", parentCalls.Load(), childCalls.Load(), len(reg.SnapshotWorkers()))
	}
	cp := reg.SnapshotWorkers()[0]
	if cp.ID != retained.ID || cp.Generation != 3 || cp.ModelBinding == nil || cp.ModelBinding.Model != turn.Model {
		t.Fatalf("retained identity/model changed: %+v", cp)
	}
}
