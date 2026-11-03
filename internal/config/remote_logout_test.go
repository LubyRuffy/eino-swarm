package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteLogoutClearsOnlyCurrentAccountOriginAndDeviceProof(t *testing.T) {
	for _, current := range []bool{true, false} {
		t.Run(map[bool]string{true: "current-active", false: "foreign-active"}[current], func(t *testing.T) {
			cfg, err := Load(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			origin, public := cfg.Remote.HubURL, "current-public"
			cfg.Remote.AuthMode = RemoteAuthNOSEC
			activeOrigin := origin
			if !current {
				activeOrigin = "https://other.example"
			}
			active, err := cfg.StageRemoteToken("active", activeOrigin, public)
			if err != nil {
				t.Fatal(err)
			}
			if err := cfg.ActivateRemoteToken(active, RemoteCredential{Token: "active", Origin: activeOrigin, Authorized: true, HostID: strings.Repeat("a", 32), UserID: strings.Repeat("b", 32)}); err != nil {
				t.Fatal(err)
			}
			own, err := cfg.StageRemoteToken("own-pending", origin, public)
			if err != nil {
				t.Fatal(err)
			}
			foreign, err := cfg.StageRemoteToken("foreign-pending", "https://other.example", public)
			if err != nil {
				t.Fatal(err)
			}
			otherDevice, err := cfg.StageRemoteToken("device-pending", origin, "other-public")
			if err != nil {
				t.Fatal(err)
			}
			if err := clearLogoutScope(cfg, origin, public); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(own); !os.IsNotExist(err) {
				t.Fatal("current proof survived logout")
			}
			for _, path := range []string{foreign, otherDevice} {
				if _, err := os.Stat(path); err != nil {
					t.Fatal("foreign proof was removed")
				}
			}
			credential, err := cfg.ReadRemoteCredential()
			if err != nil || current && credential.Token != "" || !current && credential.Token != "active" {
				t.Fatal("logout crossed active credential origin boundary")
			}
		})
	}
}

func TestRemoteLogoutPrivateStorageFailureRetainsActiveCredential(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Remote.AuthMode = RemoteAuthNOSEC
	origin := cfg.Remote.HubURL
	path, err := cfg.StageRemoteToken("active", origin, "public")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ActivateRemoteToken(path, RemoteCredential{Token: "active", Origin: origin, Authorized: true, HostID: "host", UserID: "user"}); err != nil {
		t.Fatal(err)
	}
	corrupt := filepath.Join(cfg.RemoteDir(), "pending-host-token-corrupt")
	if err := os.WriteFile(corrupt, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := clearLogoutScope(cfg, origin, "public"); err == nil {
		t.Fatal("corrupt private storage was silently ignored")
	}
	credential, err := cfg.ReadRemoteCredential()
	if err != nil || credential.Token != "active" {
		t.Fatal("failed preflight destroyed active credential")
	}
	if err := cfg.ClearRemoteAccountTokens("https://changed.example", "public", []string{"active"}); err == nil {
		t.Fatal("logout used stale origin")
	}
}

func TestRemoteLogoutRetainsProofsWhenPrivateDeletionFails(t *testing.T) {
	for _, active := range []bool{true, false} {
		t.Run(map[bool]string{true: "active-receipt", false: "pending-proof"}[active], func(t *testing.T) {
			cfg, err := Load(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			cfg.Remote.AuthMode = RemoteAuthNOSEC
			origin := cfg.Remote.HubURL
			path, err := cfg.StageRemoteToken("private", origin, "public")
			if err != nil {
				t.Fatal(err)
			}
			if active {
				if err := cfg.ActivateRemoteToken(path, RemoteCredential{Token: "private", Origin: origin, Authorized: true, HostID: "host", UserID: "user"}); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Chmod(cfg.RemoteDir(), 0500); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.Chmod(cfg.RemoteDir(), 0700); err != nil {
					t.Error(err)
				}
			}()
			if err := clearLogoutScope(cfg, origin, "public"); err == nil {
				t.Fatal("read-only private directory claimed logout success")
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("failed deletion removed recovery proof")
			}
			if active {
				credential, err := cfg.ReadRemoteCredential()
				if err != nil || credential.Token != "private" {
					t.Fatal("failed deletion lost active receipt")
				}
			}
		})
	}
}

func TestRemoteLogoutRejectsCorruptActiveCredentialAndAllowsEmptyScope(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Remote.AuthMode = RemoteAuthNOSEC
	if err := clearLogoutScope(cfg, cfg.Remote.HubURL, "public"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.WriteHostToken("{"); err != nil {
		t.Fatal(err)
	}
	if err := clearLogoutScope(cfg, cfg.Remote.HubURL, "public"); err == nil {
		t.Fatal("corrupt active credential claimed logout success")
	}
}

func clearLogoutScope(cfg *Config, origin, public string) error {
	tokens, err := cfg.RemoteAccountLogoutTokens(origin, public)
	if err != nil {
		return err
	}
	return cfg.ClearRemoteAccountTokens(origin, public, tokens)
}
