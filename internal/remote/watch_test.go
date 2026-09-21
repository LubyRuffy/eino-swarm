package remote

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestPhoneSendIsVisibleOnSubscribe(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("dual", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	t.Cleanup(sub.Close)
	resp := Handle(e, config.RemoteConfig{}, Request{ID: "s", Op: OpSend, ThreadID: th.ID, Text: "from-phone"}, "relay", "s")
	if !resp.OK {
		t.Fatalf("%+v", resp)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-sub.C:
			if ev.Kind == "user_message" && strings.Contains(ev.Text, "from-phone") {
				return
			}
		case <-deadline:
			t.Fatal("desktop subscribe missed the phone send")
		}
	}
}

func TestWatchRejectsMissingAndUnknownThread(t *testing.T) {
	e := testEngine(t)
	pump, log := testPump(t, e)
	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "1", Op: OpWatch})
	pump.Dispatch(raw)
	missing := log.waitID(t, "1", 2*time.Second)
	if missing.OK || missing.Code != "bad_request" {
		t.Fatalf("missing %+v", missing)
	}
	raw, _ = json.Marshal(Request{V: ProtocolV, ID: "2", Op: OpWatch, ThreadID: "nope"})
	pump.Dispatch(raw)
	unknown := log.waitID(t, "2", 2*time.Second)
	if unknown.OK {
		t.Fatalf("unknown %+v", unknown)
	}
}

func TestWatchFailsWhenHistoryLoadFails(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("hist", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prev := readWatchHistory
	t.Cleanup(func() { readWatchHistory = prev })
	readWatchHistory = func(*LinkPump, string, int64) ([]store.Event, bool, error) {
		return nil, false, errors.New("history closed")
	}
	pump, log := testPump(t, e)
	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "w", Op: OpWatch, ThreadID: th.ID})
	pump.Dispatch(raw)
	got := log.waitID(t, "w", 2*time.Second)
	if got.OK {
		t.Fatalf("history %+v", got)
	}
}

func TestWatchFailsWhenStoreCloses(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("closed", "", "")
	if err != nil {
		t.Fatal(err)
	}
	pump, log := testPump(t, e)
	if err := e.Store().Close(); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "w", Op: OpWatch, ThreadID: th.ID})
	pump.Dispatch(raw)
	got := log.waitID(t, "w", 2*time.Second)
	if got.OK {
		t.Fatalf("closed store %+v", got)
	}
}

func TestWatchHighestNilStoreKeepsSince(t *testing.T) {
	if watchHighest(0, nil, true, nil, "t") != 0 {
		t.Fatal("nil store")
	}
	if watchHighest(4, nil, false, nil, "t") != 4 {
		t.Fatal("since without more")
	}
	e := testEngine(t)
	th, err := e.CreateThread("hi", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, Kind: "user_message", Text: "m",
	}); err != nil {
		t.Fatal(err)
	}
	if watchHighest(9, nil, true, e.Store(), th.ID) != 9 {
		t.Fatal("tail older than since")
	}
	if err := e.Store().Close(); err != nil {
		t.Fatal(err)
	}
	if watchHighest(0, nil, true, e.Store(), th.ID) != 0 {
		t.Fatal("closed store")
	}
}

func TestWatchReplayMatchesEngineReplaySeq(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("watch", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := e.Store().AppendEvent(&store.Event{
			ThreadID: th.ID, Kind: "user_message", Text: "m",
		}); err != nil {
			t.Fatal(err)
		}
	}
	replay, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	pump, log := testPump(t, e)
	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "w", Op: OpWatch, ThreadID: th.ID})
	pump.Dispatch(raw)
	ready := log.waitOp(t, OpReady, 3*time.Second)
	if ready.ID != "w" {
		t.Fatalf("watch RPC must return the snapshot, got id %q", ready.ID)
	}
	if ready.Seq != replay[len(replay)-1].Seq {
		t.Fatalf("ready seq %d replay %d", ready.Seq, replay[len(replay)-1].Seq)
	}
	seqs := seqsOf(ready.Events)
	if len(seqs) != len(replay) {
		t.Fatalf("ready events %v replay %d", seqs, len(replay))
	}
	for i, ev := range replay {
		if seqs[i] != ev.Seq {
			t.Fatalf("seq %d want %d", seqs[i], ev.Seq)
		}
	}
	if countOp(log.snapshot(), OpEvent) != 0 {
		t.Fatal("catch-up must ride ready, not a slideshow of event frames")
	}
}

