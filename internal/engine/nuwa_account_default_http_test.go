package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A new conversation must use the selected source before its manager chooses a
// worker. Equal model names at both endpoints expose an accidental route change.
func TestNewNuwaConversationUsesChosenAccountBeforeDelegatingHTTPWorker(t *testing.T) {
	const accountID = "gateway:0123456789abcdef0123456789abcdef"
	const privateID = "private-fixture"
	const managerModel = "shared-planner-name"
	const workerModel = "independent-executor"
	const accountToken = "inert-account-fixture-token"
	const privateToken = "inert-private-fixture-token"
	var accountManagerCalls, privateManagerCalls, workerCalls atomic.Int32
	var collected atomic.Bool

	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			scheduleCompletionRequest
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode actual model request: %v", err)
			http.Error(w, "invalid fixture input", http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/account/v1/chat/completions":
			if r.Header.Get("Authorization") != "Bearer "+accountToken || request.Model != managerModel {
				t.Errorf("account manager route or credential differs: model=%q", request.Model)
			}
			accountManagerCalls.Add(1)
		case "/private/v1/chat/completions":
			if r.Header.Get("Authorization") != "Bearer "+privateToken {
				t.Error("private endpoint received another source's credential")
			}
			if request.Model == workerModel {
				workerCalls.Add(1)
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The independent evidence was inspected."}, "stop")
				return
			}
			if request.Model != managerModel {
				t.Errorf("unexpected private model %q", request.Model)
			}
			privateManagerCalls.Add(1)
		default:
			t.Errorf("unexpected model path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		catalogReceipt := nuwaValidationReceipt(request.scheduleCompletionRequest, "account-default-catalog")
		if catalogReceipt == "" {
			nuwaValidationTool(w, request.scheduleCompletionRequest, "account-default-catalog", "list_models", `{}`)
			return
		}
		if strings.Contains(catalogReceipt, accountToken) || strings.Contains(catalogReceipt, privateToken) {
			t.Error("delegation catalog disclosed an endpoint credential")
		}
		spawnReceipt := nuwaValidationReceipt(request.scheduleCompletionRequest, "account-default-spawn")
		if spawnReceipt == "" {
			var catalog struct {
				Models []nuwaModel `json:"models"`
			}
			if err := json.Unmarshal([]byte(catalogReceipt), &catalog); err != nil {
				t.Errorf("decode actual delegation catalog: %v", err)
				http.Error(w, "invalid catalog receipt", http.StatusBadRequest)
				return
			}
			selected := ""
			accountListed := false
			for _, option := range catalog.Models {
				accountListed = accountListed || option.ProviderID == accountID && option.Model == managerModel
				if option.ProviderID == privateID && option.Model == workerModel {
					selected = option.Ref
				}
			}
			if !accountListed || selected == "" {
				t.Error("actual catalog must contain the account manager and the independent private worker")
				http.Error(w, "missing model resources", http.StatusBadRequest)
				return
			}
			args, err := json.Marshal(map[string]any{
				"role": "independent-review", "task": "Inspect the assigned evidence.", "model_ref": selected,
			})
			if err != nil {
				t.Errorf("encode selected worker: %v", err)
				http.Error(w, "invalid selection", http.StatusBadRequest)
				return
			}
			nuwaValidationTool(w, request.scheduleCompletionRequest, "account-default-spawn", "spawn_agent", string(args))
			return
		}
		var spawned struct {
			AgentID string `json:"agent_id"`
		}
		if err := json.Unmarshal([]byte(spawnReceipt), &spawned); err != nil || spawned.AgentID == "" {
			t.Errorf("actual worker spawn refused: %s, decode=%v", spawnReceipt, err)
			http.Error(w, "worker not started", http.StatusBadRequest)
			return
		}
		waitReceipt := nuwaValidationReceipt(request.scheduleCompletionRequest, "account-default-wait")
		if waitReceipt == "" {
			nuwaValidationTool(w, request.scheduleCompletionRequest, "account-default-wait", "wait_agents",
				fmt.Sprintf(`{"agent_ids":[%q],"timeout_s":5}`, spawned.AgentID))
			return
		}
		var waited struct {
			Agents []struct {
				AgentID    string `json:"agent_id"`
				Generation int    `json:"generation"`
				Status     string `json:"status"`
			} `json:"agents"`
		}
		if err := json.Unmarshal([]byte(waitReceipt), &waited); err != nil || len(waited.Agents) != 1 || waited.Agents[0].AgentID != spawned.AgentID || waited.Agents[0].Status != "done" || waited.Agents[0].Generation != 1 {
			t.Errorf("actual independent result was not collected: %s, decode=%v", waitReceipt, err)
		} else {
			collected.Store(true)
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "The selected independent result was collected."}, "stop")
	}))
	t.Cleanup(endpoint.Close)

	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Models = config.ModelsConfig{
		Default: privateID, DefaultSource: config.DefaultSourceAccount,
		Providers: []config.Provider{{ID: privateID, BaseURL: endpoint.URL + "/private/v1", APIKey: privateToken,
			Model: managerModel, Catalog: []string{managerModel, workerModel}}},
	}
	e.pool = provider.New(e.cfg)
	account, err := provider.NewAccountSource(accountID, "Account fixture", endpoint.URL+"/account/v1", accountToken,
		provider.Catalog{Names: []string{managerModel}})
	if err != nil {
		t.Fatal(err)
	}
	e.pool.SetAccountSource(account)
	if got := e.pool.DefaultProviderID(); got != accountID {
		t.Fatalf("selected account did not become the effective default: %q", got)
	}
	project, err := e.CreateProject("Account manager with independent evidence", "", "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Selected source for new work", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	storedThread, err := e.store.GetThread(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if thread.ProviderID != accountID || storedThread.ProviderID != accountID {
		t.Errorf("new conversation bypassed its chosen account source: returned=%q stored=%q want=%q", thread.ProviderID, storedThread.ProviderID, accountID)
	}
	turn, err := e.StartTurn(thread.ID, "Delegate the independent evidence using an exact model catalog reference.")
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if finished.Status != store.TurnDone || !finished.NuwaEnabled || finished.ProviderID != accountID || finished.Model != managerModel {
		t.Errorf("actual completed Nuwa turn used another source: status=%q provider=%q model=%q nuwa=%v", finished.Status, finished.ProviderID, finished.Model, finished.NuwaEnabled)
	}
	if accountManagerCalls.Load() == 0 || privateManagerCalls.Load() != 0 || workerCalls.Load() != 1 || !collected.Load() {
		t.Errorf("actual HTTP routes or collection differ: account_manager=%d private_manager=%d worker=%d collected=%v", accountManagerCalls.Load(), privateManagerCalls.Load(), workerCalls.Load(), collected.Load())
	}
	view, err := e.ProjectNuwaSnapshot(t.Context(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Workers) != 1 || view.Workers[0].Status != "done" || view.Workers[0].Generation != 1 || view.Workers[0].ModelBinding == nil || view.Workers[0].ModelBinding.ProviderID != privateID || view.Workers[0].ModelBinding.Model != workerModel {
		t.Errorf("completed worker lost its independently selected source binding: %+v", view.Workers)
	}
}

// Source preferences choose defaults. Explicit choices and signed-out failures
// must keep the identity a caller already selected.
func TestNewConversationPreservesExplicitSourceAndSignedOutFallback(t *testing.T) {
	const accountID = "gateway:0123456789abcdef0123456789abcdef"
	const privateID = "private-fixture"
	endpoint := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Models = config.ModelsConfig{Default: privateID, Providers: []config.Provider{
		{ID: privateID, BaseURL: endpoint.URL, Model: "fixture-model"},
	}}
	e.pool = provider.New(e.cfg)
	account, err := provider.NewAccountSource(accountID, "Account fixture", endpoint.URL, "inert-token",
		provider.Catalog{Names: []string{"fixture-model"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, source, selected string
		signedOut              bool
		want                   string
		wantErr                error
	}{
		{name: "provider preference", source: config.DefaultSourceProvider, want: privateID},
		{name: "explicit private choice", source: config.DefaultSourceAccount, selected: privateID, want: privateID},
		{name: "signed out default", source: config.DefaultSourceAccount, signedOut: true, want: privateID},
		{name: "signed out explicit account", source: config.DefaultSourceAccount, selected: accountID, signedOut: true, wantErr: provider.ErrAccountLoginRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e.cfg.Models.DefaultSource = tc.source
			if tc.signedOut {
				e.pool.SetAccountSource(nil)
			} else {
				e.pool.SetAccountSource(account)
			}
			thread, err := e.CreateThread(tc.name, tc.selected, "")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || thread != nil {
					t.Fatalf("explicit account choice silently changed: thread=%+v err=%v", thread, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if thread.ProviderID != tc.want {
				t.Errorf("conversation source=%q want=%q", thread.ProviderID, tc.want)
			}
		})
	}
}
