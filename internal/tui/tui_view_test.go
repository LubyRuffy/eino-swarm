package tui

import (
	"fmt"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
)

// Streamed text used to fold to the first line the moment the model finished,
// and there was no key to open it again. That is the "I saw it live, then it
// vanished" bug.
func TestAStreamedAnswerIsNotFoldedWhenItFinishes(t *testing.T) {
	m := newModel(nil)
	text := "line one\nline two\nline three"
	feed(&m,
		swarm.Notification{Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID, Text: text},
		swarm.Notification{Kind: swarm.NotifyAgentMessage, AgentID: swarm.DefaultManagerID, Text: text},
	)
	answers := blocksOfKind(m.manager, blockAnswer)
	if len(answers) != 1 || answers[0].answer != text {
		t.Fatalf("answer=%+v", answers)
	}
	if !answers[0].open {
		t.Fatal("a finished answer must stay readable")
	}
	if m.manager.curAnswer != nil {
		t.Fatal("the live cursor must end or the next delta overwrites this block")
	}
	m.width, m.height = 80, 24
	view := m.View()
	if !strings.Contains(view, "line two") || !strings.Contains(view, "line three") {
		t.Fatalf("the body vanished after the stream ended:\n%s", view)
	}
}

func TestToolCallDoesNotLetTheNextDeltaEatTheCommentary(t *testing.T) {
	m := newModel(nil)
	feed(&m,
		swarm.Notification{Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID, Text: "I will look that up"},
		swarm.Notification{Kind: swarm.NotifyToolCall, AgentID: swarm.DefaultManagerID, Text: "web_search(today)"},
		swarm.Notification{Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID, Text: "here is the result"},
	)
	answers := blocksOfKind(m.manager, blockAnswer)
	if len(answers) != 2 {
		t.Fatalf("want commentary and result as two blocks, got %d", len(answers))
	}
	if answers[0].answer != "I will look that up" || answers[1].answer != "here is the result" {
		t.Fatalf("answers=%q, %q", answers[0].answer, answers[1].answer)
	}
}

func TestCollapsedThinkingKeepsTheLiveTail(t *testing.T) {
	m := newModel(nil)
	feed(&m,
		swarm.Notification{Kind: swarm.NotifyReasoningDelta, AgentID: swarm.DefaultManagerID, Text: "setup first\nthen the conclusion"},
		swarm.Notification{Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID, Text: "ok"},
	)
	m.width, m.height = 80, 24
	view := m.View()
	if !strings.Contains(view, "then the conclusion") {
		t.Fatalf("folded thought hid the line they were watching:\n%s", view)
	}
}

func TestPaneKeepsTheNewestLines(t *testing.T) {
	if got := pinBottom("a\nb\nc\nd", 2); got != "c\nd" {
		t.Fatalf("pinBottom=%q", got)
	}
	if got := pinBottom("only", 5); got != "only" {
		t.Fatalf("short=%q", got)
	}
	m := newModel(nil)
	var body strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&body, "row-%d\n", i)
	}
	body.WriteString("row-live")
	feed(&m, swarm.Notification{Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID, Text: body.String()})
	m.width, m.height = 40, 10
	view := m.View()
	if !strings.Contains(view, "row-live") {
		t.Fatalf("the live edge was cropped off the bottom:\n%s", view)
	}
}

func TestFinishedWorkerKeepsItsAnswerReadable(t *testing.T) {
	m := newModel(nil)
	feed(&m,
		swarm.Notification{Kind: swarm.NotifySpawned, AgentID: "w1", Role: "lookup"},
		swarm.Notification{Kind: swarm.NotifyDelta, AgentID: "w1", Text: "detail one\ndetail two"},
		swarm.Notification{Kind: swarm.NotifyAgentMessage, AgentID: "w1", Text: "detail one\ndetail two"},
		swarm.Notification{Kind: swarm.NotifyFinished, AgentID: "w1"},
	)
	m.selected = 0
	m.width, m.height = 80, 24
	view := m.View()
	if !strings.Contains(view, "detail two") {
		t.Fatalf("opening the worker hid the body they watched live:\n%s", view)
	}
}
