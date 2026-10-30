package remote

import (
	"context"
	"encoding/json"
	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/relay"
	pstore "github.com/LubyRuffy/pairlink/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNOSECModeNeverRegistersAnAnonymousHost(t *testing.T) {
	var registrations atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/pairlink/v1/hosts" {
			registrations.Add(1)
		}
		http.Error(w, "not authorized", http.StatusUnauthorized)
	}))
	defer gateway.Close()
	e := testEngine(t)
	cfg := e.Config()
	raw, _ := json.Marshal(map[string]any{"enabled": true, "hub_url": gateway.URL, "auth_mode": "nosec"})
	if err := json.Unmarshal(raw, &cfg.Remote); err != nil {
		t.Fatal(err)
	}
	host := New(e, cfg, nil)
	defer host.Stop()
	host.Start()
	if registrations.Load() != 0 || cfg.HasHostToken() {
		t.Fatal("NOSEC mode anonymously minted/registered a host before approved device authorization")
	}
	if host.Status().Online {
		t.Fatal("unapproved NOSEC host became online")
	}
}

func TestNOSECDeviceAuthorizationUsesRealWireAndConnectsOnlyAfterApproval(t *testing.T) {
	memory := pstore.NewMemory()
	relayHandler := relay.New(memory).Handler()
	requests := make(chan loginStartRequest, 1)
	statuses := make(chan string, 8)
	polls := make(chan struct{}, 8)
	var registrations atomic.Int32
	var origin string
	var active loginStartRequest
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case desktopAuthPrefix + "start":
			if json.NewDecoder(r.Body).Decode(&active) != nil {
				http.Error(w, "bad request", 400)
				return
			}
			if _, err := crypto.ParsePublic(active.PublicKey); err != nil {
				http.Error(w, "invalid public key", 400)
				return
			}
			requests <- active
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": loginStartResponse{DeviceCode: strings.Repeat("d", 64), UserCode: "ABCD-EFGH", VerificationURI: origin + desktopVerificationPath + "?desktop_code=ABCD-EFGH", ExpiresAt: time.Now().Add(9 * time.Minute), Interval: 5}})
		case desktopAuthPrefix + "poll":
			var proof map[string]string
			_ = json.NewDecoder(r.Body).Decode(&proof)
			if proof["device_code"] != strings.Repeat("d", 64) || pkceChallenge(proof["code_verifier"]) != active.Challenge {
				http.Error(w, "invalid proof", 403)
				return
			}
			status := <-statuses
			data := map[string]any{"status": status}
			if status == "authorized" {
				pub, _ := crypto.ParsePublic(active.PublicKey)
				if err := memory.PutHost(r.Context(), pstore.Host{Pub: pub, TokenHash: pstore.HashSecret(active.Candidate), Created: time.Now()}); err != nil {
					t.Error("could not prepare authorized relay host")
				}
				data["host_id"] = strings.Repeat("a", 32)
				data["user"] = map[string]string{"id": strings.Repeat("b", 32), "username": "approved-user"}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
		default:
			if r.URL.Path == "/pairlink/v1/hosts" {
				registrations.Add(1)
			}
			relayHandler.ServeHTTP(w, r)
		}
	}))
	origin = gateway.URL
	defer gateway.Close()
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled = true
	cfg.Remote.AuthMode = "nosec"
	cfg.Remote.HubURL = origin
	host := New(e, cfg, nil)
	host.loginWait = func(ctx context.Context, _ time.Duration) error {
		select {
		case <-polls:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer host.Stop()
	opened := make(chan string, 1)
	if err := host.StartLogin(func(target string) error { opened <- target; return nil }, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-opened:
	case <-time.After(2 * time.Second):
		t.Fatalf("browser never opened, login status: %+v", host.Status().Login)
	}
	var start loginStartRequest
	select {
	case start = <-requests:
	default:
		t.Fatal("missing actual start request")
	}
	if !tokenShape(start.Candidate) || len(start.Challenge) != 43 || start.Previous != "" {
		t.Fatal("incorrect candidate or proof shape")
	}
	staged, err := cfg.PendingRemoteToken(origin, start.PublicKey)
	if err != nil || staged.Token != start.Candidate {
		t.Fatal("candidate was not privately durable before browser launch")
	}
	for _, state := range []string{"authorization_pending", "account_pending", "account_rejected"} {
		statuses <- state
		polls <- struct{}{}
		waitNOSEC(t, func() bool { return host.Status().Login.State == state })
		if registrations.Load() != 0 || host.Status().Online || cfg.HasHostToken() {
			t.Fatal("unapproved device acquired a relay credential")
		}
		if _, err := host.Offer(context.Background()); err == nil {
			t.Fatal("unapproved device produced QR")
		}
	}
	status := host.Status()
	if status.Login.HostFP == "" || status.Login.UserCode != "ABCD-EFGH" {
		t.Fatal("public verification details missing")
	}
	raw, _ := json.Marshal(status)
	for _, secret := range []string{start.Candidate, strings.Repeat("d", 64)} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("private login material crossed the status boundary")
		}
	}
	statuses <- "authorized"
	polls <- struct{}{}
	waitNOSEC(t, func() bool { return host.Status().Online })
	saved, err := cfg.ReadRemoteCredential()
	if err != nil || !saved.Authorized || saved.Token != start.Candidate || saved.Origin != origin || saved.HostID != strings.Repeat("a", 32) {
		t.Fatal("authorized receipt was not activated")
	}
	if registrations.Load() != 1 {
		t.Fatal("authorization did not make exactly one actual registration")
	}
	if _, err := host.Offer(context.Background()); err != nil {
		t.Fatalf("approved online host cannot pair: %v", err)
	}
	stopped := make(chan struct{})
	go func() { host.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("authorization completion deadlocked with Stop")
	}
}
func waitNOSEC(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("NOSEC state transition timed out")
}
