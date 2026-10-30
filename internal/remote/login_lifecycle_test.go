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

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/pairlink/relay"
	pstore "github.com/LubyRuffy/pairlink/store"
)

func TestNOSECLoginDoesNotSendOtherOriginCredentialAndRecoversExplicitly(t *testing.T) {
	for _, sameOrigin := range []bool{false, true} {
		t.Run(map[bool]string{true: "same-origin", false: "changed-origin"}[sameOrigin], func(t *testing.T) {
			incoming := make(chan loginStartRequest, 4)
			var requests atomic.Int32
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != desktopAuthPrefix+"start" {
					t.Error("unexpected registration or poll")
					http.Error(w, "forbidden", 403)
					return
				}
				var request loginStartRequest
				_ = json.NewDecoder(r.Body).Decode(&request)
				incoming <- request
				requests.Add(1)
				http.Error(w, "device conflict", 409)
			}))
			defer gateway.Close()
			e := testEngine(t)
			cfg := e.Config()
			cfg.Remote.Enabled = true
			cfg.Remote.AuthMode = "nosec"
			cfg.Remote.HubURL = gateway.URL
			oldOrigin := "https://previous.example.invalid"
			if sameOrigin {
				oldOrigin = gateway.URL
			}
			original := strings.Repeat("a", 64)
			path, err := cfg.StageRemoteToken(original, oldOrigin, "prior-public-key")
			if err != nil {
				t.Fatal(err)
			}
			if err := cfg.ActivateRemoteToken(path, config.RemoteCredential{Token: original, Origin: oldOrigin, Authorized: true, HostID: strings.Repeat("a", 32), UserID: strings.Repeat("b", 32)}); err != nil {
				t.Fatal(err)
			}
			_ = cfg.RemovePendingRemoteToken(path)
			host := New(e, cfg, nil)
			defer host.Stop()
			start := func(newDevice bool) loginStartRequest {
				t.Helper()
				if err := host.StartLogin(func(string) error { return nil }, newDevice); err != nil {
					t.Fatal(err)
				}
				var got loginStartRequest
				select {
				case got = <-incoming:
				case <-time.After(time.Second):
					t.Fatal("missing device start")
				}
				waitNOSEC(t, func() bool { return host.Status().Login.State == "error" })
				if host.Status().Login.ErrorCode != "device_conflict" {
					t.Fatal("device conflict not exposed safely")
				}
				active, err := cfg.ReadRemoteCredential()
				if err != nil || active.Token != original || active.Origin != oldOrigin {
					t.Fatal("failed login destroyed prior active credential")
				}
				return got
			}
			first := start(false)
			if sameOrigin && (first.Candidate != original || first.Previous != original) {
				t.Fatal("same-origin reconnect changed existing host token")
			}
			if !sameOrigin && (first.Candidate == original || first.Previous != "") {
				t.Fatal("other-origin credential was sent to a new gateway")
			}
			retry := start(false)
			if retry.Candidate != first.Candidate || retry.Previous != retry.Candidate {
				t.Fatal("retry lost its private candidate and could orphan authorized host")
			}
			explicit := start(true)
			if explicit.Candidate == first.Candidate || explicit.Candidate == original || explicit.Previous != "" {
				t.Fatal("explicit new device did not mint a fresh candidate")
			}
			if requests.Load() != 3 {
				t.Fatal("conflict silently retried or rotated credentials")
			}
		})
	}
}

func TestNOSECCancelAndConfigurationChangeJoinBlockedLogin(t *testing.T) {
	for _, action := range []string{"cancel", "stop", "reload"} {
		t.Run(action, func(t *testing.T) {
			entered := make(chan struct{})
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				close(entered)
				<-r.Context().Done()
			}))
			defer gateway.Close()
			e := testEngine(t)
			cfg := e.Config()
			cfg.Remote.Enabled = true
			cfg.Remote.AuthMode = "nosec"
			cfg.Remote.HubURL = gateway.URL
			host := New(e, cfg, nil)
			defer host.Stop()
			if err := host.StartLogin(func(string) error { return nil }, false); err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("start not sent")
			}
			finished := make(chan struct{})
			go func() {
				switch action {
				case "cancel":
					host.CancelLogin()
				case "stop":
					host.Stop()
				case "reload":
					_ = cfg.Update(func(next *config.Config) error { next.Remote.HubURL = "https://changed.example.invalid"; return nil })
					host.Reload()
				}
				close(finished)
			}()
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("lifecycle operation waited on network or itself")
			}
			if cfg.HasHostToken() || host.Status().Online {
				t.Fatal("cancelled login activated credential")
			}
		})
	}
}

