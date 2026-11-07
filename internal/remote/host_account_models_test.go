package remote

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Signing in on this PC is what unlocks the server's models, and signing out
// is what takes them away. Both must reach the model pool without a restart.
func TestLoginAndLogoutDriveTheAccountModels(t *testing.T) {
	var origin string
	var challenge string
	var bootstraps atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case desktopAuthPrefix + "start":
			var start loginStartRequest
			_ = json.NewDecoder(r.Body).Decode(&start)
			challenge = start.Challenge
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": loginStartResponse{DeviceCode: strings.Repeat("d", 64), UserCode: "ABCD-EFGH", VerificationURI: origin + desktopVerificationFixturePath + "?desktop_code=ABCD-EFGH", ExpiresAt: time.Now().Add(9 * time.Minute), Interval: 5}})
		case desktopAuthPrefix + "poll":
			var proof map[string]string
			_ = json.NewDecoder(r.Body).Decode(&proof)
			if pkceChallenge(proof["code_verifier"]) != challenge {
				http.Error(w, "invalid proof", 403)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"status": "authorized", "host_id": strings.Repeat("a", 32),
				"user": map[string]string{"id": strings.Repeat("b", 32), "username": "account", "display_name": "Account"},
			}})
		case desktopAuthPrefix + "logout":
			_, _ = io.WriteString(w, `{"data":{"logged_out":true}}`)
		case "/client/desktop/v1/bootstrap":
			bootstraps.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"models": map[string]any{
				"source_id": accountModelsSourceID, "source_kind": "gateway", "label": "Server", "execution": "desktop",
				"api_base_url": origin + "/client/desktop/v1", "catalog_url": origin + "/client/desktop/v1/models", "api_access_enabled": true,
			}}})
		case "/client/desktop/v1/models":
			_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"server-model"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	origin = gateway.URL
	defer gateway.Close()

	e := testEngineWithPool(t, false)
	cfg := e.Config()
	cfg.Remote.Enabled = true
	cfg.Remote.AuthMode = "account"
	cfg.Remote.HubURL = origin
	host := New(e, cfg, nil)
	host.loginWait = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	defer host.Stop()
	host.Start()
	if st := host.AccountModels(); st.State != AccountSignedOut {
		t.Fatalf("before login: %+v", st)
	}

	if err := host.StartLogin(func(string) error { return nil }, false); err != nil {
		t.Fatal(err)
	}
	// loginWait returns at once, so the poll that authorizes runs immediately.
	waitAccountLogin(t, func() bool { return host.AccountModels().State == AccountReady })
	if st := host.AccountModels(); st.Models != 1 || st.Username != "account" {
		t.Fatalf("after login: %+v", st)
	}

	if err := host.Logout(); err != nil {
		t.Fatal(err)
	}
	// Logout itself drops the catalog. Waiting for a later refresh would hide
	// a return that left the signed-out account's models in place.
	if st := host.AccountModels(); st.State != AccountSignedOut || e.Providers().AccountSourceID() != "" {
		t.Fatalf("logout returned while the account catalog was still published: %+v", st)
	}
}

func TestReloadRefreshesOnlyWhenTheAccountChanged(t *testing.T) {
	g := newAccountGateway(t)
	var bootstraps atomic.Int32
	g.bootstrap = func(w http.ResponseWriter, r *http.Request) {
		bootstraps.Add(1)
		g.writeBootstrap(w, g.server.URL, true)
	}
	e := testEngineWithPool(t, false)
	cfg := e.Config()
	cfg.Remote.AuthMode = "account"
	cfg.Remote.HubURL = g.server.URL
	host := New(e, cfg, nil)
	defer host.Stop()
	host.Start()
	waitAccountLogin(t, func() bool { return host.AccountModels().State == AccountSignedOut })

	signInAccount(t, cfg, g.server.URL)
	host.Reload()
	waitAccountLogin(t, func() bool { return host.AccountModels().State == AccountReady })
	seen := bootstraps.Load()
	// Every settings save reloads the host; none of those may re-contact the
	// server for an account that has not changed.
	for i := 0; i < 3; i++ {
		host.Reload()
	}
	time.Sleep(150 * time.Millisecond)
	if bootstraps.Load() != seen {
		t.Fatalf("Reload contacted the server %d extra times for an unchanged account", bootstraps.Load()-seen)
	}
	signOutAccount(t, cfg)
	host.Reload()
	waitAccountLogin(t, func() bool { return host.AccountModels().State == AccountSignedOut })
}

func TestStoppedHostStopsRefreshingAccountModels(t *testing.T) {
	g := newAccountGateway(t)
	var bootstraps atomic.Int32
	g.bootstrap = func(w http.ResponseWriter, r *http.Request) {
		bootstraps.Add(1)
		g.writeBootstrap(w, g.server.URL, true)
	}
	e := testEngineWithPool(t, false)
	cfg := e.Config()
	cfg.Remote.AuthMode = "account"
	cfg.Remote.HubURL = g.server.URL
	signInAccount(t, cfg, g.server.URL)
	host := New(e, cfg, nil)
	host.accounts.interval = 10 * time.Millisecond
	host.Start()
	waitAccountLogin(t, func() bool { return bootstraps.Load() >= 2 })
	host.Stop()
	seen := bootstraps.Load()
	time.Sleep(100 * time.Millisecond)
	if bootstraps.Load() != seen {
		t.Fatal("a stopped host kept contacting the server")
	}
}

// An offline run promises that no network is reached. A signed-in credential on
// the scripted provider must therefore neither contact the server nor report
// models the pool would ignore, and the host must stay usable.
func TestOfflineProviderNeverContactsTheAccountServer(t *testing.T) {
	var requests atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The pairlink registration of an enabled host is not under test here.
		if strings.HasPrefix(r.URL.Path, desktopModelsPrefix) {
			requests.Add(1)
		}
		http.NotFound(w, r)
	}))
	defer gateway.Close()

	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled = true
	cfg.Remote.AuthMode = "account"
	cfg.Remote.HubURL = gateway.URL
	signInAccount(t, cfg, gateway.URL)
	host := New(e, cfg, nil)
	defer host.Stop()
	host.Start()
	host.Reload()
	if st := host.AccountModels(); st.State != AccountSignedOut || st.Models != 0 {
		t.Fatalf("offline host reported %+v", st)
	}
	time.Sleep(100 * time.Millisecond)
	if n := requests.Load(); n != 0 {
		t.Fatalf("offline provider made %d account requests", n)
	}
}
