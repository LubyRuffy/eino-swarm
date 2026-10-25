package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/pairlink/client"
	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/relay"
	pstore "github.com/LubyRuffy/pairlink/store"
)

type hostLinkRelay struct {
	host  *Host
	hub   *relay.Hub
	store *pstore.Memory
	url   string
}

type hostLinkPhone struct {
	conn    *client.Conn
	link    *client.Link
	id      *crypto.Identity
	ticket  string
	public  []byte
	session []byte
}

// Exercise Host's actual OnLink admission, not a fixture-owned pump loop.
func newHostLinkRelay(t *testing.T, e *engine.Engine) *hostLinkRelay {
	t.Helper()
	mem := pstore.NewMemory()
	hub := relay.New(mem)
	srv := httptest.NewServer(hub.Handler())
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	token, err := relay.IssueHostToken(ctx, mem)
	if err != nil {
		t.Fatal(err)
	}
	cfg := e.Config()
	cfg.Remote.Enabled, cfg.Remote.HubURL = true, srv.URL
	if err := cfg.WriteHostToken(token); err != nil {
		t.Fatal(err)
	}
	host := New(e, cfg, nil)
	host.Start()
	t.Cleanup(host.Stop)
	return &hostLinkRelay{host: host, hub: hub, store: mem, url: srv.URL}
}

