package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

func nuwaPairCatalog(model, executor string) config.ModelsConfig {
	return config.ModelsConfig{Default: "pair", Providers: []config.Provider{{
		ID: "pair", BaseURL: "http://127.0.0.1", Model: model,
		Catalog: []string{model, executor},
	}}}
}

func usePairCatalog(e *Engine, endpoint string, model, executor string, mock bool) {
	catalog := nuwaPairCatalog(model, executor)
	catalog.Providers[0].BaseURL = endpoint
	e.cfg.Models = catalog
	if mock {
		e.pool = provider.NewMock(e.cfg)
		return
	}
	e.pool = provider.New(e.cfg)
}

// A saved pair is the manager for the next turn, even when the conversation
// itself still names another ready model. The executor is copied onto the
// turn so a later project edit cannot retarget it.
func TestNuwaTurnUsesSavedPlannerAndFreezesExecutor(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle, e.cfg.Memory.Enabled = false, false
	usePairCatalog(e, "", "planner-model", "executor-model", true)
	project, err := e.CreateProject("Chosen models", "", t.TempDir(), false, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.SaveNuwaModelPair(project.ID, NuwaModelPair{
		PlannerProviderID: "pair", PlannerModel: "planner-model",
		ExecutorProviderID: "pair", ExecutorModel: "executor-model",
	}); err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Chosen models", "pair", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.UpdateThread(thread.ID, map[string]any{"model": "executor-model"}); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Continue the assigned work.")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Interrupt(thread.ID) })
	stored, err := e.store.GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ProviderID != "pair" || stored.Model != "planner-model" {
		t.Fatalf("turn used the conversation model %s/%s", stored.ProviderID, stored.Model)
	}
	if stored.NuwaExecutorProviderID != "pair" || stored.NuwaExecutorModel != "executor-model" {
		t.Fatalf("turn did not freeze the executor: %+v", stored)
	}
}

