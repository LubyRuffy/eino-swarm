package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/server"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/LubyRuffy/pairlink/crypto"
)

func TestNOSECSidebarLoginEnablesOnlyAccountConnectionAfterValidIntent(t *testing.T) {
	h := newHarness(t)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "not approved", http.StatusConflict) }))
	defer gateway.Close()
	if err := h.app.Config.Update(func(next *config.Config) error {
		next.Remote.HubURL = gateway.URL
		next.Remote.DisplayName = "private workstation"
		next.Remote.ThreadLimit = 17
		next.Remote.KeepAwake = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := h.app.Config.Snapshot().Remote
	service, err := server.New(server.Options{Engine: h.app.Engine, Mode: server.ModeDesktop, OpenURL: func(string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	service.SetRemote(h.app.Remote)
	local := httptest.NewServer(service.Handler())
	defer local.Close()
	defer h.app.Remote.Stop()
	for _, body := range []string{`{"enable_account":true,"extra":true}`, `{"enable_account":true} {}`, `{"enable_account":true,"new_device":true}`, `{"enable_account":true,"new_device":true,"confirmed":false}`} {
		response, err := local.Client().Post(local.URL+"/api/remote/login", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest || h.app.Config.Snapshot().Remote != before {
			t.Fatal("invalid intent changed connection settings")
		}
	}
	// Without the explicit opt-in the original preconfigured-only contract stays.
	response, err := local.Client().Post(local.URL+"/api/remote/login", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("unconfigured login status %d", response.StatusCode)
	}
	response, err = local.Client().Post(local.URL+"/api/remote/login", "application/json", strings.NewReader(`{"enable_account":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("sidebar login status %d", response.StatusCode)
	}
	want := before
	want.Enabled, want.AuthMode = true, config.RemoteAuthNOSEC
	if h.app.Config.Snapshot().Remote != want {
		t.Fatal("account enable overwrote unrelated remote settings")
	}
	reloaded, err := config.Load(h.app.Config.DataDir())
	if err != nil || reloaded.Remote != want {
		t.Fatalf("account configuration was not persisted: %v", err)
	}
}

func TestNOSECSidebarLoginCannotChangeConfigWithoutBrowserOrPersistence(t *testing.T) {
	h := newHarness(t)
	before := h.app.Config.Snapshot().Remote
	h.json(http.MethodPost, "/api/remote/login", map[string]any{"enable_account": true}, http.StatusNotImplemented)
	if h.app.Config.Snapshot().Remote != before {
		t.Fatal("unavailable browser changed config")
	}
	service, err := server.New(server.Options{Engine: h.app.Engine, OpenURL: func(string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	service.SetRemote(h.app.Remote)
	local := httptest.NewServer(service.Handler())
	defer local.Close()
	if err := os.Remove(h.app.Config.Path()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(h.app.Config.Path(), 0700); err != nil {
		t.Fatal(err)
	}
	response, err := local.Client().Post(local.URL+"/api/remote/login", "application/json", strings.NewReader(`{"enable_account":true}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusInternalServerError || h.app.Config.Snapshot().Remote != before {
		t.Fatal("failed persistence changed live config")
	}
}

func TestNOSECLogoutUsesStrictIntentAndRemovesCloudAuthority(t *testing.T) {
	h := newHarness(t)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"data":{"logged_out":true}}`) }))
	defer gateway.Close()
	if err := h.app.Config.Update(func(next *config.Config) error {
		next.Remote.AuthMode = config.RemoteAuthNOSEC
		next.Remote.Enabled = true
		next.Remote.KeepAwake = false
		next.Remote.HubURL = gateway.URL
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cfg := h.app.Config
	token := strings.Repeat("a", 64)
	path, err := cfg.StageRemoteToken(token, cfg.Snapshot().Remote.HubURL, "current-public")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ActivateRemoteToken(path, config.RemoteCredential{Token: token, Origin: cfg.Snapshot().Remote.HubURL, Authorized: true, HostID: strings.Repeat("b", 32), UserID: strings.Repeat("c", 32), Username: "account"}); err != nil {
		t.Fatal(err)
	}
	if tools.DescribeWebExecution(cfg).CloudAccount != "signed_in" {
		t.Fatal("fixture lacks cloud authority")
	}
	for _, body := range []string{`null`, `{"token":"secret"}`, `{} {}`, `{`, `{"unknown":"` + strings.Repeat("x", 2048) + `"}`} {
		response, err := h.srv.Client().Post(h.srv.URL+"/api/remote/logout", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest || !cfg.HasHostToken() {
			t.Fatalf("unsafe logout status %d", response.StatusCode)
		}
	}
	response, err := h.srv.Client().Post(h.srv.URL+"/api/remote/logout", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		HasToken bool `json:"has_token"`
		Online   bool `json:"online"`
		Login    struct{ State string }
	}
	if json.Unmarshal(raw, &got) != nil || response.StatusCode != http.StatusOK || got.HasToken || got.Online || got.Login.State != "signed_out" {
		t.Fatalf("logout did not return signed-out safe status: %d %s", response.StatusCode, raw)
	}
	if strings.Contains(string(raw), token) {
		t.Fatal("logout leaked credential")
	}
	if tools.DescribeWebExecution(cfg).CloudAccount != "sign_in_required" {
		t.Fatal("logout retained cloud authority")
	}
}

func TestNOSECLogoutRefusesLegacyAndReportsCredentialFailure(t *testing.T) {
	h := newHarness(t)
	if err := h.app.Config.WriteHostToken("anonymous-private-token"); err != nil {
		t.Fatal(err)
	}
	h.json(http.MethodPost, "/api/remote/logout", map[string]any{}, http.StatusConflict)
	if !h.app.Config.HasHostToken() {
		t.Fatal("account logout destroyed anonymous token")
	}
	if err := h.app.Config.Update(func(next *config.Config) error { next.Remote.AuthMode = config.RemoteAuthNOSEC; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.app.Config.RemoteDir()+"/host_token", []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	got := h.json(http.MethodPost, "/api/remote/logout", map[string]any{}, http.StatusInternalServerError)
	if got["code"] != "remote_logout_failed" {
		t.Fatalf("unclassified logout failure: %v", got)
	}
	status := h.json(http.MethodGet, "/api/remote/status", nil, http.StatusOK)
	if status["login"].(map[string]any)["state"] == "signed_out" {
		t.Fatal("failed cleanup claimed signed out")
	}
}

func installHTTPLogoutAccount(t *testing.T, cfg *config.Config, origin string) {
	t.Helper()
	if err := cfg.Update(func(next *config.Config) error {
		next.Remote.AuthMode, next.Remote.HubURL, next.Remote.Enabled, next.Remote.KeepAwake = config.RemoteAuthNOSEC, origin, true, false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	id, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.WriteRemoteIdentity(id.Private()); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 64)
	path, err := cfg.StageRemoteToken(token, origin, crypto.PublicBase64(id.Public()))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ActivateRemoteToken(path, config.RemoteCredential{Token: token, Origin: origin, Authorized: true, HostID: "host", UserID: "user", Username: "account"}); err != nil {
		t.Fatal(err)
	}
}

func TestNOSECLogoutHTTPKeepsStatusResponsiveAndRefusesAnotherOwner(t *testing.T) {
	h := newHarness(t)
	entered, release := make(chan struct{}), make(chan struct{})
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, `{"data":{"logged_out":true}}`)
	}))
	defer gateway.Close()
	installHTTPLogoutAccount(t, h.app.Config, gateway.URL)
	done := make(chan *http.Response, 1)
	go func() {
		response, err := h.srv.Client().Post(h.srv.URL+"/api/remote/logout", "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Error(err)
		}
		done <- response
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("logout never reached Gateway")
	}
	status := h.json(http.MethodGet, "/api/remote/status", nil, http.StatusOK)
	if status["login"].(map[string]any)["state"] != "signing_out" || status["has_token"] != true {
		t.Fatal("in-flight remote logout hid its recovery status")
	}
	got := h.json(http.MethodPost, "/api/remote/logout", map[string]any{}, http.StatusConflict)
	if got["code"] != "remote_logout_in_progress" {
		t.Fatal("second logout did not retain the current owner")
	}
	close(release)
	response := <-done
	if response == nil {
		t.Fatal("logout request failed")
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("confirmed logout status %d", response.StatusCode)
	}
}

func TestNOSECLogoutHTTPReportsUpgradeWithoutDiscardingCredential(t *testing.T) {
	h := newHarness(t)
	gateway := httptest.NewServer(http.NotFoundHandler())
	defer gateway.Close()
	installHTTPLogoutAccount(t, h.app.Config, gateway.URL)
	got := h.json(http.MethodPost, "/api/remote/logout", map[string]any{}, http.StatusConflict)
	if got["code"] != "remote_logout_unsupported" || !h.app.Config.HasHostToken() {
		t.Fatal("old Gateway discarded the retry credential or hid the upgrade requirement")
	}
	status := h.json(http.MethodGet, "/api/remote/status", nil, http.StatusOK)
	login := status["login"].(map[string]any)
	if login["state"] != "error" || login["username"] != "account" || login["user_code"] != nil {
		t.Fatalf("failed logout lost its retained safe identity: %v", login)
	}
	service, err := server.New(server.Options{Engine: h.app.Engine})
	if err != nil {
		t.Fatal(err)
	}
	local := httptest.NewServer(service.Handler())
	defer local.Close()
	response, err := local.Client().Post(local.URL+"/api/remote/logout", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatal("absent remote Host claimed logout success")
	}
}

func TestNOSECExplicitAccountEnableCannotCancelAnOwnedLogout(t *testing.T) {
	h := newHarness(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var starts atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/logout") {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			_, _ = io.WriteString(w, `{"data":{"logged_out":true}}`)
			return
		}
		starts.Add(1)
		w.WriteHeader(http.StatusConflict)
	}))
	defer gateway.Close()
	installHTTPLogoutAccount(t, h.app.Config, gateway.URL)
	service, err := server.New(server.Options{Engine: h.app.Engine, OpenURL: func(string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	service.SetRemote(h.app.Remote)
	local := httptest.NewServer(service.Handler())
	defer local.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	before := h.app.Config.Snapshot().Remote
	fileBefore, err := os.Stat(h.app.Config.Path())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan *http.Response, 1)
	go func() {
		response, err := local.Client().Post(local.URL+"/api/remote/logout", "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Error(err)
		}
		done <- response
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("logout did not reach Gateway")
	}
	client := &http.Client{Timeout: time.Second}
	response, err := client.Post(local.URL+"/api/remote/login", "application/json", strings.NewReader(`{"enable_account":true}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("account enable bypassed logout ownership: status %d", response.StatusCode)
	}
	fileAfter, err := os.Stat(h.app.Config.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(fileBefore, fileAfter) || h.app.Config.Snapshot().Remote != before || starts.Load() != 0 {
		t.Fatal("blocked login saved configuration or dispatched a new flow")
	}
	if h.app.Remote.Status().Login.State != "signing_out" {
		t.Fatal("blocked login reloaded and cancelled the logout owner")
	}
	close(release)
	logout := <-done
	if logout == nil {
		t.Fatal("logout request failed")
	}
	logout.Body.Close()
	if logout.StatusCode != http.StatusOK {
		t.Fatalf("original logout status %d", logout.StatusCode)
	}
}
