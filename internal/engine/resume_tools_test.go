package engine

import (
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestOrphanedToolCallsSkipsCallsThatAlreadyHaveAResult(t *testing.T) {
	open := orphanedToolCalls([]store.Event{
		{Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "c1", Role: "manager"},
		{Kind: swarm.NotifyToolResult.String(), ToolCallID: "c1", Text: "ok"},
		{Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "c2", Role: "manager"},
		{Kind: swarm.NotifyToolCall.String(), AgentID: "worker-1", ToolCallID: "c3", Role: "worker"},
		{Kind: swarm.NotifyToolResult.String(), ToolCallID: "c3", Text: "done"},
	})
	if len(open) != 1 || open[0].ToolCallID != "c2" || open[0].AgentID != swarm.DefaultManagerID {
		t.Fatalf("open=%+v", open)
	}
}

func TestOrphanedToolCallsIgnoresEventsWithoutACallID(t *testing.T) {
	open := orphanedToolCalls([]store.Event{
		{Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID},
		{Kind: swarm.NotifyToolResult.String(), Text: "orphan"},
	})
	if len(open) != 0 {
		t.Fatalf("empty ids must not invent a pairing: %+v", open)
	}
	if open := orphanedToolCalls(nil); len(open) != 0 {
		t.Fatalf("nil events: %+v", open)
	}
}

func TestCloseOrphanedToolCallsRecordsAStoppedResult(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID,
		Text: "exec({})", ToolCallID: "c-exec",
	})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID,
		Text: "read({})", ToolCallID: "c-read",
	})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolResult.String(), AgentID: swarm.DefaultManagerID,
		Text: "the file body", ToolCallID: "c-read",
	})

	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolCall.String(), AgentID: "worker-1",
		Text: "exec({})", ToolCallID: "c-w",
	})

	e.closeOrphanedToolCalls(turn)
	e.closeOrphanedToolCalls(turn)

	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	results := resultsByCall(events)
	if got := results["c-exec"]; got.Err != resumeToolStopped || got.Text != resumeToolStopped {
		t.Fatalf("dead call was not closed: %+v", got)
	}
	if got := results["c-w"]; got.Err != resumeToolStopped || got.AgentID != "worker-1" {
		t.Fatalf("worker call was not closed: %+v", got)
	}
	if got := results["c-read"]; got.Text != "the file body" || got.Err != "" {
		t.Fatalf("a finished call was rewritten: %+v", got)
	}
	if n := countCallResults(events, "c-exec"); n != 1 {
		t.Fatalf("closing twice duplicated the result: %d", n)
	}
	msgs, err := e.Store().ListMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if strings.Contains(m.Content, resumeToolStopped) {
			t.Fatal("the stopped tool result leaked into conversation messages")
		}
	}
	for _, leak := range []string{"notes.md", "summarize", "look into this"} {
		if strings.Contains(resumeToolStopped, leak) {
			t.Fatalf("resume tool text leaked %q", leak)
		}
	}
}

func TestResumeRecordsStoppedResultsBeforeTheResumeNotice(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID,
		Text: "exec({})", ToolCallID: "c-exec",
	})

	sub := e.Subscribe(th.ID)
	defer sub.Close()
	if _, err := e.ResumeOrphanedTurns(); err != nil {
		t.Fatal(err)
	}
	waitLive(t, sub, KindResumed, 15*time.Second)

	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	execIdx, resumeIdx := -1, -1
	for i, ev := range events {
		switch {
		case ev.Kind == swarm.NotifyToolResult.String() && ev.ToolCallID == "c-exec":
			execIdx = i
			if ev.Err != resumeToolStopped {
				t.Fatalf("manager exec still looks live: %+v", ev)
			}
		case ev.Kind == KindResumed:
			resumeIdx = i
		}
	}
	if execIdx < 0 || resumeIdx < 0 {
		t.Fatalf("missing stop/resume events: %+v", events)
	}
	if execIdx > resumeIdx {
		t.Fatal("a dead tool was still pending when the resume notice landed")
	}
	_ = e.Interrupt(th.ID)
	waitForTurn(t, e, turn.ID)
}

func TestCloseOrphanedToolCallsNoopsOnNilOrClosed(t *testing.T) {
	(*Engine)(nil).closeOrphanedToolCalls(nil)
	e := newTestEngine(t)
	e.closeOrphanedToolCalls(nil)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	_ = e.Store().Close()
	e.closeOrphanedToolCalls(turn)
}

func TestCloseOrphanedToolCallsFillsAMissingAgent(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolCall.String(), Text: "exec({})", ToolCallID: "c1",
	})
	e.closeOrphanedToolCalls(turn)
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := resultsByCall(events)["c1"]
	if got.AgentID != swarm.DefaultManagerID || got.Err != resumeToolStopped {
		t.Fatalf("missing agent_id was not filled: %+v", got)
	}
}

func resultsByCall(events []store.Event) map[string]store.Event {
	out := make(map[string]store.Event)
	for _, ev := range events {
		if ev.Kind == swarm.NotifyToolResult.String() && ev.ToolCallID != "" {
			out[ev.ToolCallID] = ev
		}
	}
	return out
}

func countCallResults(events []store.Event, callID string) int {
	n := 0
	for _, ev := range events {
		if ev.Kind == swarm.NotifyToolResult.String() && ev.ToolCallID == callID {
			n++
		}
	}
	return n
}