func TestNuwaTurnRefusesUnavailableExecutorBeforeAModelCall(t *testing.T) {
	var calls int
	endpoint := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls++
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle, e.cfg.Memory.Enabled = false, false
	usePairCatalog(e, endpoint.URL, "planner-model", "executor-model", false)
	project, err := e.CreateProject("Missing executor", "", t.TempDir(), false, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.SaveNuwaModelPair(project.ID, NuwaModelPair{
		PlannerProviderID: "pair", PlannerModel: "planner-model",
		ExecutorProviderID: "pair", ExecutorModel: "executor-model",
	}); err != nil {
		t.Fatal(err)
	}
	e.cfg.Models.Providers[0].Catalog = []string{"planner-model"}
	e.cfg.Models.Providers[0].Model = "planner-model"
	e.pool = provider.New(e.cfg)
	thread, err := e.CreateThread("Missing executor", "pair", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartTurn(thread.ID, "Continue the assigned work."); !errors.Is(err, ErrNuwaModelUnavailable) {
		t.Fatalf("missing executor err=%v", err)
	}
	if calls != 0 {
		t.Fatalf("unavailable executor made %d model requests", calls)
	}
	turns, err := e.store.ListTurns(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 0 {
		t.Fatalf("refused turn was stored: %+v", turns)
	}
}

func TestOrdinaryTurnIgnoresSavedNuwaModels(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle, e.cfg.Memory.Enabled = false, false
	usePairCatalog(e, "", "planner-model", "executor-model", true)
	project, err := e.CreateProject("Ordinary", "", t.TempDir(), false, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.SaveNuwaModelPair(project.ID, NuwaModelPair{
		PlannerProviderID: "pair", PlannerModel: "planner-model",
		ExecutorProviderID: "pair", ExecutorModel: "executor-model",
	}); err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Ordinary", "pair", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.UpdateThread(thread.ID, map[string]any{"model": "executor-model"}); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Continue the assigned work.")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Interrupt(thread.ID) })
	stored, err := e.store.GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.NuwaEnabled || stored.Model != "executor-model" || stored.NuwaExecutorModel != "" {
		t.Fatalf("ordinary turn applied the Nuwa pair: model=%s executor=%s nuwa=%v", stored.Model, stored.NuwaExecutorModel, stored.NuwaEnabled)
	}
}

func TestNuwaSavedExecutorOwnsNewRolesAndResumeKeepsItsBinding(t *testing.T) {
	var mu sync.Mutex
	var models []string
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		mu.Lock()
		models = append(models, request.Model)
		mu.Unlock()
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "The assigned result is available."}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	_, reg, turn := newModelRequirementRegistry(t, endpoint.URL, true, true, false)
	turn.NuwaExecutorProviderID = turn.ProviderID
	turn.NuwaExecutorModel = "require-executor"
	out := invokeModelRequirementTool(t, reg, "spawn_agent", `{"role":"reference-reader","task":"Inspect the assigned evidence.","fork_context":true}`)
	if strings.Contains(out, `"error"`) {
		t.Fatalf("omitted model_ref did not use the saved executor: %s", out)
	}
	collectModelRequirementWorker(t, reg, out)
	mu.Lock()
	got := append([]string(nil), models...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "require-executor" {
		t.Fatalf("worker models=%v", got)
	}
	conflict := fmt.Sprintf(`{"role":"other-reader","task":"Inspect the assigned evidence.","model_ref":%q,"fork_context":true}`, nuwaModelRef(turn.ProviderID, "require-planner"))
	refused := invokeModelRequirementTool(t, reg, "spawn_agent", conflict)
	if !strings.Contains(refused, "this project already chose the executor model") || !strings.Contains(refused, `"error"`) {
		t.Fatalf("conflicting model_ref was accepted: %s", refused)
	}
	if len(reg.SnapshotWorkers()) != 1 {
		t.Fatalf("conflicting ref created a worker: %d", len(reg.SnapshotWorkers()))
	}
	turn.NuwaExecutorModel = "require-planner"
	again := invokeModelRequirementTool(t, reg, "spawn_agent", `{"role":"reference-reader","task":"Inspect the follow-up evidence."}`)
	if strings.Contains(again, `"error"`) {
		t.Fatalf("same-role resume refused: %s", again)
	}
	collectModelRequirementWorker(t, reg, again)
	mu.Lock()
	got = append([]string(nil), models...)
	mu.Unlock()
	if len(got) != 2 || got[0] != "require-executor" || got[1] != "require-executor" {
		t.Fatalf("resume followed the new executor: %v", got)
	}
}

func TestNuwaSingleSavedModelUsesThatModel(t *testing.T) {
	var model string
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		model = request.Model
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "The assigned result is available."}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	_, reg, turn := newModelRequirementRegistry(t, endpoint.URL, true, false, false)
	turn.NuwaExecutorProviderID = turn.ProviderID
	turn.NuwaExecutorModel = turn.Model
	out := invokeModelRequirementTool(t, reg, "spawn_agent", `{"role":"reference-reader","task":"Inspect the assigned evidence.","fork_context":true}`)
	collectModelRequirementWorker(t, reg, out)
	if model != turn.Model {
		t.Fatalf("single saved model requested %q", model)
	}
}

func TestOrdinarySpawnIgnoresASavedExecutor(t *testing.T) {
	var model string
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		model = request.Model
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "The assigned result is available."}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	_, reg, turn := newModelRequirementRegistry(t, endpoint.URL, false, true, false)
	turn.NuwaExecutorProviderID = turn.ProviderID
	turn.NuwaExecutorModel = "require-executor"
	out := invokeModelRequirementTool(t, reg, "spawn_agent", `{"role":"reference-reader","task":"Inspect the assigned evidence.","fork_context":true}`)
	collectModelRequirementWorker(t, reg, out)
	if model != "require-planner" {
		t.Fatalf("ordinary worker used %q", model)
	}
}
