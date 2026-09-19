package remote

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/pairlink/client"
	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/qr"
	"github.com/LubyRuffy/pairlink/relay"
	pstore "github.com/LubyRuffy/pairlink/store"
)

func TestUDPBlockedListAndSendStayOnRelay(t *testing.T) {
	hubStore := pstore.NewMemory()
	hub := relay.New(hubStore)
	srv := httptest.NewServer(hub.Handler())
	t.Cleanup(srv.Close)

	e := testEngine(t)
	if _, err := e.CreateProject("p", "", "", true); err != nil {
		t.Fatal(err)
	}
	th, err := e.CreateThread("listed", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = e.Store().UpdateThread(th.ID, map[string]any{"title": "listed"})

	ctx := context.Background()
	token, err := relay.IssueHostToken(ctx, hubStore)
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	devID, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.RegisterHost(ctx, srv.URL, token, hostID); err != nil {
		t.Fatal(err)
	}
	hostC, err := client.Dial(ctx, client.Config{
		HubURL: srv.URL, Identity: hostID, Token: token, DisableUDP: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hostC.Close() })
	hostC.OnLink(func(l *client.Link) {
		go func() {
			for msg := range l.Recv() {
				var req Request
				if err := json.Unmarshal(msg, &req); err != nil {
					continue
				}
				resp := Handle(e, config.RemoteConfig{ThreadLimit: 5, SummaryChars: 40, OpenTurns: 3}, req, l.Path(), l.SessionID())
				raw, _ := json.Marshal(resp)
				_ = l.Send(raw)
			}
		}()
	})
	uri, _, err := hostC.CreateOffer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	png, err := qr.PNG(uri, 256)
	if err != nil {
		t.Fatal(err)
	}
	gotURI, err := qr.DecodePNG(png)
	if err != nil {
		t.Fatal(err)
	}
	if gotURI != uri {
		t.Fatalf("qr roundtrip\ngot  %q\nwant %q", gotURI, uri)
	}
	offer, err := protocol.Parse(gotURI)
	if err != nil {
		t.Fatal(err)
	}
	ticket, hostPub, sid, err := client.RedeemOffer(ctx, offer, devID)
	if err != nil {
		t.Fatal(err)
	}
	devC, err := client.Dial(ctx, client.Config{
		HubURL: offer.HubURL, Identity: devID, Token: ticket, DisableUDP: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = devC.Close() })
	devL, err := devC.OpenInitiator(ctx, hostPub, sid)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "a", Op: OpList})
	if err := devL.Send(raw); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-devL.Recv():
		var resp Response
		if err := json.Unmarshal(got, &resp); err != nil {
			t.Fatal(err)
		}
		if !resp.OK || resp.Path != protocol.PathRelay {
			t.Fatalf("%+v", resp)
		}
		found := false
		for _, tv := range resp.Threads {
			if tv.ID == th.ID {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing thread in %+v", resp.Threads)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("list timeout")
	}
	send, _ := json.Marshal(Request{V: ProtocolV, ID: "b", Op: OpSend, ThreadID: th.ID, Text: "hello from phone"})
	if err := devL.Send(send); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-devL.Recv():
		var resp Response
		if err := json.Unmarshal(got, &resp); err != nil {
			t.Fatal(err)
		}
		if !resp.OK {
			t.Fatalf("send %+v", resp)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("send timeout")
	}
	if devL.Path() != protocol.PathRelay {
		t.Fatalf("path %s", devL.Path())
	}
}
