package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteCredentialReceiptIsPrivateAtomicAndOriginBound(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Remote.HubURL = "https://gateway.example.invalid"
	old := strings.Repeat("a", 64)
	candidate := strings.Repeat("b", 64)
	if err := cfg.WriteHostToken(old); err != nil {
		t.Fatal(err)
	}
	if err := cfg.BindRemoteOrigin(old, cfg.Remote.HubURL); err != nil {
		t.Fatal(err)
	}
	cfg.Remote.AuthMode = RemoteAuthNOSEC
	if _, err := cfg.EnsureHostToken(); !errors.Is(err, ErrRemoteApprovalRequired) {
		t.Fatal("legacy proof alone became account authorization")
	}
	staged, err := cfg.StageRemoteToken(candidate, cfg.Remote.HubURL, "public-device-key")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(staged)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("candidate is not private")
	}
	dirInfo, _ := os.Stat(cfg.RemoteDir())
	if dirInfo.Mode().Perm() != 0700 {
		t.Fatal("credential directory is not private")
	}
	pending, err := cfg.PendingRemoteToken(cfg.Remote.HubURL, "public-device-key")
	if err != nil || pending.Token != candidate {
		t.Fatal("retained candidate cannot reconcile lost poll")
	}
	untouched, err := cfg.ReadRemoteCredential()
	if err != nil || untouched.Token != old {
		t.Fatal("pending login replaced active credential")
	}
	receipt := RemoteCredential{Token: candidate, Origin: cfg.Remote.HubURL, Authorized: true, HostID: "host", UserID: "user", Username: "account"}
	wrong := receipt
	wrong.Token = old
	if err := cfg.ActivateRemoteToken(staged, wrong); err == nil {
		t.Fatal("unproven candidate activated")
	}
	if err := cfg.ActivateRemoteToken(staged, receipt); err != nil {
		t.Fatal(err)
	}
	token, err := cfg.EnsureHostToken()
	if err != nil || token != candidate {
		t.Fatal("approved receipt is not usable")
	}
	tokenInfo, _ := os.Stat(cfg.remoteTokenPath())
	if tokenInfo.Mode().Perm() != 0600 {
		t.Fatal("active receipt is not private")
	}
	cfg.Remote.HubURL = "https://another.example.invalid"
	if _, err := cfg.HostToken(); !errors.Is(err, ErrRemoteApprovalRequired) {
		t.Fatal("NOSEC credential crossed origin")
	}
	cfg.Remote.AuthMode = RemoteAuthLegacy
	if _, err := cfg.HostToken(); err == nil {
		t.Fatal("mode change released credential to another origin")
	}
	if err := cfg.RemovePendingRemoteToken(cfg.remoteTokenPath()); err == nil {
		t.Fatal("pending cleanup could delete active credential")
	}
	if err := cfg.RemovePendingRemoteToken(staged); err != nil {
		t.Fatal(err)
	}
	if err := cfg.RemovePendingRemoteToken(staged); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteOriginAcceptsOnlySecureCanonicalOrigins(t *testing.T) {
	for input, want := range map[string]string{"https://Gateway.Example:443/": "https://gateway.example", "http://127.0.0.1:80": "http://127.0.0.1", "http://[::1]:8080/": "http://[::1]:8080", "http://localhost:8081": "http://localhost:8081"} {
		got, err := RemoteOrigin(input)
		if err != nil || got != want {
			t.Errorf("origin %q = %q, %v", input, got, err)
		}
	}
	for _, input := range []string{"http://gateway.example", "https://user:pass@gateway.example", "https://gateway.example/path", "https://gateway.example?token=secret", "https://gateway.example#part", "file:///tmp/gateway", "//gateway.example", "https://gateway.example/%2f", "https://gateway.example:bad"} {
		if _, err := RemoteOrigin(input); err == nil {
			t.Errorf("unsafe origin accepted: %q", input)
		}
	}
}

