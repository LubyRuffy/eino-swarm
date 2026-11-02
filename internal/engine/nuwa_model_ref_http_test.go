package engine

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A copied catalog reference must survive model-authored JSON arguments and
// reach the selected HTTP model, while durable binding keeps its old identity.
func TestNuwaPlainCatalogRefSelectsHTTPWorkerAndKeepsCanonicalBinding(t *testing.T) {
	const providerID, managerModel, workerModel = "catalog-fixture", "planner/default", "executor/selected"
	canonicalRef := provider.ChoiceID(providerID, workerModel)
	plainRef := "model:" + base64.RawURLEncoding.EncodeToString([]byte(canonicalRef))
	var workerCalls atomic.Int32
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
		if request.Model == workerModel {
			workerCalls.Add(1)
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The assigned evidence was inspected."}, "stop")
			return
		}
		if request.Model != managerModel {
			t.Errorf("unexpected model request %q", request.Model)
		}
		if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		catalogReceipt := nuwaValidationReceipt(request.scheduleCompletionRequest, "catalog-ref-list")
		if catalogReceipt == "" {
			nuwaValidationTool(w, request.scheduleCompletionRequest, "catalog-ref-list", "list_models", `{}`)
			return
		}
		spawnReceipt := nuwaValidationReceipt(request.scheduleCompletionRequest, "catalog-ref-spawn")
		if spawnReceipt == "" {
			var catalog struct {
				Models []nuwaModel `json:"models"`
			}
			if err := json.Unmarshal([]byte(catalogReceipt), &catalog); err != nil {
				t.Errorf("decode actual catalog: %v", err)
			}
			copiedRef := ""
			for _, option := range catalog.Models {
				if option.ProviderID == providerID && option.Model == workerModel {
					copiedRef = option.Ref
				}
			}
			if copiedRef != plainRef || strings.ContainsAny(copiedRef, "\t\r\n\\ ") {
				t.Errorf("catalog ref cannot be copied as printable tool arguments: %q", copiedRef)
				// Still exercise the actual safe-handle selection on the old
				// implementation rather than stopping at the catalog assertion.
				copiedRef = plainRef
			}
			args, err := json.Marshal(map[string]any{"role": "catalog-reviewer", "task": "Inspect the supplied evidence.", "model_ref": copiedRef})
			if err != nil {
				t.Error(err)
			}
			nuwaValidationTool(w, request.scheduleCompletionRequest, "catalog-ref-spawn", "spawn_agent", string(args))
			return
		}
		var spawned struct {
			AgentID string `json:"agent_id"`
		}
		if err := json.Unmarshal([]byte(spawnReceipt), &spawned); err != nil || spawned.AgentID == "" {
			t.Errorf("copied catalog reference was refused: %s (decode=%v)", spawnReceipt, err)
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The delegation was refused."}, "stop")
			return
		}
		if nuwaValidationReceipt(request.scheduleCompletionRequest, "catalog-ref-wait") == "" {
			nuwaValidationTool(w, request.scheduleCompletionRequest, "catalog-ref-wait", "wait_agents", fmt.Sprintf(`{"agent_ids":[%q],"timeout_s":5}`, spawned.AgentID))
			return
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "The selected worker result was collected."}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Models = config.ModelsConfig{Default: providerID, Providers: []config.Provider{{ID: providerID, BaseURL: endpoint.URL, Model: managerModel, Catalog: []string{managerModel, workerModel}}}}
	e.pool = provider.New(e.cfg)
	project, err := e.CreateProject("Printable model reference", "", "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Explicit catalog selection", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Delegate inspection to a model copied from the catalog.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || workerCalls.Load() != 1 {
		t.Fatalf("printable catalog ref did not execute the selected model: turn=%+v worker_calls=%d", got, workerCalls.Load())
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	bindings := 0
	finished := false
	for _, event := range events {
		if event.Kind == swarm.NotifySpawned.String() {
			if event.ModelBinding == nil || event.ModelBinding.Ref != canonicalRef || event.ModelBinding.ProviderID != providerID || event.ModelBinding.Model != workerModel {
				t.Fatalf("model choice lost canonical durable binding: %+v", event)
			}
			bindings++
		}
		finished = finished || event.Kind == swarm.NotifyFinished.String()
	}
	if bindings != 1 || !finished {
		t.Fatalf("missing lifecycle receipts: binding=%d finished=%v", bindings, finished)
	}
	view, err := e.ProjectNuwaSnapshot(t.Context(), project.ID)
	if err != nil || len(view.Workers) != 1 || view.Workers[0].Status != "done" || view.Workers[0].ModelBinding == nil || view.Workers[0].ModelBinding.Ref != canonicalRef {
		t.Fatalf("completed worker lost its canonical binding: %+v err=%v", view.Workers, err)
	}
}

func TestNuwaModelSelectionAcceptsCanonicalRefWithoutDecodingUnknownHandles(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.Models = config.ModelsConfig{Default: "catalog-fixture", Providers: []config.Provider{{ID: "catalog-fixture", BaseURL: "http://127.0.0.1:1", Model: "available-model"}}}
	e.pool = provider.New(e.cfg)
	_, thread, turn := nuwaValidationSource(t, e, "Canonical reference compatibility")
	turn.ProviderID, turn.Model = "catalog-fixture", "available-model"
	set, err := e.conversationTools(t.Context(), thread)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := e.projectContextFor(thread)
	if err != nil {
		t.Fatal(err)
	}
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	e.wireNuwaRegistry(reg, set, pc, turn)
	selectModel := reg.WorkerConfiguration().SelectWorkerModel
	canonical := provider.ChoiceID(turn.ProviderID, turn.Model)
	binding, builder, err := selectModel(t.Context(), canonical)
	if err != nil || builder == nil || binding.Ref != canonical {
		t.Fatalf("canonical persisted ref no longer accepted: %+v err=%v", binding, err)
	}
	for _, unknown := range []string{
		"model:" + base64.RawURLEncoding.EncodeToString([]byte(provider.ChoiceID(turn.ProviderID, "unlisted-model"))),
		"model:" + base64.URLEncoding.EncodeToString([]byte(canonical)),
		strings.ReplaceAll(canonical, "\t", `\t`),
		turn.Model,
	} {
		if _, _, err := selectModel(t.Context(), unknown); err == nil {
			t.Fatalf("noncatalog reference was decoded or guessed: %q", unknown)
		}
	}
}
