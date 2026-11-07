package server_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/app"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/remote"
	"github.com/LubyRuffy/eino-swarm/internal/server"
)

const (
	accountToken    = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	accountSourceID = "gateway:0123456789abcdef0123456789abcdef"
)

// accountHarness is an app with no model service of its own, signed in to a
// gateway fixture that serves one account catalog. Mock is off: a scripted
// pool ignores account models by design.
type accountHarness struct {
	*harness
	gateway *httptest.Server
	catalog string
}

func newAccountHarness(t *testing.T, signedIn bool) *accountHarness {
	t.Helper()
	for _, name := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL", "ZWAI_MODEL_BASE_URL", "ZWAI_MODEL_API_KEY", "ZWAI_MODEL_NAME"} {
		t.Setenv(name, "")
	}
	ah := &accountHarness{catalog: `{"object":"list","data":[{"id":"server-model","context_length":64000},{"id":"server-model-2"}]}`}
	var origin string
	ah.gateway = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client/desktop/v1/bootstrap":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"user": map[string]any{"id": strings.Repeat("2", 32), "username": "account", "display_name": "Account Name"},
				"models": map[string]any{
					"source_id": accountSourceID, "source_kind": "gateway", "label": "Server", "execution": "desktop",
					"api_base_url": origin + "/client/desktop/v1", "catalog_url": origin + "/client/desktop/v1/models", "api_access_enabled": true,
				},
			}})
		case "/client/desktop/v1/models":
			_, _ = io.WriteString(w, ah.catalog)
		default:
			http.NotFound(w, r)
		}
	}))
	origin = ah.gateway.URL
	t.Cleanup(ah.gateway.Close)

	a, err := app.New(app.Options{DataDir: t.TempDir(), Mode: server.ModeWeb, Version: "test", NoAssets: true})
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	a.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := a.Config.Update(func(c *config.Config) error {
		c.Remote.AuthMode = config.RemoteAuthAccount
		c.Remote.HubURL = origin
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(a.Server.Handler())
	t.Cleanup(func() {
		ts.Close()
		a.Remote.Stop()
		a.Engine.Shutdown()
		_ = a.Store.Close()
	})
	ah.harness = &harness{t: t, app: a, srv: ts}
	// app.New already started the host's background work, and a login from the
	// UI triggers a refresh itself; a credential written to disk does not.
	if signedIn {
		ah.signIn()
		a.Remote.Reload()
	}
	return ah
}

func (ah *accountHarness) signIn() {
	ah.t.Helper()
	cfg := ah.app.Config
	path, err := cfg.StageRemoteToken(accountToken, ah.gateway.URL, "test-public-key")
	if err != nil {
		ah.t.Fatal(err)
	}
	err = cfg.ActivateRemoteToken(path, config.RemoteCredential{
		Token: accountToken, Origin: ah.gateway.URL, Authorized: true,
		HostID: strings.Repeat("1", 32), UserID: strings.Repeat("2", 32), Username: "account", DisplayName: "Account Name",
	})
	if err != nil {
		ah.t.Fatal(err)
	}
}

func (ah *accountHarness) waitAccount(state string) {
	ah.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ah.app.Remote.AccountModels().State == state {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	ah.t.Fatalf("account models never reached %q: %+v", state, ah.app.Remote.AccountModels())
}

func (ah *accountHarness) raw(path string) string {
	ah.t.Helper()
	resp := ah.do(http.MethodGet, path, nil)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return string(raw)
}

// A fresh install with no model service of its own is not "unconfigured" once
// the signed-in server supplies models: the setup banner must not nag someone
// who can already chat.
func TestMetaCountsReadyAccountModelsAsConfigured(t *testing.T) {
	ah := newAccountHarness(t, false)
	ah.waitAccount(remote.AccountSignedOut)
	got := ah.json(http.MethodGet, "/api/meta", nil, http.StatusOK)
	if got["configured"] != false {
		t.Fatalf("no model service and no account must read as unconfigured: %v", got)
	}
	state, _ := got["account_models"].(map[string]any)
	if state["state"] != remote.AccountSignedOut {
		t.Fatalf("account_models=%v", got["account_models"])
	}

	ah.signIn()
	ah.app.Remote.Reload()
	ah.waitAccount(remote.AccountReady)
	got = ah.json(http.MethodGet, "/api/meta", nil, http.StatusOK)
	if got["configured"] != true {
		t.Fatalf("a ready account supplies models, so setup is done: %v", got)
	}
	state, _ = got["account_models"].(map[string]any)
	if state["state"] != remote.AccountReady || state["models"] != float64(2) || state["username"] != "account" {
		t.Fatalf("account_models=%v", got["account_models"])
	}
	if got["default_provider"] != accountSourceID {
		t.Fatalf("new conversations start on the account's models: %v", got["default_provider"])
	}
}

// The composer lists the account's models first and a new conversation starts
// on them. The credential is used inside Go only: no response and no file on
// disk may contain it.
func TestAccountModelsLeadTheListAndNeverLeakTheCredential(t *testing.T) {
	ah := newAccountHarness(t, true)
	ah.waitAccount(remote.AccountReady)

	got := ah.json(http.MethodGet, "/api/models", nil, http.StatusOK)
	rows, _ := got["models"].([]any)
	if len(rows) < 2 {
		t.Fatalf("models=%v", got)
	}
	first, _ := rows[0].(map[string]any)
	if first["source"] != "account" || first["provider_id"] != accountSourceID || first["model"] != "server-model" || first["default"] != true {
		t.Fatalf("the account's models must come first and be the default: %v", first)
	}
	if got["default"] != accountSourceID {
		t.Fatalf("default=%v", got["default"])
	}

	for _, path := range []string{"/api/models", "/api/meta", "/api/settings", "/api/remote/status"} {
		if body := ah.raw(path); strings.Contains(body, accountToken) {
			t.Fatalf("%s leaked the account credential", path)
		}
	}
	yaml, err := os.ReadFile(ah.app.Config.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(yaml), accountToken) || strings.Contains(string(yaml), accountSourceID) {
		t.Fatalf("account models were written to config.yaml:\n%s", yaml)
	}
}

// Choosing "my own service first" must hold across every later save of the
// model list, which does not carry the field.
func TestSettingsKeepTheChosenDefaultSource(t *testing.T) {
	ah := newAccountHarness(t, true)
	ah.waitAccount(remote.AccountReady)
	provider := map[string]any{"id": "default", "label": "Main", "base_url": "http://endpoint.invalid/v1", "model": "m", "api_key": "k", "timeout_seconds": 60}
	save := func(extra map[string]any) map[string]any {
		models := map[string]any{"default": "default", "providers": []map[string]any{provider}}
		for k, v := range extra {
			models[k] = v
		}
		return ah.json(http.MethodPut, "/api/settings", map[string]any{"models": models}, http.StatusOK)
	}
	source := func(settings map[string]any) any {
		return settings["settings"].(map[string]any)["models"].(map[string]any)["default_source"]
	}

	if got := source(save(nil)); got != config.DefaultSourceAccount {
		t.Fatalf("a save that does not mention the source must keep it, got %v", got)
	}
	if got := source(save(map[string]any{"default_source": "provider"})); got != config.DefaultSourceProvider {
		t.Fatalf("explicit choice was not stored: %v", got)
	}
	if got := source(save(nil)); got != config.DefaultSourceProvider {
		t.Fatalf("a later unrelated save reset the choice: %v", got)
	}
	meta := ah.json(http.MethodGet, "/api/meta", nil, http.StatusOK)
	if meta["default_provider"] != "default" {
		t.Fatalf("with provider first, new conversations use it: %v", meta["default_provider"])
	}
	ah.json(http.MethodPut, "/api/settings", map[string]any{"models": map[string]any{
		"default": "default", "default_source": "elsewhere", "providers": []map[string]any{provider},
	}}, http.StatusBadRequest)
}

// A language change must not bring the welcome back, and dismissing it must
// not reset the language.
func TestWelcomeDismissalIsIndependentOfOtherUISettings(t *testing.T) {
	ah := newAccountHarness(t, false)
	dismissed := func() bool {
		ui := ah.json(http.MethodGet, "/api/meta", nil, http.StatusOK)["ui"].(map[string]any)
		return ui["welcome_dismissed"] == true
	}
	if dismissed() {
		t.Fatal("a fresh install has not dismissed the welcome")
	}
	ah.json(http.MethodPut, "/api/settings", map[string]any{"ui": map[string]any{"welcome_dismissed": true}}, http.StatusOK)
	if !dismissed() {
		t.Fatal("dismissal was not stored")
	}
	ah.json(http.MethodPut, "/api/settings", map[string]any{"ui": map[string]any{"locale": "zh"}}, http.StatusOK)
	if !dismissed() {
		t.Fatal("a language-only save brought the welcome back")
	}
	ah.json(http.MethodPut, "/api/settings", map[string]any{"ui": map[string]any{"welcome_dismissed": false}}, http.StatusOK)
	if dismissed() {
		t.Fatal("an explicit false must clear it")
	}
	if got := ah.json(http.MethodGet, "/api/meta", nil, http.StatusOK)["locale"]; got != "zh" {
		t.Fatalf("dismissal reset the language: %v", got)
	}
}
