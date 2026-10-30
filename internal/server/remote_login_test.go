package server_test

import (
	"encoding/json"
	"github.com/LubyRuffy/eino-swarm/internal/server"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNOSECLoginRoutesRequireLocalBrowserAndRefuseManualCredential(t *testing.T) {
	h := newHarness(t)
	h.app.Config.Remote.AuthMode = "nosec"
	h.app.Config.Remote.Enabled = true
	h.app.Config.Remote.HubURL = "https://gateway.example.invalid"
	h.json(http.MethodPost, "/api/remote/login", map[string]any{}, http.StatusNotImplemented)
	h.json(http.MethodPut, "/api/remote/token", map[string]any{"token": "cannot-bypass-account-approval"}, http.StatusForbidden)
	if h.app.Config.HasHostToken() {
		t.Fatal("manual token bypassed NOSEC authorization")
	}
	h.json(http.MethodDelete, "/api/remote/login", nil, http.StatusOK)
	status := h.json(http.MethodGet, "/api/remote/status", nil, http.StatusOK)
	if status["auth_mode"] != "nosec" || status["login_available"] != false {
		t.Fatalf("unsafe capabilities: %v", status)
	}
}

func TestNOSECLocalLoginUsesStrictIntentAndSafeStatus(t *testing.T) {
	h := newHarness(t)
	h.app.Config.Remote.AuthMode = "nosec"
	h.app.Config.Remote.Enabled = true
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "not approved", http.StatusConflict) }))
	defer gateway.Close()
	h.app.Config.Remote.HubURL = gateway.URL
	service, err := server.New(server.Options{Engine: h.app.Engine, Mode: server.ModeDesktop, OpenURL: func(string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	service.SetRemote(h.app.Remote)
	local := httptest.NewServer(service.Handler())
	defer local.Close()
	defer h.app.Remote.Stop()
	for _, body := range []string{`null`, `{"new_device":true}`, `{"new_device":true,"confirmed":false}`, `{"candidate_token":"must-not-enter-ui"}`, `{} {}`, `{`} {
		response, err := local.Client().Post(local.URL+"/api/remote/login", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("unsafe login intent accepted with %d", response.StatusCode)
		}
	}
	h.app.Config.Remote.AuthMode = "legacy"
	response, err := local.Client().Post(local.URL+"/api/remote/login", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatal("legacy mode accepted account login")
	}
	h.app.Config.Remote.AuthMode = "nosec"
	response, err = local.Client().Post(local.URL+"/api/remote/login", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("login was not accepted: %d %s", response.StatusCode, raw)
	}
	for _, private := range []string{"candidate_token", "device_code", "code_verifier", "verification_uri", "pending-host-token"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("private material appeared in local response")
		}
	}
	request, _ := http.NewRequest(http.MethodDelete, local.URL+"/api/remote/login", nil)
	cancelled, err := local.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelled.Body.Close()
	var status map[string]any
	if err := json.NewDecoder(cancelled.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status["login_available"] != true || status["login"].(map[string]any)["state"] != "cancelled" {
		t.Fatal("cancel did not return safe current status")
	}
}
