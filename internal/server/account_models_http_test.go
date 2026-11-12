package server_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	gateway     *httptest.Server
	catalog     string
	profile     any
	catalogHits atomic.Int32
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
			models := map[string]any{
				"source_id": accountSourceID, "source_kind": "gateway", "label": "Server", "execution": "desktop",
				"api_base_url": origin + "/client/desktop/v1", "catalog_url": origin + "/client/desktop/v1/models", "api_access_enabled": true,
			}
			if ah.profile != nil {
				models["profile"] = ah.profile
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"user":   map[string]any{"id": strings.Repeat("2", 32), "username": "account", "display_name": "Account Name"},
				"models": models,
			}})
		case "/client/desktop/v1/models":
			ah.catalogHits.Add(1)
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

// The server row remembers which conversation model a new chat starts on.
// A later save that does not mention it keeps the name. Embedding names are
// not a conversation default.
func TestSettingsChooseTheAccountConversationModel(t *testing.T) {
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
	accountModel := func(settings map[string]any) any {
		return settings["settings"].(map[string]any)["models"].(map[string]any)["account_model"]
	}
	if got := accountModel(save(map[string]any{"account_model": "server-model-2"})); got != "server-model-2" {
		t.Fatalf("stored account model %v", got)
	}
	if got := accountModel(save(nil)); got != "server-model-2" {
		t.Fatalf("a later save cleared the account model: %v", got)
	}
	body := ah.json(http.MethodGet, "/api/models", nil, http.StatusOK)
	var chosen, other bool
	for _, row := range body["models"].([]any) {
		item := row.(map[string]any)
		if item["source"] != "account" {
			continue
		}
		switch item["model"] {
		case "server-model-2":
			chosen = item["default"] == true
		case "server-model":
			other = item["default"] == true
		}
	}
	if !chosen || other {
		t.Fatalf("composer default did not follow the saved name: %v", body["models"])
	}
	meta := ah.json(http.MethodGet, "/api/meta", nil, http.StatusOK)
	kinds := meta["account_models"].(map[string]any)["kinds"].(map[string]any)
	if kinds["unclassified"] != float64(2) || kinds["embedding"] != float64(0) {
		t.Fatalf("kinds %v", kinds)
	}
	ah.json(http.MethodPut, "/api/settings", map[string]any{"models": map[string]any{
		"default": "default", "account_model": strings.Repeat("m", 257), "providers": []map[string]any{provider},
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

func TestSettingsKeepTheAccountPresentation(t *testing.T) {
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
	presentation := func(settings map[string]any) any {
		return settings["settings"].(map[string]any)["models"].(map[string]any)["account_presentation"]
	}
	if got := presentation(save(nil)); got != config.AccountPresentationOfficial {
		t.Fatalf("absent presentation = %v", got)
	}
	if strings.Contains(string(mustRead(t, ah.app.Config.Path())), "account_presentation") {
		t.Fatal("an omitted presentation was written into the config file")
	}
	if got := presentation(save(map[string]any{"account_presentation": "full"})); got != config.AccountPresentationFull {
		t.Fatalf("explicit full presentation = %v", got)
	}
	if got := presentation(save(nil)); got != config.AccountPresentationFull {
		t.Fatalf("a later save reset the presentation: %v", got)
	}
	ah.json(http.MethodPut, "/api/settings", map[string]any{"models": map[string]any{
		"default": "default", "account_presentation": "somewhere", "providers": []map[string]any{provider},
	}}, http.StatusBadRequest)
}

func TestOfficialProfileHidesTheCatalogAcrossTheAPI(t *testing.T) {
	ah := newAccountHarness(t, false)
	ah.catalog = `{"object":"list","data":[{"id":"vendor-secret-model"}]}`
	ah.profile = map[string]any{
		"label": "Partner model", "instruction": "profile guidance marker 1842",
		"tools": []string{"read", "exec"}, "context_window": 32000,
	}
	ah.signIn()
	ah.app.Remote.Reload()
	ah.waitAccount(remote.AccountReady)
	if ah.catalogHits.Load() != 0 {
		t.Fatalf("a profile still fetched the catalog %d times", ah.catalogHits.Load())
	}
	models := ah.raw("/api/models")
	meta := ah.raw("/api/meta")
	for _, body := range []string{models, meta} {
		if strings.Contains(body, "vendor-secret-model") || strings.Contains(body, "profile guidance marker 1842") {
			t.Fatalf("official response leaked a vendor id or the instruction: %s", body)
		}
	}
	got := ah.json(http.MethodGet, "/api/models", nil, http.StatusOK)
	rows, _ := got["models"].([]any)
	if len(rows) != 1 {
		t.Fatalf("models = %v", got["models"])
	}
	row := rows[0].(map[string]any)
	if row["model"] != config.OfficialModelAlias || row["label"] != "Partner model" || row["source"] != "account" {
		t.Fatalf("official row = %v", row)
	}
	account := ah.json(http.MethodGet, "/api/meta", nil, http.StatusOK)["account_models"].(map[string]any)
	profile, _ := account["profile"].(map[string]any)
	if account["models"] != float64(1) || account["label"] != "Partner model" || profile["presentation"] != "official" || profile["instruction"] != true {
		t.Fatalf("account meta = %v", account)
	}
	tools, _ := profile["tools"].([]any)
	if profile["restrict_tools"] != true || len(tools) != 2 {
		t.Fatalf("profile tools = %v", profile)
	}

	provider := map[string]any{"id": "default", "label": "Main", "base_url": "http://endpoint.invalid/v1", "model": "own-model", "api_key": "k", "timeout_seconds": 60}
	ah.json(http.MethodPut, "/api/settings", map[string]any{"models": map[string]any{
		"default": "default", "account_presentation": "full", "providers": []map[string]any{provider},
	}}, http.StatusOK)
	full := ah.raw("/api/models")
	if strings.Contains(full, "vendor-secret-model") || !strings.Contains(full, "own-model") || !strings.Contains(full, config.OfficialModelAlias) {
		t.Fatalf("full list = %s", full)
	}
	account = ah.json(http.MethodGet, "/api/meta", nil, http.StatusOK)["account_models"].(map[string]any)
	if account["profile"].(map[string]any)["presentation"] != "full" {
		t.Fatalf("presentation did not follow settings: %v", account["profile"])
	}

	if err := os.Remove(filepath.Join(ah.app.Config.RemoteDir(), "host_token")); err != nil {
		t.Fatal(err)
	}
	ah.app.Remote.Reload()
	ah.waitAccount(remote.AccountSignedOut)
	signedOut := ah.raw("/api/meta")
	if strings.Contains(signedOut, "Partner model") || strings.Contains(signedOut, "profile guidance marker 1842") {
		t.Fatalf("sign-out kept the profile: %s", signedOut)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
