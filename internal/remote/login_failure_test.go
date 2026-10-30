package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNOSECStartRejectsUnsafeConfigurationAndPrivateFiles(t *testing.T) {
	for _, scenario := range []string{"no-browser", "disabled", "legacy", "unsafe-origin", "identity-directory", "unsafe-token", "corrupt-pending", "unsafe-directory"} {
		t.Run(scenario, func(t *testing.T) {
			e := testEngine(t)
			cfg := e.Config()
			cfg.Remote.Enabled = true
			cfg.Remote.AuthMode = "nosec"
			cfg.Remote.HubURL = "https://gateway.example.invalid"
			open := func(string) error { return nil }
			switch scenario {
			case "no-browser":
				open = nil
			case "disabled":
				cfg.Remote.Enabled = false
			case "legacy":
				cfg.Remote.AuthMode = "legacy"
			case "unsafe-origin":
				cfg.Remote.HubURL = "http://gateway.example.invalid"
			case "identity-directory":
				_ = os.Mkdir(filepath.Join(cfg.RemoteDir(), "identity"), 0700)
			case "unsafe-token":
				_ = os.WriteFile(filepath.Join(cfg.RemoteDir(), "host_token"), []byte("private"), 0644)
			case "corrupt-pending":
				_ = os.WriteFile(filepath.Join(cfg.RemoteDir(), "pending-host-token-bad"), []byte("{"), 0600)
			case "unsafe-directory":
				_, _ = loadIdentity(cfg)
				_ = os.Chmod(cfg.RemoteDir(), 0755)
			}
			host := New(e, cfg, nil)
			defer host.Stop()
			if err := host.StartLogin(open, false); err == nil {
				t.Fatal("unsafe login configuration started a remote request")
			}
		})
	}
}

func TestNOSECLoginFailuresAndTerminalStatuses(t *testing.T) {
	for _, scenario := range []string{"unsafe-verification", "browser-unavailable", "unknown-status", "malformed-authorization", "denied", "expired", "deadline", "credential-failure", "poll-expiry", "poll-client-error"} {
		t.Run(scenario, func(t *testing.T) {
			var origin string
			var polls atomic.Int32
			gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/start") {
					expiry := time.Now().Add(time.Minute)
					if scenario == "poll-expiry" {
						expiry = time.Now().Add(10 * time.Millisecond)
					}
					uri := origin + desktopVerificationPath + "?desktop_code=PUBLIC"
					if scenario == "unsafe-verification" {
						uri = "https://foreign.example.invalid" + desktopVerificationPath + "?desktop_code=PUBLIC"
					}
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]any{"data": loginStartResponse{DeviceCode: strings.Repeat("d", 64), UserCode: "PUBLIC", VerificationURI: uri, ExpiresAt: expiry, Interval: 5}})
					return
				}
				polls.Add(1)
				if scenario == "poll-client-error" {
					http.Error(w, "invalid", 400)
					return
				}
				status := map[string]string{"unknown-status": "unknown", "malformed-authorization": "authorized", "denied": "access_denied", "expired": "expired", "credential-failure": "authorized"}[scenario]
				data := map[string]any{"status": status}
				if scenario == "credential-failure" {
					data["host_id"] = strings.Repeat("a", 32)
					data["user"] = map[string]string{"id": strings.Repeat("b", 32), "username": "approved"}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
			}))
			origin = gateway.URL
			defer gateway.Close()
			e := testEngine(t)
			cfg := e.Config()
			cfg.Remote.Enabled = true
			cfg.Remote.AuthMode = "nosec"
			cfg.Remote.HubURL = origin
			host := New(e, cfg, nil)
			defer host.Stop()
			host.loginWait = func(ctx context.Context, _ time.Duration) error {
				switch scenario {
				case "poll-expiry":
					return waitLogin(ctx, 20*time.Millisecond)
				case "deadline":
					<-ctx.Done()
					return ctx.Err()
				case "credential-failure":
					entries, _ := filepath.Glob(filepath.Join(cfg.RemoteDir(), "pending-host-token-*"))
					for _, path := range entries {
						_ = os.Remove(path)
					}
				}
				return nil
			}
			opener := func(string) error {
				if scenario == "browser-unavailable" {
					return errors.New("private browser failure")
				}
				return nil
			}
			if scenario == "deadline" {
				id, err := loadIdentity(cfg)
				if err != nil {
					t.Fatal(err)
				}
				candidate := strings.Repeat("c", 64)
				pending, err := cfg.StageRemoteToken(candidate, origin, "public")
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
				defer cancel()
				attempt := &loginAttempt{ctx: ctx, cancel: cancel, done: make(chan struct{}), origin: origin, candidate: candidate, pending: pending, verifier: strings.Repeat("v", 43), open: opener, cfg: cfg.Snapshot()}
				host.mu.Lock()
				host.id = id
				host.loginActive = attempt
				host.login = LoginStatus{State: "starting"}
				host.mu.Unlock()
				go host.runLogin(attempt)
			} else if err := host.StartLogin(opener, false); err != nil {
				t.Fatal(err)
			}

			want := "error"
			if scenario == "denied" {
				want = "access_denied"
			}
			if scenario == "expired" || scenario == "deadline" || scenario == "poll-expiry" {
				want = "expired"
			}
			waitNOSEC(t, func() bool { return host.Status().Login.State == want })
			if cfg.HasHostToken() {
				t.Fatal("failed device authorization activated token")
			}
		})
	}
}

func TestNOSECNetworkAndRateLimitRetryRemainBounded(t *testing.T) {
	var origin string
	var polls atomic.Int32
	var delays []time.Duration
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/start") {
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": loginStartResponse{DeviceCode: strings.Repeat("d", 64), UserCode: "PUBLIC", VerificationURI: origin + desktopVerificationPath + "?desktop_code=PUBLIC", ExpiresAt: time.Now().Add(time.Minute), Interval: 5}})
			return
		}
		switch polls.Add(1) {
		case 1:
			http.Error(w, "temporary", 500)
		case 2:
			w.Header().Set("Retry-After", "9")
			http.Error(w, "slow down", 429)
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"status": "access_denied"}})
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
	defer host.Stop()
	host.loginWait = func(_ context.Context, delay time.Duration) error { delays = append(delays, delay); return nil }
	if err := host.StartLogin(func(string) error { return nil }, false); err != nil {
		t.Fatal(err)
	}
	waitNOSEC(t, func() bool { return host.Status().Login.State == "access_denied" })
	host.mu.Lock()
	attempt := host.loginActive
	host.mu.Unlock()
	finishLogin(attempt)
	if polls.Load() != 3 || len(delays) != 3 || delays[2] != 9*time.Second {
		t.Fatal("Retry-After or retry bound not preserved")
	}
}
