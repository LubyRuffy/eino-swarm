package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteCredentialFailuresRemainClosed(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(cfg.RemoteDir())
	if _, err := cfg.ReadRemoteCredential(); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.PendingRemoteToken("origin", "public"); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(cfg.RemoteDir(), 0700)
	_ = os.WriteFile(cfg.remoteTokenPath(), []byte("{"), 0600)
	if _, err := cfg.AuthorizedHostToken(); err == nil {
		t.Fatal("corrupt credential was authorized")
	}
	if err := cfg.BindRemoteOrigin("token", "origin"); err == nil {
		t.Fatal("corrupt credential acquired origin proof")
	}
	_ = os.WriteFile(cfg.remoteTokenPath(), []byte("token"), 0600)
	cfg.Remote.HubURL = "http://unsafe.example.invalid"
	if _, err := cfg.AuthorizedHostToken(); err == nil {
		t.Fatal("unsafe origin authorized")
	}
	if err := cfg.BindRemoteOrigin("different", "https://gateway.example.invalid"); err == nil {
		t.Fatal("wrong token acquired origin proof")
	}
	if err := cfg.BindRemoteOrigin("token", "https://gateway.example.invalid"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.BindRemoteOrigin("token", "https://gateway.example.invalid"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.BindRemoteOrigin("token", "https://another.example.invalid"); err == nil {
		t.Fatal("origin proof moved")
	}
	if err := cfg.ActivateRemoteToken(cfg.remoteTokenPath(), RemoteCredential{}); err == nil {
		t.Fatal("active path accepted as pending")
	}
	if err := cfg.ActivateRemoteToken(filepath.Join(cfg.RemoteDir(), "pending-host-token-missing"), RemoteCredential{}); err == nil {
		t.Fatal("missing pending proof accepted")
	}
	_ = os.Remove(cfg.remoteTokenPath())
	_ = os.WriteFile(cfg.remoteTokenPath(), []byte("private"), 0000)
	if _, err := privateRemoteRead(cfg.remoteTokenPath()); err == nil {
		t.Fatal("unreadable private credential accepted")
	}
	_ = os.Chmod(cfg.remoteTokenPath(), 0600)
	if err := syncRemoteDirectory(filepath.Join(cfg.RemoteDir(), "missing")); err == nil {
		t.Fatal("missing directory sync succeeded")
	}
	_ = os.Remove(cfg.remoteTokenPath())
	_ = os.Mkdir(cfg.remoteTokenPath(), 0700)
	if err := cfg.writeRemoteCredential(RemoteCredential{Token: "new"}); err == nil {
		t.Fatal("active directory was overwritten")
	}
	_ = os.Remove(cfg.remoteTokenPath())
	_ = os.Chmod(cfg.RemoteDir(), 0500)
	if _, err := cfg.StageRemoteToken("token", "origin", "public"); err == nil {
		t.Fatal("read-only directory staged credential")
	}
	if err := cfg.writeRemoteCredential(RemoteCredential{Token: "new"}); err == nil {
		t.Fatal("read-only directory activated credential")
	}
	_ = os.Chmod(cfg.RemoteDir(), 0700)
	if _, err := cfg.StageRemoteToken("token", strings.Repeat("x", 17<<10), "public"); err == nil {
		t.Fatal("oversized candidate staged")
	}
	_ = os.RemoveAll(cfg.RemoteDir())
	_ = os.WriteFile(cfg.RemoteDir(), []byte("not-directory"), 0600)
	if _, err := cfg.StageRemoteToken("token", "origin", "public"); err == nil {
		t.Fatal("non-directory candidate store accepted")
	}
	if err := cfg.writeRemoteCredential(RemoteCredential{Token: "new"}); err == nil {
		t.Fatal("non-directory active store accepted")
	}
}

func TestRemoteRetainedCandidatesRejectCorruptionAndOverflow(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cfg.RemoteDir(), "pending-host-token-corrupt")
	_ = os.WriteFile(path, []byte("{"), 0600)
	if _, err := cfg.PendingRemoteToken("origin", "public"); err == nil {
		t.Fatal("corrupt candidate accepted")
	}
	if _, err := cfg.StageRemoteToken("new", "origin", "public"); err == nil {
		t.Fatal("corrupt retained store ignored")
	}
	_ = os.Chmod(path, 0644)
	if _, err := cfg.PendingRemoteToken("origin", "public"); err == nil {
		t.Fatal("unsafe candidate accepted")
	}
	_ = os.Remove(path)
	for n := 0; n <= maxPendingRemoteCandidates; n++ {
		_ = os.WriteFile(filepath.Join(cfg.RemoteDir(), fmt.Sprintf("pending-host-token-%03d", n)), []byte(`{}`), 0600)
	}
	if _, err := cfg.PendingRemoteToken("origin", "public"); err == nil {
		t.Fatal("externally overflowing candidate store accepted")
	}
	_ = os.Chmod(cfg.RemoteDir(), 0000)
	if _, err := cfg.PendingRemoteToken("origin", "public"); err == nil {
		t.Fatal("unreadable candidate store accepted")
	}
	_ = os.Chmod(cfg.RemoteDir(), 0700)
}
