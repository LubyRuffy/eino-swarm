package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestCompactFoldsOlderReplayAndKeepsRecent(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.CompactKeepMessages = 2
	th, _ := e.CreateThread("", "", "")

	first, err := e.StartTurn(th.ID, "the first request")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	second, err := e.StartTurn(th.ID, "the second request")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, second.ID)

	got, err := e.CompactThread(th.ID)
	if err != nil {
		t.Fatalf("CompactThread: %v", err)
	}
	if strings.TrimSpace(got.CompactSummary) == "" || got.CompactThroughSeq == 0 {
		t.Fatalf("compact did not store a briefing: %+v", got)
	}
	if !strings.Contains(got.CompactSummary, "Prior work:") {
		t.Fatalf("mock briefing missing: %q", got.CompactSummary)
	}

	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var compacted store.Event
	for _, ev := range events {
		if ev.Kind == KindCompacted {
			compacted = ev
		}
	}
	if compacted.Kind == "" || compacted.AgentID != CompactAgentID || compacted.Err != "" {
		t.Fatalf("missing compacted event: %+v", compacted)
	}
	var payload compactPayload
	if err := json.Unmarshal([]byte(compacted.Text), &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload.Summary != got.CompactSummary || payload.ThroughSeq != got.CompactThroughSeq {
		t.Fatalf("payload does not match the thread: %+v vs %+v", payload, got)
	}
	if payload.CharsAfter >= payload.CharsBefore {
		t.Fatalf("compact must shrink replay: before=%d after=%d", payload.CharsBefore, payload.CharsAfter)
	}

	history, err := e.replayHistory(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	var joined strings.Builder
	for _, m := range history {
		joined.WriteString(m.Content)
		joined.WriteString("\n")
	}
	if strings.Contains(joined.String(), "the first request") {
		t.Fatalf("compacted request still in replay:\n%s", joined.String())
	}
	if !strings.Contains(joined.String(), "the second request") {
		t.Fatalf("the kept tail vanished:\n%s", joined.String())
	}

	extra := conversationExtra(got, nil)
	if !strings.Contains(extra, got.CompactSummary) {
		t.Fatal("later turns would not see the briefing")
	}
	for _, leak := range []string{"notes.md", "researcher", "re-research"} {
		if strings.Contains(strings.ToLower(compactPrompt()), leak) {
			t.Fatalf("compact prompt leaks %q", leak)
		}
	}
}

func TestCompactDoesNothingWhenTheConversationIsShort(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if _, err := e.CompactThread(th.ID); !errors.Is(err, ErrNothingToCompact) {
		t.Fatalf("empty conversation: %v", err)
	}
	turn, err := e.StartTurn(th.ID, "only one exchange")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	if _, err := e.CompactThread(th.ID); !errors.Is(err, ErrNothingToCompact) {
		t.Fatalf("one exchange is the keep tail: %v", err)
	}
}

func TestCompactIsBusyWhileATurnRuns(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurn(th.ID, "still running")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.CompactThread(th.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("want ErrBusy, got %v", err)
	}
	waitForTurn(t, e, turn.ID)
}

func TestCompactRecordsAFailedModelCall(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.CompactKeepMessages = 2
	th, _ := e.CreateThread("", "", "")
	first, _ := e.StartTurn(th.ID, "one")
	waitForTurn(t, e, first.ID)
	second, _ := e.StartTurn(th.ID, "two")
	waitForTurn(t, e, second.ID)

	prev := compactGenerate
	compactGenerate = func(context.Context, model.BaseChatModel, []*schema.Message) (*schema.Message, error) {
		return nil, errors.New("endpoint down")
	}
	t.Cleanup(func() { compactGenerate = prev })

	if _, err := e.CompactThread(th.ID); err == nil {
		t.Fatal("want the generate failure")
	}
	th, _ = e.Store().GetThread(th.ID)
	if th.CompactSummary != "" {
		t.Fatalf("a failed compact must not rewrite replay: %+v", th)
	}
	events, _ := e.Replay(th.ID, 0)
	found := false
	for _, ev := range events {
		if ev.Kind == KindCompacted && ev.Err != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("the failed compact left no trace")
	}
}

func TestASecondCompactSkipsAlreadyFoldedMessages(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.CompactKeepMessages = 2
	th, _ := e.CreateThread("", "", "")
	for _, text := range []string{"one", "two"} {
		turn, err := e.StartTurn(th.ID, text)
		if err != nil {
			t.Fatal(err)
		}
		waitForTurn(t, e, turn.ID)
	}
	if _, err := e.CompactThread(th.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.CompactThread(th.ID); !errors.Is(err, ErrNothingToCompact) {
		t.Fatalf("folding the same tail again: %v", err)
	}
}

func TestCompactHonoursAPinnedModel(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.CompactKeepMessages = 2
	e.Config().Swarm.CompactProvider = e.Config().Models.Default
	e.Config().Swarm.CompactModel = "tiny"
	th, _ := e.CreateThread("", "", "")
	for _, text := range []string{"one", "two"} {
		turn, err := e.StartTurn(th.ID, text)
		if err != nil {
			t.Fatal(err)
		}
		waitForTurn(t, e, turn.ID)
	}
	if _, err := e.CompactThread(th.ID); err != nil {
		t.Fatal(err)
	}
	turns, err := e.Store().ListTurns(th.ID)
	if err != nil || len(turns) == 0 {
		t.Fatalf("turns: %v", err)
	}
	calls, err := e.Store().ListLLMCalls(turns[len(turns)-1].ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range calls {
		if c.AgentID == CompactAgentID {
			found = true
			if c.Model != "tiny" {
				t.Fatalf("summarizer model=%q want the pinned name", c.Model)
			}
		}
	}
	if !found {
		t.Fatal("the summarizer's model call is missing from the turn's trace")
	}
}
