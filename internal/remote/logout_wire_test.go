package remote

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/pairlink/crypto"
)

func installLogoutAccount(t *testing.T, cfg *config.Config, origin string) string {
	t.Helper()
	cfg.Remote.AuthMode, cfg.Remote.HubURL = config.RemoteAuthNOSEC, origin
	id, err := loadIdentity(cfg)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 64)
	path, err := cfg.StageRemoteToken(token, origin, crypto.PublicBase64(id.Public()))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ActivateRemoteToken(path, config.RemoteCredential{Token: token, Origin: origin, Authorized: true, HostID: strings.Repeat("b", 32), UserID: strings.Repeat("c", 32), Username: "account"}); err != nil {
		t.Fatal(err)
	}
	return token
}

func TestNOSECLogoutRemoteAcknowledgementKeepsStatusResponsive(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		raw, err := io.ReadAll(r.Body)
		if err != nil || string(raw) != "{}" || r.Method != http.MethodPost || r.URL.Path != desktopAuthPrefix+"logout" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("a", 64) {
			t.Error("logout did not use the private exact-origin wire contract")
		}
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, `{"data":{"logged_out":true}}`)
	}))
	defer gateway.Close()
	e := testEngine(t)
	cfg := e.Config()
	installLogoutAccount(t, cfg, gateway.URL)
	host := New(e, cfg, nil)
	defer host.Stop()
	done := make(chan error, 1)
	go func() { done <- host.Logout() }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("logout returned before remote acknowledgement: %v", err)
	case <-time.After(time.Second):
		t.Fatal("logout never reached remote")
	}
	status := make(chan Status, 1)
	go func() { status <- host.Status() }()
	select {
	case current := <-status:
		if current.Login.State != "signing_out" || !current.HasToken || current.Login.Username != "account" || current.Login.UserCode != "" {
			t.Fatalf("in-flight logout lost its recovery authority: %+v", current)
		}
	case <-time.After(time.Second):
		t.Fatal("remote logout blocked the Host status lock")
	}
	if err := host.StartLogin(func(string) error { return nil }, false); err == nil {
		t.Fatal("new login crossed logout ownership")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	assertSignedOut(t, host)
	if err := host.Logout(); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("empty signed-out scope sent another network request")
	}
}

