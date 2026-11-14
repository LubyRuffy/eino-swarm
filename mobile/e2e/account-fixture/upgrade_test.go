package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/remote"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/pairlink/client"
	linkcrypto "github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/relay"
	pstore "github.com/LubyRuffy/pairlink/store"
)

func realUpgradeFixture(t *testing.T, devicePub []byte) *fixture {
	t.Helper()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{review: "approved", api: false, showPC: true, counts: map[string]int{}, bindings: newFixtureBindings(),
		flow: &flow{Authorized: true, Candidate: "agm_" + strings.Repeat("f", 64), DevicePub: linkcrypto.PublicBase64(devicePub), Expires: time.Now().Add(time.Hour)}}
	f.relay = relay.New(f.bindings)
	f.hub = f.relay.Handler()
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	f.origin, f.anonymous = server.URL, server.URL
	// The fixture PC uses anonymous relay pairing before any account flow.
	cfg.Product.Mode = config.ProductModeLocal
	cfg.Remote.AuthMode = config.RemoteAuthIndependent
	cfg.Remote.Enabled, cfg.Remote.HubURL, cfg.Remote.DisplayName, cfg.Remote.KeepAwake = true, f.origin, "Native PC", false
	token, err := relay.IssueHostToken(t.Context(), f.bindings)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.WriteHostToken(token); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng := engine.New(cfg, db, provider.NewMock(cfg), logger)
	t.Cleanup(eng.Shutdown)
	f.engine = eng
	if _, err := eng.CreateThread("Native PC conversation", "", ""); err != nil {
		t.Fatal(err)
	}
	f.host = remote.New(eng, cfg, logger)
	t.Cleanup(func() { f.host.Stop(); f.relay.ClosePeers() })
	f.host.Start()
	identity, err := cfg.RemoteIdentity()
	if err != nil {
		t.Fatal(err)
	}
	key, err := linkcrypto.FromPrivate(identity)
	if err != nil {
		t.Fatal(err)
	}
	f.hostPub = key.Public()
	return f
}

func fixtureJSON(t *testing.T, f *fixture, method, path string, body any, authorized bool) map[string]json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, f.origin+path, strings.NewReader(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	if authorized {
		req.Header.Set("Authorization", "Bearer "+f.flow.Candidate)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("fixture %s returned %d", path, response.StatusCode)
	}
	var result map[string]json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestFixtureAnonymousUpgradeUsesSameIdentityAndRetiresOnlyThatPhone(t *testing.T) {
	phone, err := linkcrypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	f := realUpgradeFixture(t, phone.Public())
	// The existing native fixture defaults to a second Hub listener. This case
	// explicitly requests the primary authority, rather than conflating origins.
	f.anonymous = "http://127.0.0.1:9"
	listed := fixtureJSON(t, f, http.MethodGet, clientPrefix+"/link-hosts", nil, true)
	var hosts []map[string]any
	if err := json.Unmarshal(listed["data"], &hosts); err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0]["host_pub"] != linkcrypto.PublicBase64(f.hostPub) || hosts[0]["owner_user_id"] != "native-owner" {
		t.Fatal("owner discovery lacks the full public identity required to merge an anonymous PC")
	}
	response := fixtureJSON(t, f, http.MethodGet, "/fixture/anonymous-offer?origin=primary", nil, false)
	var offered struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(response["data"], &offered); err != nil {
		t.Fatal(err)
	}
	offer, err := protocol.Parse(offered.URI)
	if err != nil || offer.HubURL != f.origin {
		t.Fatal("same-Hub anonymous QR changed authority")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	oldTicket, oldPub, oldSID, err := client.RedeemOffer(ctx, offer, phone)
	if err != nil {
		t.Fatal(err)
	}
	old, err := client.Dial(ctx, client.Config{HubURL: f.origin, Identity: phone, Token: oldTicket, DisableUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	if _, err := old.OpenInitiator(ctx, oldPub, oldSID); err != nil {
		t.Fatal(err)
	}
	other, err := linkcrypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.bindings.PutBinding(ctx, pstore.Binding{ID: "other-phone", HostPub: f.hostPub, DevicePub: other.Public(), TicketHash: pstore.HashSecret("other-phone-secret")}); err != nil {
		t.Fatal(err)
	}
	connected := fixtureJSON(t, f, http.MethodPost, clientPrefix+"/link-hosts/native-pc/connect", map[string]string{"device_pub": linkcrypto.PublicBase64(phone.Public())}, true)
	var paired struct {
		Ticket  string `json:"ticket"`
		HostPub string `json:"host_pub"`
		Session string `json:"session_id"`
	}
	if err := json.Unmarshal(connected["data"], &paired); err != nil {
		t.Fatal(err)
	}
	if paired.Ticket == oldTicket {
		t.Fatal("account upgrade reused the anonymous ticket")
	}
	if _, err := f.bindings.BindingByTicketHash(ctx, pstore.HashSecret(oldTicket)); err == nil {
		t.Fatal("upgrade left the old anonymous binding active")
	}
	old.Close()
	pub, err := linkcrypto.ParsePublic(paired.HostPub)
	if err != nil {
		t.Fatal(err)
	}
	sid, err := hex.DecodeString(paired.Session)
	if err != nil {
		t.Fatal(err)
	}
	next, err := client.Dial(ctx, client.Config{HubURL: f.origin, Identity: phone, Token: paired.Ticket, DisableUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	link, err := next.OpenInitiator(ctx, pub, sid)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(remote.Request{V: remote.ProtocolV, ID: "upgraded", Op: remote.OpList})
	if err := link.Send(raw); err != nil {
		t.Fatal(err)
	}
	select {
	case reply := <-link.Recv():
		if !strings.Contains(string(reply), "Native PC conversation") {
			t.Fatal("new ticket did not reach the real encrypted PC RPC")
		}
	case <-ctx.Done():
		t.Fatal("new ticket failed the real Noise/RPC boundary")
	}
	fixtureJSON(t, f, http.MethodPost, clientPrefix+"/auth/logout", map[string]any{}, true)
	if _, err := f.bindings.BindingByTicketHash(ctx, pstore.HashSecret(paired.Ticket)); err == nil {
		t.Fatal("logout kept the upgraded account ticket active")
	}
	if _, err := f.bindings.BindingByTicketHash(ctx, pstore.HashSecret("other-phone-secret")); err != nil {
		t.Fatal("logout revoked another phone")
	}
	if !f.host.Status().Online {
		t.Fatal("phone logout disconnected the PC")
	}
	state := fixtureJSON(t, f, http.MethodGet, "/fixture/state", nil, false)
	var observed struct {
		Active     int  `json:"active_bindings"`
		Retired    int  `json:"retired_bindings"`
		HostOnline bool `json:"host_online"`
	}
	if err := json.Unmarshal(state["data"], &observed); err != nil {
		t.Fatal(err)
	}
	if observed.Active != 1 || observed.Retired != 2 || !observed.HostOnline {
		t.Fatal("fixture evidence did not reflect the real per-phone retirement and surviving PC")
	}
}