func TestRemotePrivateCredentialRejectsUnsafeFilesAndMalformedRecords(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.RemoteDir(), 0700); err != nil {
		t.Fatal(err)
	}
	path := cfg.remoteTokenPath()
	for _, body := range []string{`{"schema":2,"token":"x"}`, `{"schema":1}`, `{`, strings.Repeat("x", (16<<10)+1)} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := cfg.ReadRemoteCredential(); err == nil {
			t.Fatal("malformed private credential accepted")
		}
	}
	if err := os.WriteFile(path, []byte("token"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.ReadRemoteCredential(); err == nil {
		t.Fatal("world-readable token accepted")
	}
	_ = os.Remove(path)
	target := filepath.Join(t.TempDir(), "private")
	_ = os.WriteFile(target, []byte("token"), 0600)
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.ReadRemoteCredential(); err == nil {
		t.Fatal("symlink credential accepted")
	}
	_ = os.Remove(path)
	if err := os.Chmod(cfg.RemoteDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.StageRemoteToken("candidate", "https://gateway.example", "public"); err == nil {
		t.Fatal("unsafe credential directory accepted")
	}
}

func TestRemoteAuthModeValidationPersistsAndRejectsUnknownMode(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Remote.AuthMode != RemoteAuthLegacy {
		t.Fatal("legacy mode default changed")
	}
	if err := cfg.Update(func(next *Config) error { next.Remote.AuthMode = RemoteAuthNOSEC; return nil }); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(cfg.DataDir())
	if err != nil || reloaded.Remote.AuthMode != RemoteAuthNOSEC {
		t.Fatal("NOSEC mode not persisted")
	}
	if err := cfg.Update(func(next *Config) error { next.Remote.AuthMode = "unknown"; return nil }); err == nil {
		t.Fatal("unknown auth mode accepted")
	}
}

func TestRemoteCandidateCapacityPreservesRetryWithoutUnboundedFiles(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	origin := "https://gateway.example.invalid"
	first, err := cfg.StageRemoteToken("candidate-0", origin, "public")
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := cfg.StageRemoteToken("candidate-0", origin, "public")
	if err != nil || duplicate != first {
		t.Fatal("retry duplicated retained private credential")
	}
	for n := 1; n < maxPendingRemoteCandidates; n++ {
		if _, err := cfg.StageRemoteToken(fmt.Sprintf("candidate-%d", n), origin, "public"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := cfg.StageRemoteToken("overflow", origin, "public"); err == nil {
		t.Fatal("unbounded candidate write accepted")
	}
	if _, err := cfg.PendingRemoteToken(origin, "public"); err != nil {
		t.Fatal("capacity blocked normal retry")
	}
	if reused, err := cfg.StageRemoteToken("candidate-0", origin, "public"); err != nil || reused != first {
		t.Fatal("capacity discarded existing recovery candidate")
	}
}

func TestRemoteActivationRejectsChangedSettings(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	origin := "https://gateway.example.invalid"
	path, err := cfg.StageRemoteToken("candidate", origin, "public")
	if err != nil {
		t.Fatal(err)
	}
	receipt := RemoteCredential{Token: "candidate", Origin: origin, Authorized: true, HostID: "host", UserID: "user"}
	if err := cfg.ActivateRemoteTokenIfCurrent(path, receipt); err == nil {
		t.Fatal("disabled legacy configuration activated receipt")
	}
	if err := cfg.Update(func(next *Config) error {
		next.Remote.Enabled = true
		next.Remote.AuthMode = RemoteAuthNOSEC
		next.Remote.HubURL = "https://other.example.invalid"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.ActivateRemoteTokenIfCurrent(path, receipt); err == nil {
		t.Fatal("changed origin activated late receipt")
	}
	if err := cfg.Update(func(next *Config) error { next.Remote.HubURL = origin; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := cfg.ActivateRemoteTokenIfCurrent(path, receipt); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteCandidateScopeCleanupAndProvenRejection(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	origin := "https://gateway.example.invalid"
	pub := "public"
	older, err := cfg.StageRemoteToken("older", origin, pub)
	if err != nil {
		t.Fatal(err)
	}
	current, err := cfg.StageRemoteToken("current", origin, pub)
	if err != nil {
		t.Fatal(err)
	}
	foreignOrigin, err := cfg.StageRemoteToken("foreign-origin", "https://other.example.invalid", pub)
	if err != nil {
		t.Fatal(err)
	}
	foreignPub, err := cfg.StageRemoteToken("foreign-pub", origin, "other-public")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetPendingRemoteRejected(current, true); err != nil {
		t.Fatal(err)
	}
	pending, err := cfg.PendingRemoteToken(origin, pub)
	if err != nil || pending.Token != "older" {
		t.Fatal("proven-rejected candidate shadowed uncertain recovery")
	}
	if err := cfg.SetPendingRemoteRejected(older, true); err != nil {
		t.Fatal(err)
	}
	pending, err = cfg.PendingRemoteToken(origin, pub)
	if err != nil || !pending.ProvenRejected {
		t.Fatal("definitive rejection was not durable")
	}
	if err := cfg.SetPendingRemoteRejected(current, false); err != nil {
		t.Fatal(err)
	}
	if err := cfg.RemoveRejectedRemoteScope(origin, pub); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(older); !os.IsNotExist(err) {
		t.Fatal("rejected proof not retired")
	}
	for _, path := range []string{current, foreignOrigin, foreignPub} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("scope cleanup removed uncertain or foreign proof")
		}
	}
	if err := cfg.CompleteRemoteTokenActivation(current, origin, pub); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(current); !os.IsNotExist(err) {
		t.Fatal("activated pending proof retained")
	}
	for _, path := range []string{foreignOrigin, foreignPub} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("successful cleanup crossed origin/public-key scope")
		}
	}
	if err := cfg.SetPendingRemoteRejected(cfg.remoteTokenPath(), true); err == nil {
		t.Fatal("rejection marker targeted active credential")
	}
	if err := cfg.CompleteRemoteTokenActivation(cfg.remoteTokenPath(), origin, pub); err == nil {
		t.Fatal("cleanup targeted active credential")
	}
}
