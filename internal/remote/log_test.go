package remote

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestLastTurnWindowSkipsOlderTurns(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("turns", "", "")
	if err != nil {
		t.Fatal(err)
	}
	first := &store.Turn{ThreadID: th.ID, Status: store.TurnDone}
	if err := e.Store().CreateTurn(first); err != nil {
		t.Fatal(err)
	}
	second := &store.Turn{ThreadID: th.ID, Status: store.TurnRunning}
	if err := e.Store().CreateTurn(second); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := e.Store().AppendEvent(&store.Event{
			ThreadID: th.ID, TurnID: first.ID, Kind: "user_message", Text: "old" + string(rune('a'+i)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: second.ID, Kind: "user_message", Text: "now",
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: second.ID, Kind: "agent_message", Text: "ok",
	}); err != nil {
		t.Fatal(err)
	}

	page, hasMore, err := lastTurnWindow(e.Store(), th.ID, 80)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMore {
		t.Fatal("older turn must still exist")
	}
	if len(page) != 2 || page[0].Text != "now" || page[1].Text != "ok" {
		t.Fatalf("last turn %+v", page)
	}
	for _, ev := range page {
		if strings.HasPrefix(ev.Text, "old") {
			t.Fatalf("older turn leaked %q", ev.Text)
		}
	}
}

func TestLastTurnWindowCapsAHugeTurnFromTheEnd(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("huge", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, Status: store.TurnRunning}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := e.Store().AppendEvent(&store.Event{
			ThreadID: th.ID, TurnID: turn.ID, Kind: "user_message", Text: "m" + string(rune('a'+i)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	page, hasMore, err := lastTurnWindow(e.Store(), th.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMore || len(page) != 2 || page[0].Text != "md" || page[1].Text != "me" {
		t.Fatalf("cap %+v hasMore=%v", textsOf(page), hasMore)
	}
}

func TestLastTurnWindowFallsBackToTailWithoutTurns(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("raw", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err := e.Store().AppendEvent(&store.Event{
			ThreadID: th.ID, Kind: "user_message", Text: "x",
		}); err != nil {
			t.Fatal(err)
		}
	}
	page, hasMore, err := lastTurnWindow(e.Store(), th.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMore || len(page) != 2 || page[0].Seq != 3 || page[1].Seq != 4 {
		t.Fatalf("fallback %+v hasMore=%v", page, hasMore)
	}
}

func TestLogPagesOlderEventsBeforeTheViewport(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("page", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := e.Store().AppendEvent(&store.Event{
			ThreadID: th.ID, Kind: "user_message", Text: "m" + string(rune('a'+i)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	resp := Handle(e, config.RemoteConfig{WatchEvents: 2}, Request{
		ID: "l", Op: OpLog, ThreadID: th.ID, Before: 4,
	}, "relay", "s")
	if !resp.OK || !resp.More || len(resp.Events) != 2 {
		t.Fatalf("log %+v", resp)
	}
	if resp.Events[0].Text != "mb" || resp.Events[1].Text != "mc" {
		t.Fatalf("page %+v", resp.Events)
	}
}

func TestLogSkipsAPageThePhoneWouldNotRender(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("skip", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, Kind: "user_message", Text: "keep",
	}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := e.Store().AppendEvent(&store.Event{
			ThreadID: th.ID, Kind: "not_a_desktop_kind", Text: "junk",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, Kind: "user_message", Text: "now",
	}); err != nil {
		t.Fatal(err)
	}
	resp := Handle(e, config.RemoteConfig{WatchEvents: 2}, Request{
		ID: "l", Op: OpLog, ThreadID: th.ID, Before: 4,
	}, "relay", "s")
	if !resp.OK || len(resp.Events) != 1 || resp.Events[0].Text != "keep" {
		t.Fatalf("skipped junk %+v", resp)
	}
}

func TestLogRejectsUnknownThreadAndMissingThread(t *testing.T) {
	e := testEngine(t)
	missing := Handle(e, config.RemoteConfig{}, Request{ID: "1", Op: OpLog, ThreadID: "nope", Before: 2}, "relay", "s")
	if missing.OK || missing.Code != "not_found" {
		t.Fatalf("missing %+v", missing)
	}
	unknown := Handle(e, config.RemoteConfig{}, Request{ID: "2", Op: OpLog, ThreadID: "x"}, "relay", "s")
	if unknown.OK || unknown.Code != "not_found" {
		t.Fatalf("unknown %+v", unknown)
	}
	noThread := Handle(e, config.RemoteConfig{}, Request{ID: "3", Op: OpLog, Before: 1}, "relay", "s")
	if noThread.OK || noThread.Code != "bad_request" {
		t.Fatalf("thread %+v", noThread)
	}
}

func TestLogZeroBeforePagesOlderThanLastTurn(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("zero", "", "")
	if err != nil {
		t.Fatal(err)
	}
	first := &store.Turn{ThreadID: th.ID, Status: store.TurnDone}
	if err := e.Store().CreateTurn(first); err != nil {
		t.Fatal(err)
	}
	second := &store.Turn{ThreadID: th.ID, Status: store.TurnRunning}
	if err := e.Store().CreateTurn(second); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: first.ID, Kind: "user_message", Text: "old",
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: second.ID, Kind: "user_message", Text: "now",
	}); err != nil {
		t.Fatal(err)
	}
	resp := Handle(e, config.RemoteConfig{WatchEvents: 8}, Request{
		ID: "l", Op: OpLog, ThreadID: th.ID,
	}, "relay", "s")
	if !resp.OK || len(resp.Events) != 1 || resp.Events[0].Text != "old" {
		t.Fatalf("zero before %+v", resp)
	}
}

func TestLogZeroBeforeOnEmptyLastTurnUsesTheTail(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("empty-log", "", "")
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
	resp := Handle(e, config.RemoteConfig{WatchEvents: 8}, Request{
		ID: "l", Op: OpLog, ThreadID: th.ID,
	}, "relay", "s")
	if !resp.OK || len(resp.Events) != 1 || resp.Events[0].Text != "prev" {
		t.Fatalf("empty last %+v", resp)
	}
}

func TestLastTurnWindowZeroCapUsesTheDefault(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("cap", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := e.Store().AppendEvent(&store.Event{
			ThreadID: th.ID, Kind: "user_message", Text: "x",
		}); err != nil {
			t.Fatal(err)
		}
	}
	page, hasMore, err := lastTurnWindow(e.Store(), th.ID, 0)
	if err != nil || hasMore || len(page) != 3 {
		t.Fatalf("default cap %+v hasMore=%v %v", page, hasMore, err)
	}
}

func TestLastTurnWindowClosedStore(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("closed", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = e.Store().Close()
	if _, _, err := lastTurnWindow(e.Store(), th.ID, 4); err == nil {
		t.Fatal("expected store error")
	}
}

func TestLogClosedStore(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("closed-log", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = e.Store().Close()
	resp := Handle(e, config.RemoteConfig{}, Request{ID: "l", Op: OpLog, ThreadID: th.ID, Before: 2}, "relay", "s")
	if resp.OK {
		t.Fatalf("closed %+v", resp)
	}
}

func TestLogPageStillSealsAfterTheHostNameIsStamped(t *testing.T) {
	// A page packed to the sealed cap used to fit in JSON and then die in
	// Seal once the display name was added. The phone waited out the RPC,
	// dropped the link, and Earlier never painted the previous turn.
	fat := strings.Repeat("a", 4000)
	events := make([]store.Event, 40)
	for i := range events {
		events[i] = store.Event{ThreadID: "t", Seq: int64(i + 1), Kind: "agent_message", Text: fat}
	}
	resp := packLogEvents("l", "relay", "s", "t", events, true, config.RemoteConfig{EventChars: 4000})
	resp.Host = strings.Repeat("n", 40)
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	const sealOverhead = 8 + 16
	if len(resp.Events) == 0 || len(raw)+sealOverhead > MaxPushPayload {
		t.Fatalf("sealed=%d events=%d", len(raw)+sealOverhead, len(resp.Events))
	}
}

func TestPackLogEventsDropsOldestUntilTheFrameFits(t *testing.T) {
	events := []store.Event{
		{ThreadID: "t", Seq: 1, Kind: "not_a_desktop_kind", Text: "skip"},
		{ThreadID: "t", Seq: 2, Kind: "user_message", Text: strings.Repeat("a", 50<<10)},
		{ThreadID: "t", Seq: 3, Kind: "user_message", Text: strings.Repeat("b", 50<<10)},
		{ThreadID: "t", Seq: 4, Kind: "user_message", Text: strings.Repeat("c", 50<<10)},
	}
	resp := packLogEvents("1", "relay", "s", "t", events, false, config.RemoteConfig{EventChars: 40000})
	if !resp.OK || !resp.More {
		t.Fatalf("pack %+v", resp)
	}
	if len(resp.Events) == 0 || len(resp.Events) >= 3 {
		t.Fatalf("should drop oldest of the oversized page: %d", len(resp.Events))
	}
}

func TestPackLogEventsSkipsKindsThePhoneDoesNotWatch(t *testing.T) {
	resp := packLogEvents("1", "relay", "s", "t", []store.Event{
		{ThreadID: "t", Seq: 9, Kind: "not_a_desktop_kind", Text: "nope"},
	}, true, config.RemoteConfig{})
	if !resp.OK || len(resp.Events) != 0 || !resp.More || resp.Seq != 9 {
		t.Fatalf("skipped %+v", resp)
	}
}

func TestLastTurnWindowEmptyTurnDoesNotPaintAnotherTurn(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("empty-turn", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, Status: store.TurnRunning}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, Kind: "user_message", Text: "raw",
	}); err != nil {
		t.Fatal(err)
	}
	page, hasMore, err := lastTurnWindow(e.Store(), th.ID, 80)
	if err != nil || len(page) != 0 || !hasMore {
		t.Fatalf("empty last turn %+v hasMore=%v %v", page, hasMore, err)
	}
}

func TestLastTurnWindowKeepsUntaggedTailAfterTheTurn(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("extra", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, Status: store.TurnDone}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: turn.ID, Kind: "user_message", Text: "in",
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, Kind: "agent_message", Text: "after",
	}); err != nil {
		t.Fatal(err)
	}
	page, hasMore, err := lastTurnWindow(e.Store(), th.ID, 80)
	if err != nil || hasMore || len(page) != 2 || page[1].Text != "after" {
		t.Fatalf("extra %+v hasMore=%v %v", textsOf(page), hasMore, err)
	}
}

func textsOf(events []store.Event) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = ev.Text
	}
	return out
}