func TestWatchLiveSendAndUnwatch(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("live", "", "")
	if err != nil {
		t.Fatal(err)
	}
	pump, log := testPump(t, e)
	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "w", Op: OpWatch, ThreadID: th.ID})
	pump.Dispatch(raw)
	log.waitOp(t, OpReady, 3*time.Second)
	send, _ := json.Marshal(Request{V: ProtocolV, ID: "s", Op: OpSend, ThreadID: th.ID, Text: "live-line"})
	pump.Dispatch(send)
	deadline := time.Now().Add(8 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("watch missed live send")
		}
		seen := false
		for _, r := range log.snapshot() {
			if r.Op == OpEvent && r.Event != nil && r.Event.Kind == "user_message" && strings.Contains(r.Event.Text, "live-line") {
				seen = true
			}
		}
		if seen {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	uw, _ := json.Marshal(Request{V: ProtocolV, ID: "u", Op: OpUnwatch})
	pump.Dispatch(uw)
	ok := log.waitID(t, "u", 2*time.Second)
	if !ok.OK {
		t.Fatalf("unwatch %+v", ok)
	}
}

func TestPushedEventFitsPairlinkFrame(t *testing.T) {
	ev := store.Event{Kind: "delta", Seq: 0, Text: strings.Repeat("x", 200000)}
	raw, ok := encodeEventPush("relay", "sess", "t", ev, config.RemoteConfig{EventChars: 4000, SummaryChars: 40})
	if !ok {
		t.Fatal("delta should clip rather than vanish when it still fits")
	}
	if len(raw) > MaxPushPayload {
		t.Fatalf("frame %d", len(raw))
	}
	huge := store.Event{Kind: "tool_delta", Seq: 0, Text: strings.Repeat("y", 200000)}
	raw, ok = encodeEventPush("relay", "sess", "t", huge, config.RemoteConfig{SummaryChars: 40})
	if !ok || len(raw) > MaxPushPayload {
		t.Fatalf("tool_delta %v %d", ok, len(raw))
	}
	if strings.Count(string(raw), "y") > 50 {
		t.Fatal("tool_delta preview was not one-line clipped")
	}
}

func TestSpawnedInstructionNeverLeavesTheHost(t *testing.T) {
	secret := "prompt-material-must-not-leave-the-pc"
	ev := store.Event{Kind: "spawned", Seq: 3, AgentID: "w1", Role: "researcher", Text: secret}
	view := eventView(ev, config.RemoteConfig{})
	if view.Text != "" {
		t.Fatalf("spawned text %q", view.Text)
	}
	raw, ok := encodeEventPush("relay", "s", "t", ev, config.RemoteConfig{})
	if !ok {
		t.Fatal("spawned must still push")
	}
	if strings.Contains(string(raw), secret) {
		t.Fatal("spawned instruction leaked")
	}
}

func TestPushKindsMatchDesktopStream(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "lib", "stream.ts"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	start := strings.Index(src, "export const KINDS = [")
	if start < 0 {
		t.Fatal("KINDS missing")
	}
	rest := src[start:]
	end := strings.Index(rest, "]")
	body := rest[:end]
	got := regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(body, -1)
	if len(got) != len(PushKinds) {
		t.Fatalf("desktop %d phone %d", len(got), len(PushKinds))
	}
	for i, m := range got {
		if m[1] != PushKinds[i] {
			t.Fatalf("kind %d %q vs %q", i, m[1], PushKinds[i])
		}
	}
}

