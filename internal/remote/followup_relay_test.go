package remote

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/pairlink/client"
	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/relay"
	pstore "github.com/LubyRuffy/pairlink/store"
)

type followupRelay struct {
	link         *client.Link
	seen         []Response
	close        func()
	handlerReady chan followupRelayHandler
}

type followupRelayHandler struct {
	pump   *LinkPump
	exited <-chan struct{}
}

// This uses encrypted local relay frames and the production LinkPump, so
// a direct call to Handle cannot hide a missing RPC or watch dispatch.
func newFollowupRelay(t *testing.T, e *engine.Engine) *followupRelay {
	t.Helper()
	mem := pstore.NewMemory()
	hub := relay.New(mem)
	srv := httptest.NewServer(hub.Handler())
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	token, err := relay.IssueHostToken(ctx, mem)
	if err != nil {
		t.Fatal(err)
	}
	hostID, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	deviceID, err := crypto.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.RegisterHost(ctx, srv.URL, token, hostID); err != nil {
		t.Fatal(err)
	}
	host, err := client.Dial(ctx, client.Config{HubURL: srv.URL, Identity: hostID, Token: token, DisableUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	r := &followupRelay{handlerReady: make(chan followupRelayHandler, 1)}
	stop := make(chan struct{})
	var admission sync.Mutex
	var stopping bool
	var handlers sync.WaitGroup
	var closeOnce sync.Once
	r.close = func() {
		closeOnce.Do(func() {
			admission.Lock()
			stopping = true
			close(stop)
			admission.Unlock()
			host.OnLink(nil)
			_ = host.Close()
			handlers.Wait()
		})
	}
	t.Cleanup(r.close)
	host.OnLink(func(link *client.Link) {
		// Close does not join OnLink or close Recv. Seal admission before
		// waiting, including a callback the client captured before Close.
		admission.Lock()
		if stopping {
			admission.Unlock()
			return
		}
		handlers.Add(1)
		admission.Unlock()
		go func() {
			defer handlers.Done()
			exited := make(chan struct{})
			defer close(exited)
			pump := newLinkPump(e, config.RemoteConfig{EventChars: 400, SummaryChars: 80}, link)
			defer pump.Close()
			select {
			case r.handlerReady <- followupRelayHandler{pump: pump, exited: exited}:
			case <-stop:
				return
			}
			for {
				select {
				case <-stop:
					return
				case raw, ok := <-link.Recv():
					if !ok {
						return
					}
					pump.Dispatch(raw)
				}
			}
		}()
	})
	uri, _, err := host.CreateOffer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	offer, err := protocol.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	// Dial returns after the WebSocket handshake, before the relay necessarily
	// publishes presence. Redeem consumes the one-use offer even when offline.
	for !hub.PeerOnline(hostID.Public()) {
		select {
		case <-ctx.Done():
			t.Fatal("relay did not publish host presence:", ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	ticket, public, session, err := client.RedeemOffer(ctx, offer, deviceID)
	if err != nil {
		t.Fatal(err)
	}
	device, err := client.Dial(ctx, client.Config{HubURL: offer.HubURL, Identity: deviceID, Token: ticket, DisableUDP: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = device.Close() })
	link, err := device.OpenInitiator(ctx, public, session)
	if err != nil {
		t.Fatal(err)
	}
	r.link = link
	return r
}

// A later test may replace global watch hooks as soon as cleanup returns.
// Socket closure alone cannot establish that the previous pump stopped.
func TestFollowupRelayCleanupJoinsItsActiveWatch(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("relay cleanup", "", "")
	if err != nil {
		t.Fatal(err)
	}
	r := newFollowupRelay(t, e)
	if response := r.rpc(t, Request{ID: "watch-cleanup", Op: OpWatch, ThreadID: th.ID}); !response.OK || response.Op != OpReady {
		t.Fatalf("the real relay did not start its watch: %+v", response)
	}
	var handler followupRelayHandler
	select {
	case handler = <-r.handlerReady:
	case <-time.After(5 * time.Second):
		t.Fatal("the relay watch handler never started")
	}
	r.close()
	select {
	case <-handler.exited:
	default:
		t.Fatal("relay cleanup returned before its pump handler exited")
	}
	handler.pump.mu.Lock()
	watching, cancel := handler.pump.watching, handler.pump.cancel
	handler.pump.mu.Unlock()
	if watching != "" || cancel != nil {
		t.Fatal("relay cleanup left its watch active")
	}
}

func (r *followupRelay) receive(t *testing.T, deadline <-chan time.Time) Response {
	t.Helper()
	select {
	case raw, ok := <-r.link.Recv():
		if !ok {
			t.Fatal("pairlink closed before the expected response")
		}
		var got Response
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got.Path != protocol.PathRelay || got.SessionID != r.link.SessionID() {
			t.Fatalf("response lost real relay/session identity: %+v", got)
		}
		r.seen = append(r.seen, got)
		return got
	case <-deadline:
		t.Fatal("pairlink response timed out")
		return Response{}
	}
}

func (r *followupRelay) rpc(t *testing.T, req Request) Response {
	t.Helper()
	req.V = ProtocolV
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.link.Send(raw); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for {
		got := r.receive(t, deadline)
		if got.ID == req.ID {
			return got
		}
	}
}

func (r *followupRelay) userEvent(t *testing.T, text string) *EventView {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		for _, resp := range r.seen {
			if resp.Op == OpEvent && resp.Event != nil && resp.Event.Kind == engine.KindUser && resp.Event.Text == text {
				return resp.Event
			}
		}
		r.receive(t, deadline)
	}
}

func waitFollowupRelayIdle(t *testing.T, e *engine.Engine, threadID, turnID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		turn, err := e.Store().GetTurn(turnID)
		if err != nil {
			t.Fatal(err)
		}
		if turn.Status == store.TurnCancelled && !e.Status(threadID).Running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("Stop did not leave a cancelled, idle conversation")
}

func TestFollowupSteerOverPairlinkStartsOnlyTheSelectedStoppedRequest(t *testing.T) {
	t.Setenv("ZWAI_MOCK_WORKER_DELAY_MS", "1200")
	e := testEngine(t)
	th, err := e.CreateThread("stopped conversation", "", "")
	if err != nil {
		t.Fatal(err)
	}
	r := newFollowupRelay(t, e)
	watch := r.rpc(t, Request{ID: "watch", Op: OpWatch, ThreadID: th.ID})
	if !watch.OK || watch.Op != OpReady {
		t.Fatalf("watch did not reach LinkPump: %+v", watch)
	}
	start := r.rpc(t, Request{ID: "start", Op: OpSend, ThreadID: th.ID, Text: "original running request"})
	if !start.OK {
		t.Fatalf("start: %+v", start)
	}
	firstEvent := r.userEvent(t, "original running request")
	for _, req := range []Request{
		{ID: "queue-one", Op: OpSend, ThreadID: th.ID, Text: "send the selected request now"},
		{ID: "queue-two", Op: OpSend, ThreadID: th.ID, Text: "keep the other request waiting"},
	} {
		if got := r.rpc(t, req); !got.OK {
			t.Fatalf("queue: %+v", got)
		}
	}
	queued, err := e.ListFollowups(th.ID)
	if err != nil || len(queued) != 2 {
		t.Fatalf("queued: %+v err=%v", queued, err)
	}
	live := r.rpc(t, Request{ID: "queue-live-steer", Op: OpSend, ThreadID: th.ID, Text: "adjust the current request"})
	if !live.OK || live.Followups == nil || len(*live.Followups) != 3 {
		t.Fatalf("live followup was not queued: %+v", live)
	}
	liveID := (*live.Followups)[2].ID
	injected := r.rpc(t, Request{ID: "inject-live", Op: OpFollowupSteer, ThreadID: th.ID, FollowupID: liveID})
	if !injected.OK || injected.Followups == nil || len(*injected.Followups) != 2 {
		t.Fatalf("live followup injection changed other queued requests: %+v", injected)
	}
	liveEvents, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var liveSteer bool
	for _, ev := range liveEvents {
		if ev.Kind == engine.KindSteer && ev.Text == "adjust the current request" {
			liveSteer = true
			if ev.TurnID != firstEvent.TurnID {
				t.Fatalf("live followup steered another turn: %+v", ev)
			}
		}
	}
	if !liveSteer {
		t.Fatal("live followup did not persist a steer on the original turn")
	}
	if got := r.rpc(t, Request{ID: "stop-original", Op: OpStop, ThreadID: th.ID}); !got.OK {
		t.Fatalf("stop: %+v", got)
	}
	waitFollowupRelayIdle(t, e, th.ID, firstEvent.TurnID)
	retained, err := e.ListFollowups(th.ID)
	if err != nil || len(retained) != 2 || retained[0].ID != queued[0].ID || retained[1].ID != queued[1].ID {
		t.Fatalf("Stop changed the queue: %+v err=%v", retained, err)
	}
	steered := r.rpc(t, Request{ID: "resume-one", Op: OpFollowupSteer, ThreadID: th.ID, FollowupID: queued[0].ID})
	if !steered.OK || steered.Followups == nil || len(*steered.Followups) != 1 || (*steered.Followups)[0].ID != queued[1].ID {
		t.Fatalf("idle followup steer did not consume exactly one row: %+v", steered)
	}
	nextEvent := r.userEvent(t, queued[0].Text)
	if nextEvent.TurnID == "" || nextEvent.TurnID == firstEvent.TurnID || nextEvent.ThreadID != th.ID || nextEvent.Seq <= firstEvent.Seq {
		t.Fatalf("idle click did not start a new primary user turn: first=%+v next=%+v", firstEvent, nextEvent)
	}
	if got := r.rpc(t, Request{ID: "stop-resumed", Op: OpStop, ThreadID: th.ID}); !got.OK {
		t.Fatalf("stop resumed turn: %+v", got)
	}
	waitFollowupRelayIdle(t, e, th.ID, nextEvent.TurnID)
	turns, err := e.Store().ListTurns(th.ID)
	if err != nil || len(turns) != 2 || turns[1].ID != nextEvent.TurnID || turns[1].UserText != queued[0].Text {
		t.Fatalf("selected row created the wrong turn(s): %+v err=%v", turns, err)
	}
	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var selectedUserCount int
	for _, ev := range events {
		if ev.Text == queued[0].Text && ev.Kind == engine.KindUser {
			selectedUserCount++
			if ev.Seq != nextEvent.Seq || ev.TurnID != nextEvent.TurnID {
				t.Fatalf("live user event differs from persisted Trace: %+v vs %+v", nextEvent, ev)
			}
		}
		if ev.Kind == engine.KindSteer && ev.Text == queued[0].Text {
			t.Fatalf("idle followup was incorrectly injected into the cancelled turn: %+v", ev)
		}
	}
	if selectedUserCount != 1 {
		t.Fatalf("selected request persisted %d user events", selectedUserCount)
	}
	other, err := e.CreateThread("other conversation", "", "")
	if err != nil {
		t.Fatal(err)
	}
	otherRow, err := e.Store().EnqueueFollowup(other.ID, "another conversation's queued request")
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []Request{
		{ID: "already-consumed", Op: OpFollowupSteer, ThreadID: th.ID, FollowupID: queued[0].ID},
		{ID: "missing-row", Op: OpFollowupSteer, ThreadID: th.ID, FollowupID: "missing"},
		{ID: "missing-thread", Op: OpFollowupSteer, ThreadID: "missing", FollowupID: queued[1].ID},
		{ID: "foreign-row", Op: OpFollowupSteer, ThreadID: other.ID, FollowupID: queued[1].ID},
	} {
		got := r.rpc(t, req)
		if got.OK || got.Code != "not_found" {
			t.Fatalf("missing followup lost its RPC classification: %+v", got)
		}
	}
	if err := e.Store().DB().Exec(`CREATE TRIGGER refuse_relay_followup_delete BEFORE DELETE ON followups BEGIN SELECT RAISE(ABORT, 'relay followup delete unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	failed := r.rpc(t, Request{ID: "storage-failure", Op: OpFollowupSteer, ThreadID: th.ID, FollowupID: queued[1].ID})
	if failed.OK || failed.Code != "" || !strings.Contains(failed.Error, "relay followup delete unavailable") {
		t.Fatalf("storage error was hidden or became idle/not-found: %+v", failed)
	}
	remaining, err := e.ListFollowups(th.ID)
	if err != nil || len(remaining) != 1 || remaining[0].ID != queued[1].ID || remaining[0].Text != queued[1].Text {
		t.Fatalf("failed RPC consumed the unselected row: %+v err=%v", remaining, err)
	}
	turns, err = e.Store().ListTurns(th.ID)
	if err != nil || len(turns) != 2 || e.Status(th.ID).Running {
		t.Fatalf("failed RPC started more work: %+v err=%v", turns, err)
	}
	otherRows, err := e.ListFollowups(other.ID)
	if err != nil || len(otherRows) != 1 || otherRows[0].ID != otherRow.ID || otherRows[0].Text != otherRow.Text || e.Status(other.ID).Running {
		t.Fatalf("foreign request affected the other conversation: %+v err=%v", otherRows, err)
	}
}