func TestNOSECLogoutUnsupportedGatewayRetainsSecretForRetry(t *testing.T) {
	var calls atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"logged_out":true}}`)
	}))
	defer gateway.Close()
	e := testEngine(t)
	cfg := e.Config()
	installLogoutAccount(t, cfg, gateway.URL)
	host := New(e, cfg, nil)
	defer host.Stop()
	if err := host.Logout(); err == nil {
		t.Fatal("unsupported Gateway was reported as logged out")
	}
	if !cfg.HasHostToken() || host.Status().Login.State != "error" || host.Status().Login.ErrorCode != "logout_unsupported" || host.Status().Login.Username != "account" || host.Status().Login.UserCode != "" {
		t.Fatal("unsupported Gateway destroyed retry proof or hid the upgrade requirement")
	}
	if err := host.Logout(); err != nil {
		t.Fatal(err)
	}
	assertSignedOut(t, host)
	if calls.Load() != 2 {
		t.Fatal("logout retry did not reuse the retained secret")
	}
}

func TestNOSECLogoutRefusalRedirectAndUnsafeReplyRetainPrivateState(t *testing.T) {
	var leaked atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1); w.WriteHeader(http.StatusOK) }))
	defer other.Close()
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTemporaryRedirect, http.StatusOK} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if code == http.StatusTemporaryRedirect {
					http.Redirect(w, r, other.URL, code)
					return
				}
				w.WriteHeader(code)
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"logged_out": false}, "error": strings.Repeat("a", 64)})
			}))
			defer gateway.Close()
			e := testEngine(t)
			cfg := e.Config()
			secret := installLogoutAccount(t, cfg, gateway.URL)
			host := New(e, cfg, nil)
			defer host.Stop()
			err := host.Logout()
			if err == nil || strings.Contains(err.Error(), secret) || !cfg.HasHostToken() {
				t.Fatal("failed remote logout exposed or discarded the unique recovery secret")
			}
			if errors.Is(err, config.ErrRemoteApprovalRequired) {
				t.Fatal("remote failure was confused with absent local authority")
			}
		})
	}
	if leaked.Load() != 0 {
		t.Fatal("private logout followed a redirect")
	}
}

func TestNOSECLogoutLateAcknowledgementCannotClearSameOriginReplacement(t *testing.T) {
	for _, replacement := range []string{"receipt", "proof"} {
		t.Run(replacement, func(t *testing.T) {
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
			e := testEngine(t)
			cfg := e.Config()
			installLogoutAccount(t, cfg, gateway.URL)
			id, err := loadIdentity(cfg)
			if err != nil {
				t.Fatal(err)
			}
			host := New(e, cfg, nil)
			defer host.Stop()
			done := make(chan error, 1)
			go func() { done <- host.Logout() }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("remote logout did not start")
			}
			if err := host.Logout(); !errors.Is(err, ErrAccountLogoutInProgress) {
				t.Fatal("second logout crossed the current owner")
			}
			next, err := cfg.StageRemoteToken("replacement", gateway.URL, crypto.PublicBase64(id.Public()))
			if err != nil {
				t.Fatal(err)
			}
			if replacement == "receipt" {
				if err := cfg.ActivateRemoteToken(next, config.RemoteCredential{Token: "replacement", Origin: gateway.URL, Authorized: true, HostID: "new-host", UserID: "new-user"}); err != nil {
					t.Fatal(err)
				}
			}
			close(release)
			if err := <-done; err == nil {
				t.Fatal("late acknowledgement claimed replacement was logged out")
			}
			current, err := cfg.ReadRemoteCredential()
			if err != nil || replacement == "receipt" && current.Token != "replacement" || replacement == "proof" && current.Token != strings.Repeat("a", 64) {
				t.Fatal("late acknowledgement deleted replacement account")
			}
			if replacement == "receipt" && host.Status().Login.Username == "account" {
				t.Fatal("late logout failure attributed the previous user to a replacement receipt")
			}
			pending, err := cfg.PendingRemoteToken(gateway.URL, crypto.PublicBase64(id.Public()))
			if err != nil || pending.Token != "replacement" {
				t.Fatal("late acknowledgement deleted replacement proof")
			}
		})
	}
}

func TestNOSECLogoutReloadCancelsAndJoinsBeforePublishingNewHubAccount(t *testing.T) {
	entered := make(chan struct{})
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
	}))
	defer gateway.Close()
	e := testEngine(t)
	cfg := e.Config()
	installLogoutAccount(t, cfg, gateway.URL)
	host := New(e, cfg, nil)
	defer host.Stop()
	done := make(chan error, 1)
	go func() { done <- host.Logout() }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("remote logout did not start")
	}
	if err := cfg.Update(func(next *config.Config) error {
		next.Remote.HubURL = "https://replacement.example"
		next.Remote.Enabled = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	pending, err := cfg.StageRemoteToken("new", "https://replacement.example", "public")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ActivateRemoteToken(pending, config.RemoteCredential{Token: "new", Origin: "https://replacement.example", Authorized: true, HostID: "new-host", UserID: "new-user", Username: "replacement"}); err != nil {
		t.Fatal(err)
	}
	reloaded := make(chan struct{})
	go func() { host.Reload(); close(reloaded) }()
	select {
	case <-reloaded:
	case <-time.After(time.Second):
		t.Fatal("Reload failed to cancel and join remote logout")
	}
	if err := <-done; err == nil {
		t.Fatal("cancelled old logout reported success")
	}
	status := host.Status()
	if status.HubURL != "https://replacement.example" || status.Login.Username != "replacement" || status.Login.State != "authorized" || !status.HasToken {
		t.Fatalf("old logout overwrote replacement Hub account: %+v", status)
	}
}

func TestNOSECLogoutPartialRemoteSuccessAndLostAcknowledgementRemainRetryable(t *testing.T) {
	for _, failure := range []string{"gateway-error", "lost-receipt"} {
		t.Run(failure, func(t *testing.T) {
			var calls atomic.Int32
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 2 {
					if failure == "gateway-error" {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					w.Header().Set("Content-Length", "100")
					_, _ = io.WriteString(w, `{"data":`)
					return
				}
				_, _ = io.WriteString(w, `{"data":{"logged_out":true}}`)
			}))
			defer gateway.Close()
			e := testEngine(t)
			cfg := e.Config()
			installLogoutAccount(t, cfg, gateway.URL)
			id, err := loadIdentity(cfg)
			if err != nil {
				t.Fatal(err)
			}
			public := crypto.PublicBase64(id.Public())
			if _, err := cfg.StageRemoteToken("candidate-pending", gateway.URL, public); err != nil {
				t.Fatal(err)
			}
			host := New(e, cfg, nil)
			defer host.Stop()
			if err := host.Logout(); err == nil {
				t.Fatal("partial remote acknowledgement reported success")
			}
			if !cfg.HasHostToken() {
				t.Fatal("partial remote success removed the only retry credential")
			}
			pending, err := cfg.PendingRemoteToken(gateway.URL, public)
			if err != nil || pending.Token != "candidate-pending" {
				t.Fatal("partial failure discarded pending proof")
			}
			if err := host.Logout(); err != nil {
				t.Fatal(err)
			}
			assertSignedOut(t, host)
			if calls.Load() != 4 {
				t.Fatal("retry skipped a retained credential")
			}
		})
	}
}