func TestWatchMissingThreadAndHandleDoesNotWatch(t *testing.T) {
	e := testEngine(t)
	pump, log := testPump(t, e)
	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "w", Op: OpWatch, ThreadID: "nope"})
	pump.Dispatch(raw)
	resp := log.waitID(t, "w", 2*time.Second)
	if resp.OK || resp.Code != "not_found" {
		t.Fatalf("%+v", resp)
	}
	empty, _ := json.Marshal(Request{V: ProtocolV, ID: "e", Op: OpWatch})
	pump.Dispatch(empty)
	bad := log.waitID(t, "e", 2*time.Second)
	if bad.OK || bad.Code != "bad_request" {
		t.Fatalf("%+v", bad)
	}
	viaHandle := Handle(e, config.RemoteConfig{}, Request{ID: "h", Op: OpWatch, ThreadID: "x"}, "relay", "s")
	if viaHandle.OK || viaHandle.Code != "unknown_op" {
		t.Fatalf("Handle must not start a watch %+v", viaHandle)
	}
}

func TestCatchUpSkipsUnknownKindsAndClipsNewlines(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("cu", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, Kind: "user_message", Text: "line1\nline2",
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, Kind: "not_a_desktop_kind", Text: "ghost",
	}); err != nil {
		t.Fatal(err)
	}
	pump, log := testPump(t, e)
	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "w", Op: OpWatch, ThreadID: th.ID})
	pump.Dispatch(raw)
	ready := log.waitOp(t, OpReady, 3*time.Second)
	var texts []string
	var kinds []string
	for _, ev := range ready.Events {
		kinds = append(kinds, ev.Kind)
		texts = append(texts, ev.Text)
	}
	if strings.Join(kinds, ",") != "user_message" {
		t.Fatalf("kinds %v", kinds)
	}
	if texts[0] != "line1\nline2" {
		t.Fatalf("newlines %q", texts[0])
	}
	if countOp(log.snapshot(), OpEvent) != 0 {
		t.Fatal("unknown kinds must not become event frames")
	}
}

func TestWatchOpensAtTheLiveEdgeNotTheOldestEvent(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("tail", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if err := e.Store().AppendEvent(&store.Event{
			ThreadID: th.ID, Kind: "user_message", Text: "m" + string(rune('a'+i)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	pump, log := testPumpCfg(t, e, config.RemoteConfig{EventChars: 400, SummaryChars: 40, WatchEvents: 3})
	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "w", Op: OpWatch, ThreadID: th.ID})
	pump.Dispatch(raw)
	ready := log.waitOp(t, OpReady, 3*time.Second)
	texts := textsOfViews(ready.Events)
	if len(texts) != 3 || texts[0] != "mf" || texts[2] != "mh" {
		t.Fatalf("tail %+v", texts)
	}
	if ready.Seq != ready.Events[len(ready.Events)-1].Seq {
		t.Fatalf("ready %d last %d", ready.Seq, ready.Events[len(ready.Events)-1].Seq)
	}
	if !ready.More {
		t.Fatal("older rows must still exist")
	}
	if countOp(log.snapshot(), OpEvent) != 0 {
		t.Fatal("live-edge catch-up must not trickle as event frames")
	}
}

func TestWatchOpensOnTheLastTurnNotEarlierOnes(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("last-turn", "", "")
	if err != nil {
		t.Fatal(err)
	}
	old := &store.Turn{ThreadID: th.ID, Status: store.TurnDone}
	if err := e.Store().CreateTurn(old); err != nil {
		t.Fatal(err)
	}
	live := &store.Turn{ThreadID: th.ID, Status: store.TurnRunning}
	if err := e.Store().CreateTurn(live); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := e.Store().AppendEvent(&store.Event{
			ThreadID: th.ID, TurnID: old.ID, Kind: "user_message", Text: "old",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: live.ID, Kind: "user_message", Text: "now",
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: live.ID, Kind: "agent_message", Text: "ok",
	}); err != nil {
		t.Fatal(err)
	}
	pump, log := testPump(t, e)
	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "w", Op: OpWatch, ThreadID: th.ID})
	pump.Dispatch(raw)
	ready := log.waitOp(t, OpReady, 3*time.Second)
	if !ready.More {
		t.Fatal("earlier turn must still exist")
	}
	if strings.Join(textsOfViews(ready.Events), ",") != "now,ok" {
		t.Fatalf("last turn %+v", textsOfViews(ready.Events))
	}
	if countOp(log.snapshot(), OpEvent) != 0 {
		t.Fatal("last-turn catch-up must not trickle as event frames")
	}
}