func TestNOSECLateAuthorizedPollCannotActivateAfterCancel(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var origin string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/start") {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": loginStartResponse{DeviceCode: strings.Repeat("d", 64), UserCode: "PUBLIC", VerificationURI: origin + desktopVerificationPath + "?desktop_code=PUBLIC", ExpiresAt: time.Now().Add(time.Minute), Interval: 5}})
			return
		}
		close(entered)
		<-release
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"status": "authorized", "host_id": strings.Repeat("a", 32), "user": map[string]string{"id": strings.Repeat("b", 32), "username": "approved"}}})
	}))
	origin = gateway.URL
	defer gateway.Close()
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled = true
	cfg.Remote.AuthMode = "nosec"
	cfg.Remote.HubURL = origin
	host := New(e, cfg, nil)
	host.loginWait = func(context.Context, time.Duration) error { return nil }
	defer host.Stop()
	if err := host.StartLogin(func(string) error { return nil }, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("poll not sent")
	}
	host.CancelLogin()
	close(release)
	if cfg.HasHostToken() || host.Status().Login.State != "cancelled" {
		t.Fatal("late authorization bypassed cancellation")
	}
}

func TestNOSECPrivatePairlinkRequestsNeverFollowRedirectsOrEchoSecrets(t *testing.T) {
	var forwarded atomic.Int32
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1); http.Error(w, "untrusted", 403) }))
	defer external.Close()
	var redirect atomic.Bool
	redirect.Store(true)
	secret := strings.Repeat("a", 64)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if redirect.Load() {
			http.Redirect(w, r, external.URL+r.URL.Path, http.StatusTemporaryRedirect)
			return
		}
		http.Error(w, secret, http.StatusForbidden)
	}))
	defer gateway.Close()
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled = true
	cfg.Remote.AuthMode = "nosec"
	cfg.Remote.HubURL = gateway.URL
	path, err := cfg.StageRemoteToken(secret, gateway.URL, "public")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ActivateRemoteToken(path, config.RemoteCredential{Token: secret, Origin: gateway.URL, Authorized: true, HostID: strings.Repeat("a", 32), UserID: strings.Repeat("b", 32)}); err != nil {
		t.Fatal(err)
	}
	host := New(e, cfg, nil)
	defer host.Stop()
	for _, reveal := range []bool{false, true} {
		redirect.Store(!reveal)
		host.Reload()
		_, listErr := host.ListBindings(context.Background())
		revokeErr := host.RevokeBinding(context.Background(), "binding")
		raw, _ := json.Marshal(host.Status())
		if strings.Contains(string(raw), secret) || listErr != nil && strings.Contains(listErr.Error(), secret) || revokeErr != nil && strings.Contains(revokeErr.Error(), secret) {
			t.Fatal("untrusted Pairlink response exposed private credential")
		}
	}
	if forwarded.Load() != 0 {
		t.Fatal("private Pairlink request followed another origin")
	}
}

func TestNOSECPollProofDenialIsTerminal(t *testing.T) {
	var origin string
	var polls atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/start") {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": loginStartResponse{DeviceCode: strings.Repeat("d", 64), UserCode: "PUBLIC", VerificationURI: origin + desktopVerificationPath + "?desktop_code=PUBLIC", ExpiresAt: time.Now().Add(time.Minute), Interval: 5}})
			return
		}
		polls.Add(1)
		http.Error(w, "proof denied", http.StatusUnauthorized)
	}))
	origin = gateway.URL
	defer gateway.Close()
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled = true
	cfg.Remote.AuthMode = "nosec"
	cfg.Remote.HubURL = origin
	host := New(e, cfg, nil)
	var waits atomic.Int32
	host.loginWait = func(ctx context.Context, _ time.Duration) error {
		if waits.Add(1) == 1 {
			return nil
		}
		<-ctx.Done()
		return ctx.Err()
	}
	defer host.Stop()
	if err := host.StartLogin(func(string) error { return nil }, false); err != nil {
		t.Fatal(err)
	}
	waitNOSEC(t, func() bool { return host.Status().Login.State == "access_denied" })
	if polls.Load() != 1 || cfg.HasHostToken() {
		t.Fatal("proof denial retried or activated credential")
	}
}

func TestNOSECStopDoesNotWaitBehindBlockedOffer(t *testing.T) {
	memory := pstore.NewMemory()
	handler := relay.New(memory).Handler()
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/pairlink/v1/pairings" {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
			}
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer gateway.Close()
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled = true
	cfg.Remote.AuthMode = "nosec"
	cfg.Remote.HubURL = gateway.URL
	id, err := loadIdentity(cfg)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 64)
	if err := memory.PutHost(context.Background(), pstore.Host{Pub: id.Public(), TokenHash: pstore.HashSecret(token), Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	path, err := cfg.StageRemoteToken(token, gateway.URL, "public")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ActivateRemoteToken(path, config.RemoteCredential{Token: token, Origin: gateway.URL, Authorized: true, HostID: strings.Repeat("a", 32), UserID: strings.Repeat("b", 32)}); err != nil {
		t.Fatal(err)
	}
	host := New(e, cfg, nil)
	defer host.Stop()
	host.Start()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	offerDone := make(chan error, 1)
	go func() { _, err := host.Offer(ctx); offerDone <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("offer did not reach HTTP boundary")
	}
	stopped := make(chan struct{})
	go func() { host.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Stop waited behind offer network under host mutex")
	}
	cancel()
	select {
	case err := <-offerDone:
		if err == nil {
			t.Fatal("stopped host published a late QR")
		}
	case <-time.After(time.Second):
		t.Fatal("offer did not stop")
	}
}
