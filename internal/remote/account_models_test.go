package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

const (
	accountModelsToken    = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	accountModelsSourceID = "gateway:0123456789abcdef0123456789abcdef"
)

// accountGateway is a loopback stand-in for the server's desktop model routes.
type accountGateway struct {
	t         *testing.T
	server    *httptest.Server
	mu        sync.Mutex
	bootstrap func(w http.ResponseWriter, r *http.Request)
	catalog   func(w http.ResponseWriter, r *http.Request)
	requests  []string
	auths     []string
}

func newAccountGateway(t *testing.T) *accountGateway {
	g := &accountGateway{t: t}
	g.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.requests = append(g.requests, r.URL.RequestURI())
		g.auths = append(g.auths, r.Header.Get("Authorization"))
		bootstrap, catalog := g.bootstrap, g.catalog
		g.mu.Unlock()
		switch r.URL.Path {
		case "/client/desktop/v1/bootstrap":
			bootstrap(w, r)
		case "/client/desktop/v1/models":
			catalog(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(g.server.Close)
	g.bootstrap = func(w http.ResponseWriter, r *http.Request) { g.writeBootstrap(w, g.server.URL, true) }
	g.catalog = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"server-model","context_length":64000},{"id":"server-model-2"}]}`)
	}
	return g
}

func (g *accountGateway) writeBootstrap(w http.ResponseWriter, base string, access bool) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
		"user": map[string]any{"id": strings.Repeat("2", 32), "username": "Account", "display_name": "Account Name"}, "host_id": strings.Repeat("1", 32),
		"models": map[string]any{
			"source_id": accountModelsSourceID, "source_kind": "gateway", "label": "Server", "execution": "desktop",
			"api_base_url": base + "/client/desktop/v1", "catalog_url": base + "/client/desktop/v1/models", "api_access_enabled": access,
		},
	}})
}

func (g *accountGateway) seen() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.requests...)
}

func accountModelsFixture(t *testing.T, g *accountGateway) (*AccountModels, *provider.Pool, *config.Config) {
	t.Helper()
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Update(func(next *config.Config) error {
		next.Remote.AuthMode = config.RemoteAuthNOSEC
		next.Remote.HubURL = g.server.URL
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	signInAccount(t, cfg, g.server.URL)
	pool := provider.New(cfg)
	return NewAccountModels(cfg, pool, slog.New(slog.NewTextHandler(io.Discard, nil))), pool, cfg
}

func signInAccount(t *testing.T, cfg *config.Config, origin string) {
	t.Helper()
	path, err := cfg.StageRemoteToken(accountModelsToken, origin, "test-public-key")
	if err != nil {
		t.Fatal(err)
	}
	err = cfg.ActivateRemoteToken(path, config.RemoteCredential{
		Token: accountModelsToken, Origin: origin, Authorized: true,
		HostID: strings.Repeat("1", 32), UserID: strings.Repeat("2", 32), Username: "Account", DisplayName: "Account Name",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func signOutAccount(t *testing.T, cfg *config.Config) {
	t.Helper()
	if err := os.Remove(filepath.Join(cfg.RemoteDir(), "host_token")); err != nil {
		t.Fatal(err)
	}
}

func TestSignedInAccountPublishesItsModelsToTheComposer(t *testing.T) {
	g := newAccountGateway(t)
	am, pool, _ := accountModelsFixture(t, g)
	if st := am.State(); st.State != AccountPending {
		t.Fatalf("a signed-in start must not claim signed_out before its first refresh: %+v", st)
	}

	st := am.Refresh(context.Background())
	if st.State != AccountReady || st.Models != 2 || st.Label != "Server" || st.Username != "Account" || st.DisplayName != "Account Name" {
		t.Fatalf("state: %+v", st)
	}
	list := pool.List()
	if len(list) < 2 || list[0].ProviderID != accountModelsSourceID || list[0].Source != provider.SourceAccount || list[0].ContextWindow != 64000 || list[0].Model != "server-model" {
		t.Fatalf("composer rows: %+v", list)
	}
	for i, auth := range g.auths {
		if auth != "Bearer "+accountModelsToken {
			t.Fatalf("request %d used credential %q", i, auth)
		}
	}
	for _, uri := range g.seen() {
		if strings.Contains(uri, "?") || strings.Contains(uri, accountModelsToken) {
			t.Fatalf("the credential or a query reached a URL: %s", uri)
		}
	}
	raw, _ := json.Marshal(am.State())
	if strings.Contains(string(raw), accountModelsToken) || strings.Contains(string(raw), g.server.URL) {
		t.Fatalf("state leaks the credential or endpoint: %s", raw)
	}
}

// The credential must never be sent to a host the credential does not belong
// to, whatever a (compromised or misconfigured) server claims.
func TestBootstrapPointingToAnotherOriginIsRefusedBeforeAnyCredentialIsSent(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("a foreign origin received a request")
	}))
	defer other.Close()
	for name, mutate := range map[string]func(map[string]any){
		"api_base_url": func(m map[string]any) { m["api_base_url"] = other.URL + "/client/desktop/v1" },
		"catalog_url":  func(m map[string]any) { m["catalog_url"] = other.URL + "/models" },
	} {
		t.Run(name, func(t *testing.T) {
			g := newAccountGateway(t)
			g.bootstrap = func(w http.ResponseWriter, r *http.Request) {
				rec := httptest.NewRecorder()
				g.writeBootstrap(rec, g.server.URL, true)
				var body map[string]map[string]map[string]any
				_ = json.Unmarshal(rec.Body.Bytes(), &body)
				mutate(body["data"]["models"])
				_ = json.NewEncoder(w).Encode(body)
			}
			am, pool, _ := accountModelsFixture(t, g)
			st := am.Refresh(context.Background())
			if st.State != AccountUnavailable || st.Reason != AccountReasonUnsafe || pool.AccountSourceID() != "" {
				t.Fatalf("foreign endpoint accepted: %+v", st)
			}
		})
	}
}

func TestBootstrapWithUnexpectedIdentityIsRefused(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"source id":   func(m map[string]any) { m["source_id"] = "gateway:not-hex" },
		"source id 2": func(m map[string]any) { m["source_id"] = "own-provider" },
		"kind":        func(m map[string]any) { m["source_kind"] = "provider" },
		"execution":   func(m map[string]any) { m["execution"] = "phone" },
	} {
		t.Run(name, func(t *testing.T) {
			g := newAccountGateway(t)
			g.bootstrap = func(w http.ResponseWriter, r *http.Request) {
				rec := httptest.NewRecorder()
				g.writeBootstrap(rec, g.server.URL, true)
				var body map[string]map[string]map[string]any
				_ = json.Unmarshal(rec.Body.Bytes(), &body)
				mutate(body["data"]["models"])
				_ = json.NewEncoder(w).Encode(body)
			}
			am, pool, _ := accountModelsFixture(t, g)
			if st := am.Refresh(context.Background()); st.State != AccountUnavailable || pool.AccountSourceID() != "" {
				t.Fatalf("unexpected bootstrap identity accepted: %+v", st)
			}
		})
	}
}

func TestRedirectsNeverCarryTheCredentialElsewhere(t *testing.T) {
	var hits atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer other.Close()
	g := newAccountGateway(t)
	g.bootstrap = func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL+"/x", http.StatusFound) }
	am, pool, _ := accountModelsFixture(t, g)
	if st := am.Refresh(context.Background()); st.State != AccountUnavailable || hits.Load() != 0 || pool.AccountSourceID() != "" {
		t.Fatalf("redirect followed: %+v hits=%d", st, hits.Load())
	}
}

func TestSigningOutRemovesTheProjectionAndForgetsTheCredential(t *testing.T) {
	g := newAccountGateway(t)
	am, pool, cfg := accountModelsFixture(t, g)
	if st := am.Refresh(context.Background()); st.State != AccountReady {
		t.Fatalf("setup: %+v", st)
	}
	signOutAccount(t, cfg)
	before := len(g.seen())
	st := am.Refresh(context.Background())
	if st.State != AccountSignedOut || pool.AccountSourceID() != "" || len(g.seen()) != before {
		t.Fatalf("signed-out state=%+v source=%q requests=%d->%d", st, pool.AccountSourceID(), before, len(g.seen()))
	}
	if st.Label != "" || st.Username != "" || st.Models != 0 {
		t.Fatalf("a signed-out account kept its identity: %+v", st)
	}
}

func TestCredentialForAnotherServerDoesNotSignInHere(t *testing.T) {
	g := newAccountGateway(t)
	am, pool, cfg := accountModelsFixture(t, g)
	if err := cfg.Update(func(next *config.Config) error { next.Remote.HubURL = "https://another-server.invalid"; return nil }); err != nil {
		t.Fatal(err)
	}
	if st := am.Refresh(context.Background()); st.State != AccountSignedOut || len(g.seen()) != 0 || pool.AccountSourceID() != "" {
		t.Fatalf("a credential for another origin was used: %+v", st)
	}
}

func TestAnonymousModeHasNoAccount(t *testing.T) {
	g := newAccountGateway(t)
	am, _, cfg := accountModelsFixture(t, g)
	if err := cfg.Update(func(next *config.Config) error { next.Remote.AuthMode = ""; return nil }); err != nil {
		t.Fatal(err)
	}
	if st := am.Refresh(context.Background()); st.State != AccountSignedOut || len(g.seen()) != 0 {
		t.Fatalf("anonymous pairing produced an account: %+v", st)
	}
}

func TestAccountWithoutModelAccessIsExplainedNotHidden(t *testing.T) {
	g := newAccountGateway(t)
	g.bootstrap = func(w http.ResponseWriter, r *http.Request) { g.writeBootstrap(w, g.server.URL, false) }
	am, pool, _ := accountModelsFixture(t, g)
	st := am.Refresh(context.Background())
	if st.State != AccountNoAccess || st.Username != "Account" || pool.AccountSourceID() != "" {
		t.Fatalf("state: %+v", st)
	}
	for _, uri := range g.seen() {
		if strings.HasSuffix(uri, "/models") {
			t.Fatal("the catalog was requested for an account without access")
		}
	}
}

func TestCatalogRefusalMeansNoAccessAndAnEmptyCatalogToo(t *testing.T) {
	for name, handler := range map[string]func(http.ResponseWriter, *http.Request){
		"forbidden": func(w http.ResponseWriter, r *http.Request) { http.Error(w, `{"error":"x"}`, http.StatusForbidden) },
		"empty": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			g := newAccountGateway(t)
			g.catalog = handler
			am, pool, _ := accountModelsFixture(t, g)
			if st := am.Refresh(context.Background()); st.State != AccountNoAccess || pool.AccountSourceID() != "" {
				t.Fatalf("state: %+v", st)
			}
		})
	}
}

// An older server has no desktop model routes. That is a state to explain, not
// a failure that breaks the rest of the app.
func TestServerWithoutDesktopModelsIsReportedAsUnsupported(t *testing.T) {
	g := newAccountGateway(t)
	g.bootstrap = func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }
	am, pool, _ := accountModelsFixture(t, g)
	if st := am.Refresh(context.Background()); st.State != AccountUnavailable || st.Reason != AccountReasonUnsupported || pool.AccountSourceID() != "" {
		t.Fatalf("state: %+v", st)
	}
}

func TestRevokedDeviceAsksForANewLogin(t *testing.T) {
	g := newAccountGateway(t)
	am, pool, _ := accountModelsFixture(t, g)
	if st := am.Refresh(context.Background()); st.State != AccountReady {
		t.Fatalf("setup: %+v", st)
	}
	g.bootstrap = func(w http.ResponseWriter, r *http.Request) { http.Error(w, `{}`, http.StatusUnauthorized) }
	st := am.Refresh(context.Background())
	if st.State != AccountSignedOut || st.Reason != AccountReasonRejected || pool.AccountSourceID() != "" {
		t.Fatalf("a rejected credential kept its models: %+v", st)
	}
}

// A brief outage must not take working models away mid-conversation, but a
// first load that fails has nothing to fall back to.
func TestTransientFailureKeepsWorkingModelsOnlyWhenThereAreSome(t *testing.T) {
	g := newAccountGateway(t)
	g.bootstrap = func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", http.StatusBadGateway) }
	am, pool, _ := accountModelsFixture(t, g)
	if st := am.Refresh(context.Background()); st.State != AccountUnavailable || st.Reason != AccountReasonGateway || pool.AccountSourceID() != "" {
		t.Fatalf("first load: %+v", st)
	}
	g.bootstrap = func(w http.ResponseWriter, r *http.Request) { g.writeBootstrap(w, g.server.URL, true) }
	if st := am.Refresh(context.Background()); st.State != AccountReady {
		t.Fatalf("recovery: %+v", st)
	}
	g.bootstrap = func(w http.ResponseWriter, r *http.Request) { http.Error(w, "down", http.StatusBadGateway) }
	st := am.Refresh(context.Background())
	if st.State != AccountReady || pool.AccountSourceID() != accountModelsSourceID {
		t.Fatalf("an outage removed working models: %+v", st)
	}
}

func TestUnreachableServerIsReportedAsANetworkProblem(t *testing.T) {
	g := newAccountGateway(t)
	am, _, _ := accountModelsFixture(t, g)
	g.server.Close()
	if st := am.Refresh(context.Background()); st.State != AccountUnavailable || st.Reason != AccountReasonNetwork {
		t.Fatalf("state: %+v", st)
	}
}

func TestOversizedServerResponsesAreRefused(t *testing.T) {
	g := newAccountGateway(t)
	g.bootstrap = func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":"`+strings.Repeat("x", 2<<20)+`"}`)
	}
	am, _, _ := accountModelsFixture(t, g)
	if st := am.Refresh(context.Background()); st.State != AccountUnavailable {
		t.Fatalf("an unbounded response was accepted: %+v", st)
	}
}

