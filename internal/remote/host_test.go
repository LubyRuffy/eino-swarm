package remote

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/pairlink/client"
	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/relay"
	pstore "github.com/LubyRuffy/pairlink/store"
)

func TestHostOfferAndServeLinkOverRelay(t *testing.T) {
	hubStore := pstore.NewMemory()
	hub := relay.New(hubStore)
	srv := httptest.NewServer(hub.Handler())
	t.Cleanup(srv.Close)

	ctx := context.Background()
	token, err := relay.IssueHostToken(ctx, hubStore)
	if err != nil {
		t.Fatal(err)
	}

	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled = true
	cfg.Remote.HubURL = srv.URL
	if err := cfg.WriteHostToken(token); err != nil {
		t.Fatal(err)
	}

	h := New(e, cfg, nil)
	h.Start()
	h.Start() // already has a conn; second start must be a no-op
	st := h.Status()
	if !st.Online || st.Fingerprint == "" || st.Error != "" {
		t.Fatalf("status %+v", st)
	}

	offer, err := h.Offer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if offer.URI == "" || len(offer.PNG) < 80 || offer.ExpiresAt.Before(time.Now()) {
		t.Fatalf("offer %+v png=%d", offer, len(offer.PNG))
	}

	parsed, err := protocol.Parse(offer.URI)
	if err != nil {
		t.Fatal(err)
	}
	devID, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	ticket, hostPub, sid, err := client.RedeemOffer(ctx, parsed, devID)
	if err != nil {
		t.Fatal(err)
	}
	devC, err := client.Dial(ctx, client.Config{
		HubURL: parsed.HubURL, Identity: devID, Token: ticket, DisableUDP: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = devC.Close() })
	link, err := devC.OpenInitiator(ctx, hostPub, sid)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "h", Op: OpList})
	if err := link.Send(raw); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-link.Recv():
		var resp Response
		if err := json.Unmarshal(got, &resp); err != nil {
			t.Fatal(err)
		}
		if !resp.OK {
			t.Fatalf("%+v", resp)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("list timeout")
	}
	if err := link.Send([]byte("not-json")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-link.Recv():
		var resp Response
		if err := json.Unmarshal(got, &resp); err != nil {
			t.Fatal(err)
		}
		if resp.OK || resp.Code != "bad_request" {
			t.Fatalf("junk %+v", resp)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("junk timeout")
	}

	binds, err := h.ListBindings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(binds) != 1 {
		t.Fatalf("bindings %+v", binds)
	}
	if err := h.RevokeBinding(ctx, binds[0].ID); err != nil {
		t.Fatal(err)
	}
	h.Reload()
	if h.Status().Online {
		// Reload stops then starts; hub still up so it should come back.
	}
	h.Stop()
	if h.Status().Online {
		t.Fatal("still online after stop")
	}
}

func TestHostStartWithoutHubURLStaysOffline(t *testing.T) {
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled = true
	h := New(e, cfg, nil)
	h.Start()
	st := h.Status()
	if st.Online || st.Error != "remote needs hub_url" {
		t.Fatalf("expected hub_url error %+v", st)
	}
	if st.HasToken {
		t.Fatal("must not mint a token without a hub")
	}
}

func TestHostStartTokenPathError(t *testing.T) {
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled = true
	cfg.Remote.HubURL = "http://127.0.0.1:9"
	if err := os.Mkdir(filepath.Join(cfg.RemoteDir(), "host_token"), 0o700); err != nil {
		t.Fatal(err)
	}
	h := New(e, cfg, nil)
	h.Start()
	if h.Status().Online || h.Status().Error == "" {
		t.Fatalf("expected token path error %+v", h.Status())
	}
}

func TestHostStartWithoutTokenStaysOffline(t *testing.T) {
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled = true
	cfg.Remote.HubURL = "http://127.0.0.1:9"
	h := New(e, cfg, nil)
	h.Start()
	st := h.Status()
	if st.Online || st.Error == "" {
		t.Fatalf("expected offline %+v", st)
	}
	if !st.HasToken {
		t.Fatal("unreachable hub must still mint a local host token")
	}
	if _, err := h.Offer(context.Background()); err == nil {
		t.Fatal("offer must fail offline")
	}
}

func TestLoadIdentityRemintsShortKey(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.WriteRemoteIdentity([]byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	id, err := loadIdentity(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(id.Public()) != 32 {
		t.Fatal("expected a new identity")
	}
}

func TestOfferAfterHubDies(t *testing.T) {
	hubStore := pstore.NewMemory()
	hub := relay.New(hubStore)
	srv := httptest.NewServer(hub.Handler())
	ctx := context.Background()
	token, err := relay.IssueHostToken(ctx, hubStore)
	if err != nil {
		t.Fatal(err)
	}
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled = true
	cfg.Remote.HubURL = srv.URL
	if err := cfg.WriteHostToken(token); err != nil {
		t.Fatal(err)
	}
	h := New(e, cfg, nil)
	h.Start()
	t.Cleanup(h.Stop)
	srv.Close()
	if _, err := h.Offer(ctx); err == nil {
		t.Fatal("offer must fail when the hub is gone")
	}
}
