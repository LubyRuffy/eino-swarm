package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteDefaultsAndTokenStayOffYAML(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Remote.Enabled {
		t.Fatal("remote must start off")
	}
	if cfg.Remote.HubURL != "" {
		t.Fatal("hub url must be blank until configured")
	}
	if cfg.Remote.ThreadLimit != DefaultRemoteThreadLimit {
		t.Fatalf("thread limit %d", cfg.Remote.ThreadLimit)
	}
	if cfg.Remote.EventChars != DefaultRemoteEventChars {
		t.Fatalf("event chars %d", cfg.Remote.EventChars)
	}
	if _, err := os.Stat(cfg.RemoteDir()); err != nil {
		t.Fatal(err)
	}
	secret := "host-token-secret"
	if err := cfg.WriteHostToken(secret); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(cfg.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatal("token leaked into config.yaml")
	}
	got, err := cfg.HostToken()
	if err != nil || got != secret {
		t.Fatalf("token %q %v", got, err)
	}
	if !cfg.HasHostToken() {
		t.Fatal("expected token")
	}
	if err := cfg.WriteHostToken(""); err != nil {
		t.Fatal(err)
	}
	if cfg.HasHostToken() {
		t.Fatal("cleared token still present")
	}
}

func TestEnsureHostTokenMintsOnceAndStaysOffYAML(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "aigw__not-a-host-token")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := cfg.EnsureHostToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 {
		t.Fatalf("token len %d", len(first))
	}
	if strings.Contains(first, "aigw") {
		t.Fatal("host token must not be a gateway key")
	}
	for _, c := range first {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Fatalf("not hex %q", first)
		}
	}
	second, err := cfg.EnsureHostToken()
	if err != nil || second != first {
		t.Fatalf("reuse %q %q %v", first, second, err)
	}
	raw, err := os.ReadFile(cfg.Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), first) {
		t.Fatal("token leaked into config.yaml")
	}
}

func TestEnsureHostTokenDirectoryIsAnError(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(cfg.remoteTokenPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.EnsureHostToken(); err == nil {
		t.Fatal("directory must not mint")
	}
}

func TestEnsureHostTokenWriteFailure(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := cfg.RemoteDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if _, err := cfg.EnsureHostToken(); err == nil {
		t.Fatal("read-only remote dir must not mint")
	}
}

func TestRemoteIdentityRoundTrip(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	priv := make([]byte, 32)
	for i := range priv {
		priv[i] = byte(i + 1)
	}
	if err := cfg.WriteRemoteIdentity(priv); err != nil {
		t.Fatal(err)
	}
	got, err := cfg.RemoteIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 32 || got[0] != 1 {
		t.Fatalf("identity %x", got)
	}
	info, err := os.Stat(filepath.Join(cfg.RemoteDir(), remoteIdentityFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm %v", info.Mode().Perm())
	}
}

func TestRemoteNormalizeClampsAndTrims(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	dir := t.TempDir()
	raw := []byte("remote:\n  enabled: true\n  hub_url: \"  http://127.0.0.1:9  \"\n  thread_limit: -1\n  summary_chars: 0\n  open_turns: -2\n  event_chars: 0\n")
	if err := os.WriteFile(filepath.Join(dir, FileName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Remote.Enabled || cfg.Remote.HubURL != "http://127.0.0.1:9" {
		t.Fatalf("%+v", cfg.Remote)
	}
	if cfg.Remote.ThreadLimit != DefaultRemoteThreadLimit || cfg.Remote.SummaryChars != DefaultRemoteSummaryChars || cfg.Remote.OpenTurns != DefaultRemoteOpenTurns || cfg.Remote.EventChars != DefaultRemoteEventChars {
		t.Fatalf("clamped %+v", cfg.Remote)
	}
}

func TestHostTokenDirectoryIsAnError(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := cfg.remoteTokenPath()
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.HostToken(); err == nil {
		t.Fatal("expected error")
	}
	if cfg.HasHostToken() {
		t.Fatal("directory is not a token")
	}
}

func TestRemoteIdentityReadError(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(cfg.remoteIdentityPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.RemoteIdentity(); err == nil {
		t.Fatal("expected error")
	}
}
