package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/LubyRuffy/pairlink/crypto"
)

func TestAccountLogoutJoinsRelayAndLinksAndCannotRestoreAfterReload(t *testing.T) {
	e := testEngine(t)
	r := newHostLinkRelay(t, e)
	phone := r.phone(t)
	phone.send(t, Request{ID: "hello", Op: OpHello})
	phone.receive(t, func(response Response) bool { return response.ID == "hello" })
	host, cfg := r.host, e.Config()
	credential, err := cfg.ReadRemoteCredential()
	if err != nil {
		t.Fatal(err)
	}
	public := crypto.PublicBase64(host.id.Public())
	path, err := cfg.StageRemoteToken(credential.Token, r.url, public)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ActivateRemoteToken(path, config.RemoteCredential{Token: credential.Token, Origin: r.url, Authorized: true, HostID: strings.Repeat("a", 32), UserID: strings.Repeat("b", 32), Username: "account"}); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Update(func(next *config.Config) error { next.Remote.AuthMode = config.RemoteAuthAccount; return nil }); err != nil {
		t.Fatal(err)
	}
	before := cfg.Snapshot().Remote
	host.mu.Lock()
	oldConn, oldLinks := host.conn, host.links
	host.mu.Unlock()
	if err := host.Logout(); err != nil {
		t.Fatal(err)
	}
	oldLinks.wg.Wait()
	if oldLinks.ctx.Err() == nil || oldConn.Connected() {
		t.Fatal("logout left relay or owned phone handlers active")
	}
	assertSignedOut(t, host)
	if cfg.Snapshot().Remote != before {
		t.Fatal("logout changed saved remote preferences")
	}
	if tools.DescribeWebExecution(cfg).CloudAccount != "sign_in_required" {
		t.Fatal("new cloud calls retained old account credential")
	}
	host.Reload()
	assertSignedOut(t, host)
	reloaded, err := config.Load(cfg.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	restarted := New(e, reloaded, nil)
	defer restarted.Stop()
	restarted.Start()
	assertSignedOut(t, restarted)
}

func TestAccountLogoutFencesLateApprovedPollAndRemovesOnlyCurrentProof(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var origin string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/logout") {
			_, _ = w.Write([]byte(`{"data":{"logged_out":true}}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/start") {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": loginStartResponse{DeviceCode: strings.Repeat("d", 64), UserCode: "PUBLIC", VerificationURI: origin + desktopVerificationFixturePath + "?desktop_code=PUBLIC", ExpiresAt: time.Now().Add(time.Minute), Interval: 5}})
			return
		}
		close(entered)
		<-release
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"status": "authorized", "host_id": strings.Repeat("a", 32), "user": map[string]string{"id": strings.Repeat("b", 32), "username": "late"}}})
	}))
	origin = gateway.URL
	defer gateway.Close()
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled, cfg.Remote.AuthMode, cfg.Remote.HubURL = true, config.RemoteAuthAccount, origin
	id, err := loadIdentity(cfg)
	if err != nil {
		t.Fatal(err)
	}
	public := crypto.PublicBase64(id.Public())
	foreign, err := cfg.StageRemoteToken("foreign", "https://other.example", public)
	if err != nil {
		t.Fatal(err)
	}
	otherDevice, err := cfg.StageRemoteToken("other-device", origin, "another-public")
	if err != nil {
		t.Fatal(err)
	}
	host := New(e, cfg, nil)
	host.loginWait = func(context.Context, time.Duration) error { return nil }
	defer host.Stop()
	if err := host.StartLogin(func(string) error { return nil }, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("poll did not start")
	}
	if err := host.Logout(); err != nil {
		t.Fatal(err)
	}
	close(release)
	assertSignedOut(t, host)
	pending, err := cfg.PendingRemoteToken(origin, public)
	if err != nil || pending.Token != "" {
		t.Fatal("logout retained current login proof")
	}
	for _, path := range []string{foreign, otherDevice} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("logout removed another origin/device proof")
		}
	}
}

func TestAccountLogoutFailureDoesNotClaimSuccessAndIndependentIsUntouched(t *testing.T) {
	e := testEngine(t)
	cfg := e.Config()
	if err := cfg.WriteHostToken("anonymous"); err != nil {
		t.Fatal(err)
	}
	host := New(e, cfg, nil)
	defer host.Stop()
	if err := host.Logout(); err == nil {
		t.Fatal("legacy logout was accepted")
	}
	if token, _ := cfg.HostToken(); token != "anonymous" {
		t.Fatal("legacy logout destroyed anonymous credential")
	}
	cfg.Remote.AuthMode = config.RemoteAuthAccount
	if err := os.WriteFile(filepath.Join(cfg.RemoteDir(), "host_token"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := host.Logout(); err == nil {
		t.Fatal("corrupt private storage claimed logout success")
	}
	if host.Status().Login.State != "error" || host.Status().Login.ErrorCode != "credential_unavailable" {
		t.Fatal("cleanup failure did not retain explicit failed state")
	}
}

func TestAccountLogoutReadsExistingDiskIdentityWithoutMintingAnother(t *testing.T) {
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.AuthMode = config.RemoteAuthAccount
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"data":{"logged_out":true}}`)) }))
	defer gateway.Close()
	cfg.Remote.HubURL = gateway.URL
	id, err := loadIdentity(cfg)
	if err != nil {
		t.Fatal(err)
	}
	public := crypto.PublicBase64(id.Public())
	if _, err := cfg.StageRemoteToken("pending", cfg.Remote.HubURL, public); err != nil {
		t.Fatal(err)
	}
	host := New(e, cfg, nil)
	defer host.Stop()
	if err := host.Logout(); err != nil {
		t.Fatal(err)
	}
	pending, err := cfg.PendingRemoteToken(cfg.Remote.HubURL, public)
	if err != nil || pending.Token != "" {
		t.Fatal("logout missed private proof for saved identity")
	}
	raw, err := cfg.RemoteIdentity()
	if err != nil || string(raw) != string(id.Private()) {
		t.Fatal("logout changed device identity")
	}
	if err := cfg.WriteRemoteIdentity([]byte("corrupt")); err != nil {
		t.Fatal(err)
	}
	if err := host.Logout(); err == nil {
		t.Fatal("unknown device scope claimed logout success")
	}
	cfg.Remote.HubURL = "http://unsafe.example"
	if err := host.Logout(); err == nil {
		t.Fatal("invalid current origin claimed logout success")
	}
}

func assertSignedOut(t *testing.T, host *Host) {
	t.Helper()
	status := host.Status()
	if status.Login == nil || status.Login.State != "signed_out" || status.HasToken || status.Online {
		t.Fatalf("account remained active after logout: %+v", status)
	}
}