func TestWatchJunkJSONAndCatchUpFromSince(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("since", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err := e.Store().AppendEvent(&store.Event{
			ThreadID: th.ID, Kind: "agent_message", Text: "a",
		}); err != nil {
			t.Fatal(err)
		}
	}
	pump, log := testPump(t, e)
	pump.Dispatch([]byte("not-json"))
	junk := log.waitOp(t, "", 2*time.Second)
	if junk.OK || junk.Code != "bad_request" {
		t.Fatalf("junk %+v", junk)
	}
	replay, err := e.Replay(th.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "w", Op: OpWatch, ThreadID: th.ID, Since: 2})
	pump.Dispatch(raw)
	ready := log.waitOp(t, OpReady, 3*time.Second)
	if ready.Seq != replay[len(replay)-1].Seq {
		t.Fatalf("since ready %d replay %d", ready.Seq, replay[len(replay)-1].Seq)
	}
	if len(ready.Events) != len(replay) {
		t.Fatalf("ready events %d replay %d", len(ready.Events), len(replay))
	}
	if countOp(log.snapshot(), OpEvent) != 0 {
		t.Fatal("since catch-up must ride ready, not event frames")
	}
}

func testPump(t *testing.T, e *engine.Engine) (*LinkPump, *pushLog) {
	return testPumpCfg(t, e, config.RemoteConfig{EventChars: 400, SummaryChars: 40})
}

func testPumpCfg(t *testing.T, e *engine.Engine, cfg config.RemoteConfig) (*LinkPump, *pushLog) {
	t.Helper()
	log := &pushLog{}
	pump := &LinkPump{
		eng:       e,
		cfg:       cfg,
		path:      "relay",
		sessionID: "sess",
		send: func(b []byte) error {
			var r Response
			if err := json.Unmarshal(b, &r); err != nil {
				t.Errorf("push json: %v", err)
				return err
			}
			log.append(r)
			return nil
		},
	}
	t.Cleanup(pump.Close)
	return pump, log
}

type pushLog struct {
	mu  sync.Mutex
	got []Response
}

func (p *pushLog) append(r Response) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.got = append(p.got, r)
}

func (p *pushLog) snapshot() []Response {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Response, len(p.got))
	copy(out, p.got)
	return out
}

