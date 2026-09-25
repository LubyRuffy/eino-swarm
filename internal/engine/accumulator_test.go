package engine

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// The accumulator is the piece that decides what survives a reload, so its
// rules are worth pinning down directly rather than only through a full run.
func TestAccumulatorPersistsCompleteTextNotDeltas(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	acc := newAccumulator(e, th.ID, turn.ID, 0)

	agent := "researcher-1"
	feed := func(kind swarm.NotifyKind, text string) {
		acc.onNotify(swarm.Notification{Kind: kind, AgentID: agent, Role: "researcher", Text: text})
	}

	feed(swarm.NotifyTurn, "turn 1")
	feed(swarm.NotifyReasoningDelta, "I ")
	feed(swarm.NotifyReasoningDelta, "I should ")
	feed(swarm.NotifyReasoningDelta, "I should check.")
	feed(swarm.NotifyDelta, "Check")
	feed(swarm.NotifyDelta, "Checking now")
	acc.onNotify(swarm.Notification{Kind: swarm.NotifyToolCall, AgentID: agent,
		Role: "researcher", Text: "read({})", ToolCallID: "c1"})
	acc.onNotify(swarm.Notification{Kind: swarm.NotifyToolResult, AgentID: agent,
		Role: "researcher", Text: "file body", ToolCallID: "c1"})
	feed(swarm.NotifyTurn, "turn 2")
	feed(swarm.NotifyReasoningDelta, "Now I can answer.")
	feed(swarm.NotifyDelta, "The")
	feed(swarm.NotifyDelta, "The answer")
	feed(swarm.NotifyAgentMessage, "The answer")
	acc.flushAll()

	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	var texts []string
	for _, ev := range events {
		kinds = append(kinds, ev.Kind)
		texts = append(texts, ev.Text)
		if ev.Kind == swarm.NotifyDelta.String() || ev.Kind == swarm.NotifyReasoningDelta.String() ||
			ev.Kind == swarm.NotifyTurn.String() {
			t.Fatalf("%q should be live-only, but it was persisted: %+v", ev.Kind, ev)
		}
	}

	want := []string{
		KindReasoning,                     // turn 1's thinking, complete
		swarm.NotifyAgentMessage.String(), // turn 1's commentary before the tool call
		swarm.NotifyToolCall.String(),
		swarm.NotifyToolResult.String(),
		KindReasoning,                     // turn 2's thinking
		swarm.NotifyAgentMessage.String(), // the final answer
	}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("persisted %v\nwant     %v", kinds, want)
	}
	if texts[0] != "I should check." {
		t.Fatalf("reasoning was not persisted whole: %q", texts[0])
	}
	if texts[1] != "Checking now" {
		t.Fatalf("interim commentary was not persisted whole: %q", texts[1])
	}
	if texts[4] != "Now I can answer." {
		t.Fatalf("turn 2's reasoning leaked turn 1's text: %q", texts[4])
	}
	// the agent_message notification already carries the whole answer, so the
	// pending delta accumulator must be dropped rather than persisted twice
	if texts[5] != "The answer" {
		t.Fatalf("final answer=%q", texts[5])
	}
}

func TestToolDeltaIsBroadcastNotStored(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	acc := newAccumulator(e, th.ID, turn.ID, 0)
	acc.onNotify(swarm.Notification{
		Kind: swarm.NotifyToolCall, AgentID: swarm.DefaultManagerID, Text: "exec({})", ToolCallID: "c1",
	})
	acc.onNotify(swarm.Notification{
		Kind: swarm.NotifyToolCallDelta, AgentID: swarm.DefaultManagerID,
		Text: "exec(12)", ToolCallID: "c1",
	})
	acc.onNotify(swarm.Notification{
		Kind: swarm.NotifyToolDelta, AgentID: swarm.DefaultManagerID,
		Text: `{"stdout":"a","stderr":""}`, ToolCallID: "c1",
	})
	acc.onNotify(swarm.Notification{
		Kind: swarm.NotifyToolResult, AgentID: swarm.DefaultManagerID,
		Text: `{"stdout":"ab","stderr":""}`, ToolCallID: "c1",
	})
	acc.flushAll()

	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == swarm.NotifyToolDelta.String() || ev.Kind == swarm.NotifyToolCallDelta.String() {
			t.Fatalf("streamed tool text must not be persisted: %+v", ev)
		}
	}
}

