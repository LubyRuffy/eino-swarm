package engine

import (
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A compacted ADK transcript is a view for the next Generate. The reviewer
// must still see the procedure that lived only on the event log, or a long
// /goal session that auto-compacted would teach the project nothing.
func TestReviewSourceReadsEventsNotAFoldedTranscript(t *testing.T) {
	const procedure = "the-secret-recovery-steps"
	events := []store.Event{
		{Kind: KindUser, Text: "keep going"},
		{Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, Text: "exec({cmd:" + procedure + "})"},
		{Kind: swarm.NotifyToolResult.String(), Text: "it worked after " + procedure},
	}
	out := renderReviewFromEvents(events, "earlier briefing only", "done")
	if !strings.Contains(out, procedure) {
		t.Fatalf("the reviewer must see the procedure from the event log:\n%s", out)
	}
	if !strings.Contains(out, "keep going") || !strings.Contains(out, "done") {
		t.Fatalf("the reviewer must still see the request and the answer:\n%s", out)
	}
	if !strings.Contains(out, "session briefing") || !strings.Contains(out, "earlier briefing only") {
		t.Fatalf("a rolling session briefing must lead the review:\n%s", out)
	}
}

func TestReviewSourceSkipsCompactAndLiveNoise(t *testing.T) {
	huge := strings.Repeat("x", reviewMaxCharsPerMessage*3)
	out := renderReviewFromEvents([]store.Event{
		{Kind: KindCompacted, Text: "folded briefing that must not be the source"},
		{Kind: KindSessionMemory, Text: "session memory event is for the trace, not a second briefing"},
		{Kind: swarm.NotifyDelta.String(), Text: "half an answer"},
		{Kind: KindProgress, Text: `{"elapsed_ms":1}`},
		{Kind: KindUser, Text: "the request"},
		{Kind: swarm.NotifyToolResult.String(), Text: huge},
	}, "", "final")
	if strings.Contains(out, "folded briefing") || strings.Contains(out, "session memory event") {
		t.Fatalf("compact/session events leaked into the review:\n%s", out)
	}
	if strings.Contains(out, huge) {
		t.Fatal("a large tool result reached the reviewer whole")
	}
	if !strings.Contains(out, "the request") {
		t.Fatalf("missing the request:\n%s", out)
	}
	if len([]rune(out)) > reviewMaxChars+1000 {
		t.Fatalf("unbounded review transcript: %d", len([]rune(out)))
	}
	if body := renderReviewFromEvents(nil, "  ", "   "); body != "" {
		t.Fatalf("an empty conversation rendered %q", body)
	}
}

func TestReviewSourcePutsThisTurnAheadOfALongBriefingBudget(t *testing.T) {
	briefing := strings.Repeat("b", reviewMaxChars)
	out := renderReviewFromEvents([]store.Event{
		{Kind: KindUser, Text: "this-turn-marker"},
	}, briefing, "")
	if !strings.Contains(out, "this-turn-marker") {
		t.Fatal("a long session briefing spent the budget before this turn's events")
	}
}

func TestManagerWroteMemoryRequiresALandedWrite(t *testing.T) {
	if managerWroteMemory(nil) {
		t.Fatal("no events is not a write")
	}
	if managerWroteMemory([]store.Event{{
		Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID,
		Text: memory.ToolSkillView + "({name:x})", ToolCallID: "c0",
	}}) {
		t.Fatal("opening a skill is not curating memory")
	}
	if managerWroteMemory([]store.Event{{
		Kind: swarm.NotifyToolCall.String(), AgentID: "worker-1",
		Text: memory.ToolMemory + "({action:add})", ToolCallID: "c1",
	}, {
		Kind: swarm.NotifyToolResult.String(), ToolCallID: "c1", Text: "ok",
	}}) {
		t.Fatal("a worker cannot curate the project store")
	}
	if managerWroteMemory([]store.Event{{
		Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID,
		Text: memory.ToolMemory + "({action:add})", ToolCallID: "c2",
	}, {
		Kind: swarm.NotifyToolResult.String(), ToolCallID: "c2", Err: "refused",
	}}) {
		t.Fatal("a refused write must not skip the reviewer")
	}
	if !managerWroteMemory([]store.Event{{
		Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID,
		Text: memory.ToolMemory + "({action:add})", ToolCallID: "c3",
	}, {
		Kind: swarm.NotifyToolResult.String(), ToolCallID: "c3", Text: "stored",
	}}) {
		t.Fatal("a landed memory write must skip the automatic reviewer")
	}
	if !managerWroteMemory([]store.Event{{
		Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID,
		Text: memory.ToolSkillManage + "({action:create})",
	}}) {
		t.Fatal("a skill write without a call id still counts")
	}
}

func TestToolCallNameStopsAtTheParen(t *testing.T) {
	if got := toolCallName("  memory({\"action\":\"add\"})  "); got != memory.ToolMemory {
		t.Fatalf("toolCallName=%q", got)
	}
	if got := toolCallName("exec"); got != "exec" {
		t.Fatalf("bare name=%q", got)
	}
}

func TestReviewEventLineReadsTheLogNotTheNoise(t *testing.T) {
	cases := []struct {
		ev         store.Event
		wantRole   string
		wantSubstr string
		ok         bool
	}{
		{store.Event{Kind: KindUser}, "", "", false},
		{store.Event{Kind: KindSteer, Text: "nudge"}, "human", "nudge", true},
		{store.Event{Kind: KindSteer, Text: goalSessionWrapSteer()}, "", "", false},
		{store.Event{Kind: KindGoalContinued, Text: "again"}, "human", "again", true},
		{store.Event{Kind: swarm.NotifyAgentMessage.String()}, "", "", false},
		{store.Event{Kind: swarm.NotifyAgentMessage.String(), Text: "answer"}, "assistant", "answer", true},
		{store.Event{Kind: swarm.NotifyAgentMessage.String(), AgentID: "worker-1", Text: "from a worker"}, "worker-1", "from a worker", true},
		{store.Event{Kind: swarm.NotifyToolCall.String()}, "", "", false},
		{store.Event{Kind: swarm.NotifyToolResult.String(), Err: "failed"}, "tool", "failed", true},
		{store.Event{Kind: swarm.NotifyToolResult.String(), Text: "body", Err: "failed"}, "tool", "failed", true},
		{store.Event{Kind: swarm.NotifyToolResult.String()}, "", "", false},
		{store.Event{Kind: swarm.NotifySpawned.String()}, "", "", false},
		{store.Event{Kind: swarm.NotifySpawned.String(), AgentID: "w1"}, "spawn", "w1", true},
		{store.Event{Kind: swarm.NotifySpawned.String(), Role: "reader"}, "spawn", "reader", true},
		{store.Event{Kind: swarm.NotifyFinished.String()}, "worker", "finished", true},
		{store.Event{Kind: swarm.NotifyFinished.String(), AgentID: "w1", Text: "done"}, "w1", "done", true},
		{store.Event{Kind: KindProgress, Text: "tick"}, "", "", false},
	}
	for _, tc := range cases {
		role, text, ok := reviewEventLine(tc.ev)
		if ok != tc.ok {
			t.Fatalf("%s ok=%v want %v", tc.ev.Kind, ok, tc.ok)
		}
		if !ok {
			continue
		}
		if role != tc.wantRole || !strings.Contains(text, tc.wantSubstr) {
			t.Fatalf("%s role=%q text=%q", tc.ev.Kind, role, text)
		}
	}
}