// Logout can finish while a refresh is waiting on the server. Publishing that
// late answer would resurrect models for an account that is already gone.
// The fetch can return after the credential has already changed, and the
// change can also land in the gap after the fetch observed it. Either way the
// old catalog must not be what a reader sees, including the caller of logout.
func TestACredentialThatLeavesBeforePublishIsNotApplied(t *testing.T) {
	g := newAccountGateway(t)
	am, pool, cfg := accountModelsFixture(t, g)
	am.beforePublish = func() { signOutAccount(t, cfg) }
	st := am.Refresh(context.Background())
	if st.State != AccountSignedOut || pool.AccountSourceID() != "" {
		t.Fatalf("published a catalog after its credential was gone: %+v id=%s", st, pool.AccountSourceID())
	}
}

func TestSealDropsACatalogWhoseCredentialIsAlreadyGone(t *testing.T) {
	g := newAccountGateway(t)
	am, pool, cfg := accountModelsFixture(t, g)
	if st := am.Refresh(context.Background()); st.State != AccountReady || pool.AccountSourceID() == "" {
		t.Fatalf("setup: %+v", st)
	}
	signOutAccount(t, cfg)
	if pool.AccountSourceID() == "" {
		t.Fatal("setup removed the catalog before seal")
	}
	am.Seal()
	if st := am.State(); st.State != AccountSignedOut || pool.AccountSourceID() != "" {
		t.Fatalf("seal left %+v id=%s", st, pool.AccountSourceID())
	}
	// A newer sign-in must not inherit the previous catalog either.
	signInAccount(t, cfg, g.server.URL)
	am.Seal()
	if st := am.State(); st.State != AccountPending || pool.AccountSourceID() != "" {
		t.Fatalf("a new sign-in kept the previous catalog: %+v id=%s", st, pool.AccountSourceID())
	}
}