// An agent cut off mid-sentence must still leave its partial text behind.
func TestAccumulatorFlushesPartialWorkAtTheEnd(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	acc := newAccumulator(e, th.ID, turn.ID, 0)

	acc.onNotify(swarm.Notification{Kind: swarm.NotifyReasoningDelta,
		AgentID: swarm.DefaultManagerID, Text: "half a thought"})
	acc.onNotify(swarm.Notification{Kind: swarm.NotifyDelta,
		AgentID: swarm.DefaultManagerID, Text: "half an ans"})
	acc.flushAll()

	events, _ := e.Replay(th.ID, 0)
	var reasoning, answer string
	for _, ev := range events {
		switch ev.Kind {
		case KindReasoning:
			reasoning = ev.Text
		case swarm.NotifyAgentMessage.String():
			answer = ev.Text
		}
	}
	if reasoning != "half a thought" {
		t.Fatalf("partial reasoning lost: %q", reasoning)
	}
	if answer != "half an ans" {
		t.Fatalf("partial answer lost: %q", answer)
	}

	// flushing twice must not duplicate anything
	before := len(events)
	acc.flushAll()
	after, _ := e.Replay(th.ID, 0)
	if len(after) != before {
		t.Fatalf("a second flush duplicated events: %d then %d", before, len(after))
	}
}

// A failed sub-agent has to reach the timeline with its error attached, or the
// UI silently shows a worker that never finished.
func TestAccumulatorCarriesErrors(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	acc := newAccumulator(e, th.ID, turn.ID, 0)

	acc.onNotify(swarm.Notification{
		Kind: swarm.NotifyFinished, AgentID: "reviewer-2", Role: "reviewer",
		Text: "", Err: errors.New("agent timed out"),
	})
	events, _ := e.Replay(th.ID, 0)
	if len(events) != 1 {
		t.Fatalf("want one event, got %d", len(events))
	}
	ev := events[0]
	if ev.Kind != swarm.NotifyFinished.String() || ev.AgentID != "reviewer-2" {
		t.Fatalf("event=%+v", ev)
	}
	if ev.Err != "agent timed out" {
		t.Fatalf("the failure did not reach the timeline: %+v", ev)
	}
	if ev.Role != "reviewer" {
		t.Fatalf("role lost: %+v", ev)
	}
}

func TestAccumulatorDropsFinishedAfterQuit(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.runtimeFor(th.ID).abandon()
	acc := newAccumulator(e, th.ID, turn.ID, 0)
	acc.onNotify(swarm.Notification{
		Kind: swarm.NotifyFinished, AgentID: "worker-1", Role: "worker",
		Text: "", Err: errors.New("cancelled"),
	})
	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == swarm.NotifyFinished.String() {
			t.Fatalf("quit recorded a cancelled worker as finished: %+v", ev)
		}
	}
}

// Unknown notification kinds must still be persisted rather than dropped, so a
// new swarm event type shows up in the timeline before the UI knows about it.
func TestAccumulatorPersistsUnknownKinds(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	acc := newAccumulator(e, th.ID, turn.ID, 0)
	acc.onNotify(swarm.Notification{Kind: swarm.NotifyKind(999), AgentID: "x", Text: "future"})

	events, _ := e.Replay(th.ID, 0)
	if len(events) != 1 || events[0].Text != "future" {
		t.Fatalf("an unrecognized kind was dropped: %+v", events)
	}
}

// A turn cannot start when the conversation's provider is not usable, and the
// failure must be recorded rather than leaving a turn stuck at running.
func TestStartTurnFailsClosedOnAnUnusableProvider(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")

	// swap the mock pool for a real one whose provider has no endpoint
	real := newRealPoolEngine(t, e)
	if _, err := real.StartTurn(th.ID, "go"); err == nil {
		t.Fatal("want an error when the provider is not configured")
	}
	turns, err := real.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, turn := range turns {
		if turn.Status == store.TurnRunning {
			t.Fatalf("a turn was left running after the provider failed: %+v", turn)
		}
	}
	if real.Status(th.ID).Running {
		t.Fatal("the conversation is marked running after a failed start")
	}
	if real.Providers() == nil {
		t.Fatal("Providers must be exposed for the settings endpoints")
	}
}

// newRealPoolEngine reuses an engine's store and config but swaps in the real
// provider pool, whose default provider has no endpoint configured.
func newRealPoolEngine(t *testing.T, e *Engine) *Engine {
	t.Helper()
	return New(e.Config(), e.Store(), provider.New(e.Config()),
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}
