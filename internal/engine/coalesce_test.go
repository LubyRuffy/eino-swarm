package engine

import (
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func waitLive(t *testing.T, sub *Subscription, kind string, timeout time.Duration) store.Event {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-sub.C:
			if ev.Kind == kind {
				return ev
			}
		case <-deadline:
			t.Fatalf("timed out waiting for a %q event", kind)
		}
	}
}

// A 50-token-per-second stream must not redraw the UI fifty times a second.
// The coalescer holds tokens and sends the latest one once the window closes.
func TestCoalescedDeltasKeepTheLatestText(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("coalesce", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()

	acc := newAccumulator(e, th.ID, "turn-1", 25*time.Millisecond)
	const n = 12
	for i := 1; i <= n; i++ {
		acc.onNotify(swarm.Notification{
			Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID, Text: strings.Repeat("x", i),
		})
	}

	ev := waitLive(t, sub, swarm.NotifyDelta.String(), 500*time.Millisecond)
	if ev.Text != strings.Repeat("x", n) {
		t.Fatalf("coalescer sent a stale snapshot: %q", ev.Text)
	}

	select {
	case extra := <-sub.C:
		if extra.Kind == swarm.NotifyDelta.String() {
			t.Fatalf("a second delta leaked through the window: %+v", extra)
		}
	case <-time.After(40 * time.Millisecond):
	}
}

// A tool call is a turn boundary. The last streamed commentary has to be on
// the wire before the call appears, or the UI would show a tool running
// against an answer that is still 25ms in the future.
func TestAToolCallFlushesTheHeldDelta(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("flush-on-call", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()

	acc := newAccumulator(e, th.ID, "turn-1", 2*time.Second)
	acc.onNotify(swarm.Notification{
		Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID, Text: "Checking now",
	})
	acc.onNotify(swarm.Notification{
		Kind: swarm.NotifyToolCall, AgentID: swarm.DefaultManagerID,
		Text: "read({})", ToolCallID: "c1",
	})

	delta := waitLive(t, sub, swarm.NotifyDelta.String(), 200*time.Millisecond)
	if delta.Text != "Checking now" {
		t.Fatalf("the held delta was not flushed: %q", delta.Text)
	}
	call := waitLive(t, sub, swarm.NotifyToolCall.String(), 200*time.Millisecond)
	if call.Text != "read({})" {
		t.Fatalf("tool call=%q", call.Text)
	}
}

// The complete answer is about to be recorded, so emitting the last partial
// first would paint the same text twice. Drop it instead.
func TestACompleteAnswerDropsTheHeldDelta(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("drop-live", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()

	acc := newAccumulator(e, th.ID, "turn-1", 2*time.Second)
	acc.onNotify(swarm.Notification{
		Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID, Text: "The ans",
	})
	acc.onNotify(swarm.Notification{
		Kind: swarm.NotifyAgentMessage, AgentID: swarm.DefaultManagerID, Text: "The answer",
	})

	ev := waitLive(t, sub, swarm.NotifyAgentMessage.String(), 200*time.Millisecond)
	if ev.Text != "The answer" {
		t.Fatalf("complete answer=%q", ev.Text)
	}
	select {
	case extra := <-sub.C:
		if extra.Kind == swarm.NotifyDelta.String() {
			t.Fatalf("a dropped delta still arrived: %+v", extra)
		}
	case <-time.After(40 * time.Millisecond):
	}
}

// Ending a turn must send whatever is still held and cancel the timer, or a
// cancelled stream would lose its last tokens and a timer would fire after done.
func TestFlushAllSendsTheHeldDelta(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("flush-all", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()

	acc := newAccumulator(e, th.ID, "turn-1", 2*time.Second)
	acc.onNotify(swarm.Notification{
		Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID, Text: "half an ans",
	})
	acc.flushAll()

	ev := waitLive(t, sub, swarm.NotifyDelta.String(), 200*time.Millisecond)
	if ev.Text != "half an ans" {
		t.Fatalf("held delta was not flushed at the end of the turn: %q", ev.Text)
	}
}

// With coalescing off, every token is a frame — tests and a user who asked
// for that get what they configured.
func TestZeroCoalesceEmitsEveryToken(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("no-coalesce", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()

	acc := newAccumulator(e, th.ID, "turn-1", 0)
	acc.onNotify(swarm.Notification{
		Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID, Text: "a",
	})
	acc.onNotify(swarm.Notification{
		Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID, Text: "ab",
	})

	first := waitLive(t, sub, swarm.NotifyDelta.String(), 200*time.Millisecond)
	second := waitLive(t, sub, swarm.NotifyDelta.String(), 200*time.Millisecond)
	if first.Text != "a" || second.Text != "ab" {
		t.Fatalf("zero coalesce must not drop tokens: %q then %q", first.Text, second.Text)
	}
}

// Two agents streaming at once must not share a window: coalescing the
// manager's tokens into a worker's (or the other way around) would show the
// wrong text on the wrong row.
func TestCoalesceWindowsArePerAgent(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("per-agent", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()

	acc := newAccumulator(e, th.ID, "turn-1", 25*time.Millisecond)
	acc.onNotify(swarm.Notification{Kind: swarm.NotifyDelta, AgentID: "manager", Text: "mgr"})
	acc.onNotify(swarm.Notification{Kind: swarm.NotifyDelta, AgentID: "worker-1", Role: "worker", Text: "wrk"})

	seen := map[string]string{}
	deadline := time.After(500 * time.Millisecond)
	for len(seen) < 2 {
		select {
		case ev := <-sub.C:
			if ev.Kind == swarm.NotifyDelta.String() {
				seen[ev.AgentID] = ev.Text
			}
		case <-deadline:
			t.Fatalf("missing a per-agent delta: %+v", seen)
		}
	}
	if seen["manager"] != "mgr" || seen["worker-1"] != "wrk" {
		t.Fatalf("windows leaked across agents: %+v", seen)
	}
}

func TestParallelToolDeltasDoNotOverwrite(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("parallel-exec", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()

	acc := newAccumulator(e, th.ID, "turn-1", 25*time.Millisecond)
	agent := swarm.DefaultManagerID
	acc.onNotify(swarm.Notification{
		Kind: swarm.NotifyToolDelta, AgentID: agent, ToolCallID: "c1", Text: `{"stdout":"a","stderr":""}`,
	})
	acc.onNotify(swarm.Notification{
		Kind: swarm.NotifyToolDelta, AgentID: agent, ToolCallID: "c2", Text: `{"stdout":"x","stderr":""}`,
	})
	acc.onNotify(swarm.Notification{
		Kind: swarm.NotifyToolDelta, AgentID: agent, ToolCallID: "c1", Text: `{"stdout":"ab","stderr":""}`,
	})

	seen := map[string]string{}
	deadline := time.After(500 * time.Millisecond)
	for len(seen) < 2 {
		select {
		case ev := <-sub.C:
			if ev.Kind == swarm.NotifyToolDelta.String() {
				seen[ev.ToolCallID] = ev.Text
			}
		case <-deadline:
			t.Fatalf("missing a per-call tool_delta: %+v", seen)
		}
	}
	if !strings.Contains(seen["c1"], `"stdout":"ab"`) || !strings.Contains(seen["c2"], `"stdout":"x"`) {
		t.Fatalf("parallel exec snapshots mixed: %+v", seen)
	}
}