func (r *hostLinkRelay) phone(t *testing.T) *hostLinkPhone {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	offer, err := r.host.Offer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := protocol.Parse(offer.URI)
	if err != nil {
		t.Fatal(err)
	}
	id, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	// Dial returning does not prove that the relay published host presence.
	for !r.hub.PeerOnline(r.host.id.Public()) {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	ticket, public, session, err := client.RedeemOffer(ctx, parsed, id)
	if err != nil {
		t.Fatal(err)
	}
	p := &hostLinkPhone{id: id, ticket: ticket, public: public, session: session}
	r.reconnect(t, p)
	return p
}

func (r *hostLinkRelay) reconnect(t *testing.T, p *hostLinkPhone) {
	t.Helper()
	if p.conn != nil {
		_ = p.conn.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := client.Dial(ctx, client.Config{HubURL: r.url, Identity: p.id, Token: p.ticket, DisableUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	link, err := conn.OpenInitiator(ctx, p.public, p.session)
	if err != nil {
		t.Fatal(err)
	}
	p.conn, p.link = conn, link
}

func (p *hostLinkPhone) send(t *testing.T, req Request) {
	t.Helper()
	req.V = ProtocolV
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.link.Send(raw); err != nil {
		t.Fatal(err)
	}
}

func (p *hostLinkPhone) receive(t *testing.T, match func(Response) bool) Response {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case raw := <-p.link.Recv():
			var got Response
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if match(got) {
				return got
			}
		case <-deadline:
			t.Fatal("production Host response timed out")
			return Response{}
		}
	}
}

func (p *hostLinkPhone) watch(t *testing.T, threadID string) {
	t.Helper()
	p.send(t, Request{ID: "watch", Op: OpWatch, ThreadID: threadID})
	got := p.receive(t, func(r Response) bool { return r.ID == "watch" })
	if !got.OK || got.Op != OpReady {
		t.Fatalf("watch: %+v", got)
	}
}

func (r *hostLinkRelay) dataFrames(t *testing.T, p *hostLinkPhone) int {
	t.Helper()
	trace, err := r.store.Trace(context.Background(), p.link.SessionID())
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, ev := range trace {
		if ev.Kind == "forward" && ev.PeerFP == crypto.Fingerprint(p.public) && ev.Note == fmt.Sprintf("type=%d", protocol.TypeData) {
			total++
		}
	}
	return total
}

func TestHostReconnectStopsOldWatchBeforeItCanSendAgain(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("reconnected watch", "", "")
	if err != nil {
		t.Fatal(err)
	}
	r := newHostLinkRelay(t, e)
	p := r.phone(t)
	var previous *hostLink
	for generation := 0; generation < 3; generation++ {
		if generation != 0 {
			r.reconnect(t, p)
		}
		p.watch(t, th.ID)
		if previous != nil {
			waitHostLinkExit(t, previous)
		}
		previous = currentHostLink(t, r.host, p)
	}
	before := r.dataFrames(t, p)
	if err := e.SetThreadGoal(th.ID, "a later standing objective"); err != nil {
		t.Fatal(err)
	}
	p.receive(t, func(resp Response) bool { return resp.Event != nil && resp.Event.Kind == engine.KindGoal })
	// A relay receipt sees obsolete ciphertext even though the fresh phone cannot
	// decrypt it. A screen-only assertion would silently miss the duplicate work.
	time.Sleep(100 * time.Millisecond)
	if got := r.dataFrames(t, p) - before; got != 1 {
		t.Fatalf("one event after two reconnects generated %d host ciphertext frames, want 1", got)
	}
}

func currentHostLink(t *testing.T, h *Host, p *hostLinkPhone) *hostLink {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.links == nil || h.links.current[string(p.id.Public())] == nil {
		t.Fatal("ready phone has no Host-owned link")
	}
	return h.links.current[string(p.id.Public())]
}

func waitHostLinkExit(t *testing.T, link *hostLink) {
	t.Helper()
	select {
	case <-link.done:
		if link.ctx.Err() == nil {
			t.Fatal("retired link lost its cancellation cause")
		}
	case <-time.After(time.Second):
		t.Fatal("obsolete serveLink did not finish closing its watch and catalog")
	}
}

func TestHostDifferentPhonesRetainIndependentWatches(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("independent phones", "", "")
	if err != nil {
		t.Fatal(err)
	}
	r := newHostLinkRelay(t, e)
	first, second := r.phone(t), r.phone(t)
	first.watch(t, th.ID)
	second.watch(t, th.ID)
	firstOld := currentHostLink(t, r.host, first)
	secondCurrent := currentHostLink(t, r.host, second)
	r.reconnect(t, first)
	first.watch(t, th.ID)
	waitHostLinkExit(t, firstOld)
	firstCurrent := currentHostLink(t, r.host, first)
	select {
	case <-secondCurrent.done:
		t.Fatal("reconnecting one device retired another device's watch")
	default:
	}
	beforeFirst, beforeSecond := r.dataFrames(t, first), r.dataFrames(t, second)
	if err := e.SetThreadGoal(th.ID, "independent later event"); err != nil {
		t.Fatal(err)
	}
	for _, phone := range []*hostLinkPhone{first, second} {
		phone.receive(t, func(resp Response) bool { return resp.Event != nil && resp.Event.Kind == engine.KindGoal })
	}
	if r.dataFrames(t, first)-beforeFirst != 1 || r.dataFrames(t, second)-beforeSecond != 1 {
		t.Fatal("the independent live watches did not each send exactly one event")
	}
	r.host.Stop()
	waitHostLinkExit(t, firstCurrent)
	waitHostLinkExit(t, secondCurrent)
}

func TestHostStopAndReloadCancelOwnedCatalogRequests(t *testing.T) {
	for _, action := range []string{"stop", "reload", "reconnect"} {
		t.Run(action, func(t *testing.T) {
			entered, exited := make(chan struct{}), make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
				close(entered)
				select {
				case <-req.Context().Done():
					close(exited)
				case <-release:
				}
			}))
			t.Cleanup(upstream.Close)
			t.Cleanup(func() { once.Do(func() { close(release) }) })
			e := catalogEngine(t, config.Provider{ID: "catalog", BaseURL: upstream.URL, Model: "saved"})
			r := newHostLinkRelay(t, e)
			p := r.phone(t)
			p.send(t, Request{ID: "hello-before", Op: OpHello})
			p.receive(t, func(r Response) bool { return r.ID == "hello-before" })
			oldLink := currentHostLink(t, r.host, p)
			r.host.mu.Lock()
			oldOwner := r.host.links
			r.host.mu.Unlock()
			p.send(t, Request{ID: "refresh", Op: OpRefreshCatalog})
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("refresh did not reach HTTP")
			}
			done := make(chan struct{})
			go func() {
				switch action {
				case "stop":
					r.host.Stop()
				case "reload":
					r.host.Reload()
				case "reconnect":
				}
				close(done)
			}()
			if action == "reconnect" {
				r.reconnect(t, p)
				p.send(t, Request{ID: "hello", Op: OpHello})
				p.receive(t, func(r Response) bool { return r.ID == "hello" })
			}
			select {
			case <-exited:
			case <-time.After(time.Second):
				t.Fatal(action + " left obsolete catalog HTTP request active")
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal(action + " did not finish cleanup")
			}
			waitHostLinkExit(t, oldLink)
			if action == "reload" {
				r.reconnect(t, p)
				p.send(t, Request{ID: "hello-reloaded", Op: OpHello})
				p.receive(t, func(r Response) bool { return r.ID == "hello-reloaded" })
				fresh := currentHostLink(t, r.host, p)
				// Simulate the dependency invoking an already-captured old callback.
				r.host.acceptLink(oldOwner, &client.Link{PeerPub: p.id.Public()})
				if got := currentHostLink(t, r.host, p); got != fresh {
					t.Fatal("late old-Conn callback replaced the new connection's link")
				}
			} else if action == "stop" {
				r.host.acceptLink(oldOwner, &client.Link{PeerPub: p.id.Public()})
				r.host.mu.Lock()
				resurrected := r.host.links != nil || r.host.conn != nil
				r.host.mu.Unlock()
				if resurrected {
					t.Fatal("late callback revived stopped Host")
				}
			}
		})
	}
}

func TestHostCancelledLinkAdmissionAndEarlyServeExitDoNotLeak(t *testing.T) {
	e := testEngine(t)
	h := New(e, e.Config(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	owner := &hostLinkGroup{ctx: ctx, cancel: cancel, current: make(map[string]*hostLink)}
	h.links = owner
	cancel()
	h.acceptLink(owner, &client.Link{PeerPub: []byte("cancelled-peer")})
	if len(owner.current) != 0 {
		t.Fatal("a cancelled owner admitted a late link")
	}
	active := &hostLink{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	owner.current["cancelled-peer"] = active
	owner.wg.Add(1)
	// Cancellation can win between admission and the scheduled goroutine's start.
	h.serveLink(owner, "cancelled-peer", active, nil, e.Config().Remote)
	waitHostLinkExit(t, active)
	if len(owner.current) != 0 {
		t.Fatal("a link cancelled before startup kept its ownership slot")
	}
	owner.wg.Wait()
}
