package config

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestRemoteLogoutTokenSnapshotIsScopedAndCannotClearReplacement(t *testing.T) {
	for _, replacement := range []string{"receipt", "proof"} {
		t.Run(replacement, func(t *testing.T) {
			cfg, err := Load(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			cfg.Remote.AuthMode = RemoteAuthAccount
			origin, public := cfg.Remote.HubURL, "public"
			old, err := cfg.StageRemoteToken("old", origin, public)
			if err != nil {
				t.Fatal(err)
			}
			if err := cfg.ActivateRemoteToken(old, RemoteCredential{Token: "old", Origin: origin, Authorized: true, HostID: "host", UserID: "user"}); err != nil {
				t.Fatal(err)
			}
			foreign, err := cfg.StageRemoteToken("foreign", "https://other.example", public)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := cfg.StageRemoteToken("other-device", origin, "another-public"); err != nil {
				t.Fatal(err)
			}
			tokens, err := cfg.RemoteAccountLogoutTokens(origin, public)
			if err != nil || !slices.Equal(tokens, []string{"old"}) {
				t.Fatalf("unsafe token snapshot: %v", err)
			}
			next, err := cfg.StageRemoteToken("replacement", origin, public)
			if err != nil {
				t.Fatal(err)
			}
			if replacement == "receipt" {
				if err := cfg.ActivateRemoteToken(next, RemoteCredential{Token: "replacement", Origin: origin, Authorized: true, HostID: "host-new", UserID: "user-new"}); err != nil {
					t.Fatal(err)
				}
			}
			if err := cfg.ClearRemoteAccountTokens(origin, public, tokens); err == nil {
				t.Fatal("stale logout snapshot deleted replacement authority")
			}
			for _, path := range []string{old, next, foreign} {
				if _, err := os.Stat(path); err != nil {
					t.Fatal("stale logout mutated private proofs")
				}
			}
		})
	}
}

func TestRemoteLogoutTokenSnapshotRejectsUnsafeStateAndAcknowledgesExactScope(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Remote.AuthMode = RemoteAuthAccount
	origin := cfg.Remote.HubURL
	if _, err := cfg.RemoteAccountLogoutTokens("https://changed.example", "public"); err == nil {
		t.Fatal("stale origin accepted")
	}
	if _, err := cfg.StageRemoteToken("one", origin, "public"); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.StageRemoteToken("two", origin, "public"); err != nil {
		t.Fatal(err)
	}
	tokens, err := cfg.RemoteAccountLogoutTokens(origin, "public")
	if err != nil || len(tokens) != 2 {
		t.Fatal("current candidates missing")
	}
	if err := cfg.ClearRemoteAccountTokens(origin, "public", tokens); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.StageRemoteToken(strings.Repeat("x", 257), origin, "public"); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.RemoteAccountLogoutTokens(origin, "public"); err == nil {
		t.Fatal("oversized credential entered Authorization header")
	}
}