func TestRefreshFinishingAfterSignOutPublishesNothing(t *testing.T) {
	g := newAccountGateway(t)
	entered, release := make(chan struct{}), make(chan struct{})
	g.bootstrap = func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		g.writeBootstrap(w, g.server.URL, true)
	}
	am, pool, cfg := accountModelsFixture(t, g)
	done := make(chan AccountModelsState, 1)
	go func() { done <- am.Refresh(context.Background()) }()
	<-entered
	signOutAccount(t, cfg)
	close(release)
	st := <-done
	if st.State != AccountSignedOut || pool.AccountSourceID() != "" {
		t.Fatalf("a late response resurrected the account: %+v", st)
	}
}

func TestCancelledRefreshLeavesTheLastStateAlone(t *testing.T) {
	g := newAccountGateway(t)
	am, pool, _ := accountModelsFixture(t, g)
	if st := am.Refresh(context.Background()); st.State != AccountReady {
		t.Fatalf("setup: %+v", st)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if st := am.Refresh(ctx); st.State != AccountReady || pool.AccountSourceID() != accountModelsSourceID {
		t.Fatalf("cancellation changed the account: %+v", st)
	}
}

func TestBackgroundLoopRefreshesOnTriggerAndPeriodically(t *testing.T) {
	g := newAccountGateway(t)
	var bootstraps atomic.Int32
	g.bootstrap = func(w http.ResponseWriter, r *http.Request) {
		bootstraps.Add(1)
		g.writeBootstrap(w, g.server.URL, true)
	}
	am, _, cfg := accountModelsFixture(t, g)
	am.interval = 20 * time.Millisecond
	am.Start()
	am.Start() // idempotent
	waitFor(t, func() bool { return am.State().State == AccountReady && bootstraps.Load() >= 3 }, "periodic refresh")

	signOutAccount(t, cfg)
	am.Trigger()
	waitFor(t, func() bool { return am.State().State == AccountSignedOut }, "triggered refresh after sign-out")
	am.Stop()
	am.Stop() // safe twice
	calls := bootstraps.Load()
	time.Sleep(80 * time.Millisecond)
	if bootstraps.Load() != calls {
		t.Fatal("the loop kept running after Stop")
	}
	am.Start()
	defer am.Stop()
	signInAccount(t, cfg, g.server.URL)
	am.Trigger()
	waitFor(t, func() bool { return am.State().State == AccountReady }, "restart after Stop")
}

func TestNilAccountModelsReportsSignedOut(t *testing.T) {
	var am *AccountModels
	if st := am.State(); st.State != AccountSignedOut {
		t.Fatalf("%+v", st)
	}
	am.Trigger()
	am.Start()
	am.Stop()
	if st := am.Refresh(context.Background()); st.State != AccountSignedOut {
		t.Fatalf("%+v", st)
	}
}

func waitFor(t *testing.T, ok func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(fmt.Sprintf("timed out waiting for %s", what))
}

// The credential may only travel to URLs on the origin it was issued for; each
// way of smuggling it elsewhere (another host, userinfo, query, fragment, a
// non-absolute path) must be refused.
func TestAccountURLsMustStayOnTheCredentialOrigin(t *testing.T) {
	const origin = "https://gateway.example"
	for raw, want := range map[string]bool{
		origin + "/client/desktop/v1":                   true,
		" " + origin + "/client/desktop/v1/models ":     true,
		"https://other.example/client/desktop/v1":       false,
		"http://gateway.example/client/desktop/v1":      false,
		"https://user@gateway.example/client/v1":        false,
		origin + "/client/desktop/v1?next=elsewhere":    false,
		origin + "/client/desktop/v1#fragment":          false,
		origin:                                          false,
		"mailto:someone@gateway.example":                false,
		"/client/desktop/v1":                            false,
		"https://gateway.example:444/client/desktop/v1": false,
	} {
		if got := sameOriginURL(raw, origin); got != want {
			t.Errorf("sameOriginURL(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestAccountStatusErrorNamesTheHTTPStatus(t *testing.T) {
	if msg := (&accountStatusError{code: 502}).Error(); !strings.Contains(msg, "502") {
		t.Fatalf("message %q hides the status", msg)
	}
}