func (p *pushLog) waitOp(t *testing.T, op string, d time.Duration) Response {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		for _, r := range p.snapshot() {
			if op == "" {
				return r
			}
			if r.Op == op {
				return r
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for op %q got %+v", op, p.snapshot())
	return Response{}
}

func (p *pushLog) waitID(t *testing.T, id string, d time.Duration) Response {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		for _, r := range p.snapshot() {
			if r.ID == id {
				return r
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for id %q", id)
	return Response{}
}

func TestEventCharsAndSummaryDefaults(t *testing.T) {
	if eventChars(config.RemoteConfig{}) != config.DefaultRemoteEventChars {
		t.Fatal("event chars default")
	}
	if summaryChars(config.RemoteConfig{}) != config.DefaultRemoteSummaryChars {
		t.Fatal("summary chars default")
	}
	if eventChars(config.RemoteConfig{EventChars: 9}) != 9 {
		t.Fatal("event chars override")
	}
	if watchEvents(config.RemoteConfig{}) != config.DefaultRemoteWatchEvents {
		t.Fatal("watch events default")
	}
	if watchEvents(config.RemoteConfig{WatchEvents: 12}) != 12 {
		t.Fatal("watch events override")
	}
}

func TestShouldPush(t *testing.T) {
	if !shouldPush("user_message") || shouldPush("not_a_desktop_kind") {
		t.Fatal("kind filter")
	}
}

func TestEventViewMarksImagesWithoutBytes(t *testing.T) {
	ev := store.Event{
		Kind: "user_message", Seq: 1, Text: "see",
		Images: []store.ImageRef{{ID: "img1", Name: "secret-file.png", MIME: "image/png"}},
	}
	view := eventView(ev, config.RemoteConfig{})
	if !view.HasImages {
		t.Fatal("has_images")
	}
	raw, ok := encodeEventPush("relay", "s", "t", ev, config.RemoteConfig{})
	if !ok || strings.Contains(string(raw), "secret-file.png") {
		t.Fatalf("image name leaked ok=%v %s", ok, raw)
	}
}

func TestCatchUpClosedStore(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("closed", "", "")
	if err != nil {
		t.Fatal(err)
	}
	pump, _ := testPump(t, e)
	_ = e.Store().Close()
	highest := int64(0)
	if _, err := pump.catchUp(context.Background(), th.ID, &highest); err == nil {
		t.Fatal("expected replay error")
	}
}

func TestCatchUpStopsWhenTheWatchIsCancelled(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("cancel-cu", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if err := e.Store().AppendEvent(&store.Event{
			ThreadID: th.ID, Kind: "user_message", Text: "m",
		}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	pump, log := testPump(t, e)
	n := 0
	orig := pump.send
	pump.send = func(b []byte) error {
		n++
		if n == 1 {
			cancel()
		}
		return orig(b)
	}
	highest := int64(0)
	if _, err := pump.catchUp(ctx, th.ID, &highest); err == nil {
		t.Fatal("cancelled catch-up must stop")
	}
	if len(log.snapshot()) >= 20 {
		t.Fatalf("kept pushing after cancel: %d", len(log.snapshot()))
	}
}

func TestWatchCancelSkipsReady(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("skip-ready", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, Kind: "user_message", Text: "m",
	}); err != nil {
		t.Fatal(err)
	}
	pump, log := testPump(t, e)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sub := e.Subscribe(th.ID)
	pump.runWatch(ctx, sub, th.ID, 0)
	for _, r := range log.snapshot() {
		if r.Op == OpReady {
			t.Fatal("a cancelled watch must not ready")
		}
	}
}

func TestWatchReplaysAfterALaggedSubscriber(t *testing.T) {
	prev := lagCheckInterval
	lagCheckInterval = 5 * time.Millisecond
	t.Cleanup(func() { lagCheckInterval = prev })
	prevLag := watchLagged
	watchLagged = func(*engine.Subscription) bool { return true }
	t.Cleanup(func() { watchLagged = prevLag })

	e := testEngine(t)
	th, err := e.CreateThread("lag", "", "")
	if err != nil {
		t.Fatal(err)
	}
	pump, log := testPump(t, e)
	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "w", Op: OpWatch, ThreadID: th.ID})
	pump.Dispatch(raw)
	log.waitOp(t, OpReady, 3*time.Second)
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, Kind: "user_message", Text: "missed-while-lagged",
	}); err != nil {
		t.Fatal(err)
	}
	log.waitOp(t, OpLagged, 3*time.Second)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, r := range log.snapshot() {
			if r.Op == OpEvent && r.Event != nil && strings.Contains(r.Event.Text, "missed-while-lagged") {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("lagged catch-up must replay stored events")
}

func TestEncodeEventPushShrinksThenDropsOversizedEnvelope(t *testing.T) {
	cfg := config.RemoteConfig{EventChars: 80000}
	ev := store.Event{Kind: "agent_message", Seq: 7, Text: strings.Repeat("x", 80000)}
	raw, ok := encodeEventPush("relay", "s", "t", ev, cfg)
	if !ok || len(raw) > MaxPushPayload {
		t.Fatalf("shrink ok=%v n=%d", ok, len(raw))
	}
	usage := store.Event{Kind: "usage", Seq: 1, Text: "a\nb\n" + strings.Repeat("z", 100)}
	view := eventView(usage, config.RemoteConfig{EventChars: 20})
	if strings.Contains(view.Text, "\n") {
		t.Fatalf("usage should be one-line %q", view.Text)
	}
	mem := store.Event{Kind: "session_memory", Seq: 2, Text: strings.Repeat("m", 50)}
	if eventView(mem, config.RemoteConfig{EventChars: 8}).Text != strings.Repeat("m", 8)+"…" {
		t.Fatal("session_memory clip")
	}
	huge := strings.Repeat("p", MaxPushPayload)
	if _, ok := encodeEventPush(huge, "s", "t", store.Event{Kind: "delta", Seq: 0, Text: "x"}, cfg); ok {
		t.Fatal("seq0 oversized envelope must drop")
	}
	if _, ok := encodeEventPush(huge, "s", "t", store.Event{Kind: "spawned", Seq: 3, Text: ""}, cfg); ok {
		t.Fatal("stored oversized envelope must drop")
	}
	dot := store.Event{Kind: "agent_message", Seq: 1, Text: "…"}
	if _, ok := encodeEventPush(huge, "s", "t", dot, config.RemoteConfig{EventChars: 10}); ok {
		t.Fatal("ellipsis oversized must drop after clearing")
	}
}

func TestRunWatchFailsWhenStoreCloses(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("rw-closed", "", "")
	if err != nil {
		t.Fatal(err)
	}
	pump, log := testPump(t, e)
	sub := e.Subscribe(th.ID)
	if err := e.Store().Close(); err != nil {
		t.Fatal(err)
	}
	pump.runWatch(context.Background(), sub, th.ID, 0)
	for _, r := range log.snapshot() {
		if !r.OK {
			return
		}
	}
	t.Fatal("closed store must fail the watch")
}

func TestRunWatchStopsWhenSubscriptionCloses(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("subclose", "", "")
	if err != nil {
		t.Fatal(err)
	}
	pump, log := testPump(t, e)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	sub := e.Subscribe(th.ID)
	go pump.runWatch(ctx, sub, th.ID, 0)
	log.waitOp(t, OpReady, 3*time.Second)
	sub.Close()
	time.Sleep(50 * time.Millisecond)
}

func TestSecondWatchReplacesTheFirst(t *testing.T) {
	e := testEngine(t)
	a, err := e.CreateThread("a", "", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.CreateThread("b", "", "")
	if err != nil {
		t.Fatal(err)
	}
	pump, log := testPump(t, e)
	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "1", Op: OpWatch, ThreadID: a.ID})
	pump.Dispatch(raw)
	log.waitOp(t, OpReady, 3*time.Second)
	raw, _ = json.Marshal(Request{V: ProtocolV, ID: "2", Op: OpWatch, ThreadID: b.ID})
	pump.Dispatch(raw)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var last string
		for _, r := range log.snapshot() {
			if r.Op == OpReady {
				last = r.ThreadID
			}
		}
		if last == b.ID {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("second watch did not ready")
}

func TestWatchEmptyLastTurnReadyStillPages(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("empty-last", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, Status: store.TurnRunning}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, Kind: "user_message", Text: "prev",
	}); err != nil {
		t.Fatal(err)
	}
	pump, log := testPump(t, e)
	raw, _ := json.Marshal(Request{V: ProtocolV, ID: "w", Op: OpWatch, ThreadID: th.ID})
	pump.Dispatch(raw)
	ready := log.waitOp(t, OpReady, 3*time.Second)
	if ready.ID != "w" {
		t.Fatalf("watch id %q", ready.ID)
	}
	if !ready.More {
		t.Fatal("older rows must still be pageable")
	}
	if len(ready.Events) != 0 {
		t.Fatalf("empty last turn leaked %+v", textsOfViews(ready.Events))
	}
	if ready.Seq != 1 {
		t.Fatalf("cursor seq %d", ready.Seq)
	}
}

func TestPushEventDropsOversizedDelta(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("sz", "", "")
	if err != nil {
		t.Fatal(err)
	}
	pump, _ := testPump(t, e)
	pump.path = strings.Repeat("p", MaxPushPayload)
	if pump.pushEvent(th.ID, store.Event{Kind: "delta", Seq: 0, Text: "x"}) {
		t.Fatal("oversized delta must not count as delivered")
	}
	if !pump.pushEvent(th.ID, store.Event{Kind: "agent_message", Seq: 9, Text: ""}) {
		t.Fatal("stored events still count so catch-up can advance")
	}
	if pump.pushEvent(th.ID, store.Event{Kind: "not_a_desktop_kind", Seq: 0, Text: "x"}) {
		t.Fatal("unknown delta")
	}
}

func seqsOf(events []EventView) []int64 {
	out := make([]int64, len(events))
	for i, ev := range events {
		out[i] = ev.Seq
	}
	return out
}

func textsOfViews(events []EventView) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = ev.Text
	}
	return out
}

func countOp(got []Response, op string) int {
	n := 0
	for _, r := range got {
		if r.Op == op {
			n++
		}
	}
	return n
}
