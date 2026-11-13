package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/pairlink/crypto"
)

func TestALaterLoginKeepsTheSavedAvatarWhenTheNewAddressIsOmitted(t *testing.T) {
	const kept = "https://images.fofa.info/kept.png?v=1"
	for _, tc := range []struct {
		name   string
		userID string
		avatar string
		want   string
	}{
		{name: "omitted", userID: strings.Repeat("b", 32), want: kept},
		{name: "unsafe", userID: strings.Repeat("b", 32), avatar: "javascript:alert(1)", want: kept},
		{name: "other-account", userID: strings.Repeat("c", 32), want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
					w.WriteHeader(http.StatusCreated)
					_ = json.NewEncoder(w).Encode(map[string]any{"data": loginStartResponse{DeviceCode: strings.Repeat("d", 64), UserCode: "ABCD-EFGH", VerificationURI: origin + desktopVerificationFixturePath + "?desktop_code=ABCD-EFGH", ExpiresAt: time.Now().Add(9 * time.Minute), Interval: 5}})
				case desktopAuthPrefix + "poll":
					user := map[string]string{"id": tc.userID, "username": "nosec_alvaro_0123456789abcdef", "display_name": "Álvaro García", "email": "private@example.test", "key": "private-nosec-key"}
					if tc.avatar != "" {
						user["avatar_url"] = tc.avatar
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"status": "authorized", "host_id": strings.Repeat("a", 32), "user": user}})
				default:
					http.NotFound(w, r)
				}
			}))
			origin = gateway.URL
			defer gateway.Close()
			e := testEngine(t)
			cfg := e.Config()
			cfg.Remote.Enabled = true
			cfg.Remote.AuthMode = config.RemoteAuthAccount
			cfg.Remote.HubURL = origin
			path, err := cfg.StageRemoteToken(strings.Repeat("c", 64), origin, "seed-key")
			if err != nil {
				t.Fatal(err)
			}
			if err := cfg.ActivateRemoteToken(path, config.RemoteCredential{
				Token: strings.Repeat("c", 64), Origin: origin, Authorized: true,
				HostID: strings.Repeat("a", 32), UserID: strings.Repeat("b", 32), Username: "nosec_alvaro_0123456789abcdef",
				DisplayName: "Old Name", AvatarURL: kept,
			}); err != nil {
				t.Fatal(err)
			}
			host := New(e, cfg, nil)
			host.loginWait = func(ctx context.Context, _ time.Duration) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
					return nil
				}
			}
			defer host.Stop()
			if err := host.StartLogin(func(string) error { return nil }, false); err != nil {
				t.Fatal(err)
			}
			waitAccountLogin(t, func() bool {
				saved, err := cfg.ReadRemoteCredential()
				return err == nil && saved.DisplayName == "Álvaro García"
			})
			saved, err := cfg.ReadRemoteCredential()
			if err != nil {
				t.Fatal(err)
			}
			if saved.DisplayName != "Álvaro García" || saved.AvatarURL != tc.want || host.Status().Login.AvatarURL != tc.want {
				t.Fatalf("avatar saved %q status %q name %q", saved.AvatarURL, host.Status().Login.AvatarURL, saved.DisplayName)
			}
			raw, _ := json.Marshal(saved)
			if strings.Contains(string(raw), "private@example.test") || strings.Contains(string(raw), "private-nosec-key") || strings.Contains(string(raw), "javascript:") {
				t.Fatalf("secret or rejected address stored: %s", raw)
			}
		})
	}
}
