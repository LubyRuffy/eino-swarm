package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// plantUnfinishedTurn is a previous process that died mid-answer: the row is
// still running, there is no live runtime, and the original request is what
// a restart has to continue.
func plantUnfinishedTurn(t *testing.T, e *Engine, threadID, text string) *store.Turn {
	t.Helper()
	th, err := e.Store().GetThread(threadID)
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{
		ThreadID:   threadID,
		UserText:   text,
		ProviderID: th.ProviderID,
		Model:      th.Model,
	}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(text) != "" {
		if err := e.Store().AppendMessages(threadID, turn.ID, []store.Message{
			{Role: "user", Content: text},
		}); err != nil {
			t.Fatal(err)
		}
	}
	return turn
}

func countKind(events []store.Event, kind string) int {
	n := 0
	for _, ev := range events {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

// A turn left running by a crash must pick up on the next start, not sit in
// the sidebar as Working forever and not be recorded as if the user stopped it.
func TestResumeOrphanedTurnsContinuesACrash(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "finish the unfinished request")

	n, err := e.ResumeOrphanedTurns()
	if err != nil {
		t.Fatalf("ResumeOrphanedTurns: %v", err)
	}
	if n != 1 {
		t.Fatalf("resumed %d, want 1", n)
	}
	if !e.Status(th.ID).Running {
		t.Fatal("the restarted conversation is not working")
	}

	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnDone {
		t.Fatalf("want a finished turn, got %s (%s)", finished.Status, finished.Error)
	}

	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if countKind(events, KindResumed) != 1 {
		t.Fatalf("the timeline is missing the resume: %+v", events)
	}
	if countKind(events, KindUser) != 1 {
		t.Fatalf("the original request should appear once, got %d", countKind(events, KindUser))
	}
	for _, ev := range events {
		if ev.Kind == KindResumed {
			for _, leak := range []string{"finish the unfinished request", "notes.md", "summarize"} {
				if strings.Contains(ev.Text, leak) {
					t.Fatalf("the resume event leaked %q: %s", leak, ev.Text)
				}
			}
		}
	}

	msgs, err := e.Store().ListMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if m.Content == resumeCue {
			t.Fatal("the resume cue was stored as a conversation message")
		}
	}
}

func TestResumeOrphanedTurnsDoesNotRepeatAStoredUserMessage(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "already on the timeline")
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: KindUser, AgentID: "manager", Text: turn.UserText,
	})

	if _, err := e.ResumeOrphanedTurns(); err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if countKind(events, KindUser) != 1 {
		t.Fatalf("want one user_message, got %d", countKind(events, KindUser))
	}
}

func TestResumeOrphanedTurnsContinuesEveryUnfinishedConversation(t *testing.T) {
	e := newTestEngine(t)
	a, err := e.CreateThread("one", "", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.CreateThread("two", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ta := plantUnfinishedTurn(t, e, a.ID, "first leftover")
	tb := plantUnfinishedTurn(t, e, b.ID, "second leftover")

	n, err := e.ResumeOrphanedTurns()
	if err != nil {
		t.Fatalf("ResumeOrphanedTurns: %v", err)
	}
	if n != 2 {
		t.Fatalf("resumed %d, want 2", n)
	}

	fa := waitForTurn(t, e, ta.ID)
	fb := waitForTurn(t, e, tb.ID)
	if fa.Status != store.TurnDone || fb.Status != store.TurnDone {
		t.Fatalf("unfinished conversations did not all complete: %+v %+v", fa, fb)
	}
}

// Stop is a human decision. Restarting the app must not undo it.
func TestResumeOrphanedTurnsIgnoresAUserStop(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "a request the human stopped")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	stopped := waitForTurn(t, e, turn.ID)
	if stopped.Status != store.TurnCancelled {
		t.Fatalf("want cancelled, got %s", stopped.Status)
	}

	n, err := e.ResumeOrphanedTurns()
	if err != nil {
		t.Fatalf("ResumeOrphanedTurns: %v", err)
	}
	if n != 0 {
		t.Fatalf("resumed a turn the user stopped: %d", n)
	}
	got, err := e.Store().GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.TurnCancelled {
		t.Fatalf("a user stop was rewritten: %+v", got)
	}
}

func TestResumeOrphanedTurnsIsANoopWhenNothingIsRunning(t *testing.T) {
	e := newTestEngine(t)
	n, err := e.ResumeOrphanedTurns()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("resumed %d on an empty store", n)
	}
}

// Two running rows on one conversation cannot both be live. Keep the later
// one; closing the older leftover is what stops the UI from looking busy for
// a turn that will never run.
func TestResumeOrphanedTurnsKeepsTheLatestTurnOnAConversation(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	older := plantUnfinishedTurn(t, e, th.ID, "older leftover")
	newer := plantUnfinishedTurn(t, e, th.ID, "newer leftover")

	n, err := e.ResumeOrphanedTurns()
	if err != nil {
		t.Fatalf("ResumeOrphanedTurns: %v", err)
	}
	if n != 1 {
		t.Fatalf("resumed %d, want 1", n)
	}

	waitForTurn(t, e, newer.ID)
	got, err := e.Store().GetTurn(older.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == store.TurnRunning {
		t.Fatalf("the older leftover is still marked running: %+v", got)
	}
	if got.Status == store.TurnDone {
		t.Fatal("the older leftover was run as well as the later one")
	}
}

func TestResumeOrphanedTurnsClosesATurnItCannotRestart(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// No request and no transcript: there is nothing to continue, and leaving
	// the row running would freeze the conversation.
	turn := plantUnfinishedTurn(t, e, th.ID, "")

	n, err := e.ResumeOrphanedTurns()
	if err != nil {
		t.Fatalf("ResumeOrphanedTurns: %v", err)
	}
	if n != 0 {
		t.Fatalf("claimed to resume an unresumable turn: %d", n)
	}
	got, err := e.Store().GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == store.TurnRunning {
		t.Fatalf("an unresumable turn is still marked running: %+v", got)
	}
}

func TestResumeOrphanedTurnsClosesATurnWithNoProvider(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, UserText: "orphaned", ProviderID: "no-such-provider"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendMessages(th.ID, turn.ID, []store.Message{
		{Role: "user", Content: turn.UserText},
	}); err != nil {
		t.Fatal(err)
	}

	n, err := e.ResumeOrphanedTurns()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("resumed a turn against a missing provider: %d", n)
	}
	got, err := e.Store().GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == store.TurnRunning {
		t.Fatalf("still running: %+v", got)
	}
}

func TestResumeOrphanedTurnsUsesTheConversationProviderWhenTheTurnHasNone(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, UserText: "inherit provider"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendMessages(th.ID, turn.ID, []store.Message{
		{Role: "user", Content: turn.UserText},
	}); err != nil {
		t.Fatal(err)
	}

	n, err := e.ResumeOrphanedTurns()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("resumed %d, want 1", n)
	}
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnDone {
		t.Fatalf("want done, got %s (%s)", finished.Status, finished.Error)
	}
}

func TestResumeOrphanedTurnsClosesATurnWhoseConversationIsGone(t *testing.T) {
	e := newTestEngine(t)
	turn := &store.Turn{ThreadID: "th_missing", UserText: "leftover", ProviderID: "p"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	n, err := e.ResumeOrphanedTurns()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("resumed a turn with no conversation: %d", n)
	}
	got, err := e.Store().GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == store.TurnRunning {
		t.Fatalf("still running: %+v", got)
	}
}

func TestResumeOrphanedTurnsReportsAClosedStore(t *testing.T) {
	e := newTestEngine(t)
	_ = e.Store().Close()
	if _, err := e.ResumeOrphanedTurns(); err == nil {
		t.Fatal("a closed database must surface, not look like there was nothing to resume")
	}
}

func TestTurnHasKindIsFalseWhenTheStoreCannotBeRead(t *testing.T) {
	e := newTestEngine(t)
	_ = e.Store().Close()
	if e.turnHasKind("tn_x", KindUser) {
		t.Fatal("a closed store must not claim an event exists")
	}
}

func TestResumeOrphanedTurnsDoesNotKillALiveTurn(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "already being continued")
	if _, err := e.ResumeOrphanedTurns(); err != nil {
		t.Fatal(err)
	}
	n, err := e.ResumeOrphanedTurns()
	if err != nil {
		t.Fatalf("second resume: %v", err)
	}
	if n != 0 {
		t.Fatalf("second resume started %d more runs", n)
	}
	got, err := e.Store().GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == store.TurnError {
		t.Fatalf("a live leftover was marked failed: %+v", got)
	}
	waitForTurn(t, e, turn.ID)
}

func TestResumeMessagesKeepsTheOriginalRequestOnce(t *testing.T) {
	history := []adk.Message{schema.UserMessage("the request"), schema.AssistantMessage("partial", nil)}
	got := resumeMessages(history, "the request")
	users := 0
	cues := 0
	for _, m := range got {
		if m == nil || m.Role != schema.User {
			continue
		}
		users++
		if m.Content == resumeCue {
			cues++
		}
	}
	if users != 2 || cues != 1 {
		t.Fatalf("users=%d cues=%d (want the original plus one cue)", users, cues)
	}
	again := resumeMessages(got, "the request")
	cues = 0
	for _, m := range again {
		if m != nil && m.Role == schema.User && m.Content == resumeCue {
			cues++
		}
	}
	if cues != 1 {
		t.Fatalf("stacked %d resume cues", cues)
	}

	fromScratch := resumeMessages(nil, "the request")
	if len(fromScratch) != 2 || fromScratch[0].Content != "the request" || fromScratch[1].Content != resumeCue {
		t.Fatalf("empty history: %+v", fromScratch)
	}
	withGap := resumeMessages([]adk.Message{nil, schema.UserMessage("the request")}, "")
	if !lastUserIs(withGap, resumeCue) || !hasUserMessage(withGap) {
		t.Fatal("a nil history entry must not hide the original request")
	}
	if lastUserIs(nil, resumeCue) || hasUserMessage(nil) || lastUserIs([]adk.Message{nil}, resumeCue) {
		t.Fatal("empty history is not a user message")
	}
}

func TestPickOrphanedTurnsKeepsTheLatestSeq(t *testing.T) {
	resume, drop := pickOrphanedTurns([]store.Turn{
		{ID: "old", ThreadID: "th_a", Seq: 1},
		{ID: "new", ThreadID: "th_a", Seq: 2},
		{ID: "other", ThreadID: "th_b", Seq: 1},
	})
	if len(resume) != 2 || len(drop) != 1 || drop[0].ID != "old" {
		t.Fatalf("resume=%+v drop=%+v", resume, drop)
	}
	ids := map[string]bool{}
	for _, t := range resume {
		ids[t.ID] = true
	}
	if !ids["new"] || !ids["other"] {
		t.Fatalf("kept the wrong turns: %+v", resume)
	}

	_, drop = pickOrphanedTurns([]store.Turn{
		{ID: "new", ThreadID: "th_a", Seq: 2},
		{ID: "old", ThreadID: "th_a", Seq: 1},
	})
	if len(drop) != 1 || drop[0].ID != "old" {
		t.Fatalf("later older row should still be dropped: %+v", drop)
	}
}

// A crash mid-turn must not throw away earlier finished exchanges. Resume is
// continuing the leftover request, not starting a blank conversation.
func TestResumeKeepsEarlierTurnsInReplay(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.StartTurn(th.ID, "remember the first request")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)

	plantUnfinishedTurn(t, e, th.ID, "the leftover request")
	dump := replayDump(t, e, th.ID)
	for _, want := range []string{"remember the first request", "the leftover request"} {
		if !strings.Contains(dump, want) {
			t.Fatalf("history lost %q before resume:\n%s", want, dump)
		}
	}
	if !strings.Contains(dump, "assistant:") {
		t.Fatalf("earlier answer missing from resume replay:\n%s", dump)
	}
}

// The UI stores completed answers as events as they land. Replay for the
// model used to wait until the turn ended, so a crash or force-quit made the
// next start forget everything said after the original request.
func TestReplayHistoryIncludesOnScreenAnswerBeforeTurnEnds(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()

	if _, err := e.StartTurn(th.ID, "a request still being answered"); err != nil {
		t.Fatal(err)
	}
	answer := waitLive(t, sub, swarm.NotifyAgentMessage.String(), 15*time.Second)
	if strings.TrimSpace(answer.Text) == "" {
		t.Fatal("the live answer was empty")
	}
	dump := replayDump(t, e, th.ID)
	if !strings.Contains(dump, answer.Text) {
		t.Fatalf("model replay is missing the on-screen answer while the turn is still running:\n%s\nanswer=%q", dump, answer.Text)
	}
	_ = e.Interrupt(th.ID)
}

// A previous process that died after flushing answers to the event log (but
// before persistTranscript) must still feed those answers into resume.
func TestResumeReplaysAgentMessagesLeftOnlyOnTheEventLog(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "finish the leftover request")
	partial := "already wrote the patch; continue from there"
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyAgentMessage.String(), AgentID: swarm.DefaultManagerID,
		Text: partial,
	}); err != nil {
		t.Fatal(err)
	}

	dump := replayDump(t, e, th.ID)
	if !strings.Contains(dump, partial) {
		t.Fatalf("event-only answer missing from resume replay:\n%s", dump)
	}
	msgs, err := e.Store().ListMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored := false
	for _, m := range msgs {
		if m.Role == string(schema.Assistant) && strings.Contains(m.Content, partial) {
			stored = true
			break
		}
	}
	if !stored {
		t.Fatal("event-only answer was replayed but not stored; the next crash would forget it again")
	}
	if n := strings.Count(replayDump(t, e, th.ID), partial); n != 1 {
		t.Fatalf("healed answer replayed %d times, want 1", n)
	}
}

func TestReplayHistoryIgnoresWorkerAnswersOnTheEventLog(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "the leftover request")
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyAgentMessage.String(), AgentID: "researcher-1",
		Text: "worker-only finding that the manager must not inherit",
	}); err != nil {
		t.Fatal(err)
	}
	dump := replayDump(t, e, th.ID)
	if strings.Contains(dump, "worker-only finding") {
		t.Fatalf("worker event leaked into manager replay:\n%s", dump)
	}
}

func TestIsManagerAgentTreatsBlankAsTheManager(t *testing.T) {
	if !isManagerAgent(swarm.DefaultManagerID) || !isManagerAgent("") || isManagerAgent("researcher-1") {
		t.Fatal("only the manager (and a blank id) should persist into replay")
	}
}

func TestPersistManagerAnswerSkipsEmptyText(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	e.persistManagerAnswer(th.ID, "tn_x", "   ")
	msgs, err := e.Store().ListMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("empty answer stored: %+v", msgs)
	}
}

func TestAppendMissingEventAnswersIsNoopWithoutLiveTurns(t *testing.T) {
	e := newTestEngine(t)
	out, err := e.appendMissingEventAnswers("th_x", nil, nil, nil)
	if err != nil || len(out) != 0 {
		t.Fatalf("empty live set: %v %+v", err, out)
	}
}

func TestStoredAssistantTextIgnoresOtherTurnsAndEmptyRows(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendMessages(th.ID, "tn_a", []store.Message{
		{Role: "assistant", Content: "keep me"},
		{Role: "assistant", Content: "   "},
		{Role: "user", Content: "not assistant"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendMessages(th.ID, "tn_b", []store.Message{
		{Role: "assistant", Content: "other turn"},
	}); err != nil {
		t.Fatal(err)
	}
	have := e.storedAssistantText(th.ID, "tn_a")
	if !have["keep me"] || have["other turn"] || have[""] {
		t.Fatalf("have=%v", have)
	}
	by := e.assistantTextByTurn(th.ID)
	if !by["tn_a"]["keep me"] || !by["tn_b"]["other turn"] || by["tn_a"]["other turn"] {
		t.Fatalf("by=%v", by)
	}
	closed := newTestEngine(t)
	_ = closed.Store().Close()
	if len(closed.storedAssistantText("th_x", "tn_x")) != 0 {
		t.Fatal("a closed store must look empty, not panic")
	}
}

func TestAppendMissingEventAnswersSkipsJunkAndDuplicates(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "the leftover request")
	for _, ev := range []store.Event{
		{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, Text: "not an answer"},
		{ThreadID: th.ID, TurnID: "tn_compacted", Kind: swarm.NotifyAgentMessage.String(), AgentID: swarm.DefaultManagerID, Text: "folded away"},
		{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyAgentMessage.String(), AgentID: swarm.DefaultManagerID, Text: "   "},
		{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyAgentMessage.String(), AgentID: swarm.DefaultManagerID, Text: "keep this"},
		{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyAgentMessage.String(), AgentID: swarm.DefaultManagerID, Text: "keep this"},
	} {
		if err := e.Store().AppendEvent(&ev); err != nil {
			t.Fatal(err)
		}
	}
	dump := replayDump(t, e, th.ID)
	if strings.Contains(dump, "folded away") || strings.Contains(dump, "not an answer") {
		t.Fatalf("junk leaked into replay:\n%s", dump)
	}
	if strings.Count(dump, "keep this") != 1 {
		t.Fatalf("duplicate or missing answer:\n%s", dump)
	}
}

// Compact can cut through a turn. The event log still has the folded
// answers; replaying them (and worse, persisting them with a new seq)
// would undo /compact on the next crash-heal scan.
func TestReplayHistoryDoesNotUncompactFoldedAnswers(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "kept request")
	e.persistManagerAnswer(th.ID, turn.ID, "folded away on purpose")
	e.persistManagerAnswer(th.ID, turn.ID, "still live")
	msgs, err := e.Store().ListMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	var through int64
	for _, m := range msgs {
		if m.Content == "folded away on purpose" {
			through = m.Seq
		}
	}
	if through == 0 {
		t.Fatal("missing the folded row")
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{
		"compact_summary":     "briefing",
		"compact_through_seq": through,
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyAgentMessage.String(), AgentID: swarm.DefaultManagerID,
		Text: "folded away on purpose",
	}); err != nil {
		t.Fatal(err)
	}

	dump := replayDump(t, e, th.ID)
	if strings.Contains(dump, "folded away on purpose") {
		t.Fatalf("compacted answer leaked back into replay:\n%s", dump)
	}
	if !strings.Contains(dump, "still live") {
		t.Fatalf("live tail vanished:\n%s", dump)
	}

	after, err := e.Store().ListMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, m := range after {
		if m.Content != "folded away on purpose" {
			continue
		}
		n++
		if m.Seq > through {
			t.Fatalf("compacted answer was rewritten past the watermark: seq=%d through=%d", m.Seq, through)
		}
	}
	if n != 1 {
		t.Fatalf("want one stored copy of the folded answer, got %d", n)
	}
}

func TestAppendMissingEventAnswersReportsAClosedStore(t *testing.T) {
	e := newTestEngine(t)
	_ = e.Store().Close()
	_, err := e.appendMissingEventAnswers("th_x", map[string]struct{}{"tn": {}}, map[string]struct{}{}, nil)
	if err == nil {
		t.Fatal("a closed database must surface")
	}
}

func TestBlankAgentIdAnswersStillEnterReplay(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "the leftover request")
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyAgentMessage.String(),
		Text: "answer recorded without an agent id",
	}); err != nil {
		t.Fatal(err)
	}
	dump := replayDump(t, e, th.ID)
	if !strings.Contains(dump, "answer recorded without an agent id") {
		t.Fatalf("blank agent_id dropped:\n%s", dump)
	}
}

func TestPersistTranscriptSkipsAnswersAlreadyStored(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "the request")
	e.persistManagerAnswer(th.ID, turn.ID, "already stored")
	e.persistTranscript(th.ID, turn.ID, []adk.Message{
		schema.SystemMessage("instruction"),
		schema.UserMessage("the request"),
		nil,
		schema.UserMessage(resumeCue),
		schema.UserMessage("[steer] later"),
		schema.AssistantMessage("already stored", nil),
		schema.AssistantMessage("brand new", nil),
		{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{ID: "tc-1"}}},
	}, 1)

	msgs, err := e.Store().ListMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	var assistants, tools, cues, steers int
	for _, m := range msgs {
		switch {
		case m.Content == resumeCue:
			cues++
		case strings.HasPrefix(m.Content, "[steer]"):
			steers++
		case m.Role == "assistant" && strings.TrimSpace(m.Content) != "":
			assistants++
		case m.Role == "assistant" && m.ToolCalls != "":
			tools++
		}
	}
	if cues != 0 || steers != 0 {
		t.Fatalf("resume cue or steer leaked into messages: cues=%d steers=%d", cues, steers)
	}
	if assistants != 2 {
		t.Fatalf("want the live answer plus the new one, got %d content assistants: %+v", assistants, msgs)
	}
	if tools != 1 {
		t.Fatalf("want the tool-call row kept, got %d: %+v", tools, msgs)
	}
}

func TestPersistTranscriptNoopsWhenTheRunProducedNothingNew(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	e.persistTranscript(th.ID, "tn_x", []adk.Message{schema.SystemMessage("s"), schema.UserMessage("u")}, 1)
	msgs, err := e.Store().ListMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("empty tail stored rows: %+v", msgs)
	}
}

func TestPersistManagerAnswerSurvivesAClosedStore(t *testing.T) {
	e := newTestEngine(t)
	_ = e.Store().Close()
	e.persistManagerAnswer("th_x", "tn_x", "hello")
}

func TestResumeCueIsGenericAndGrounded(t *testing.T) {
	if strings.TrimSpace(resumeCue) == "" || strings.TrimSpace(resumeNotice) == "" {
		t.Fatal("the resume copy is empty")
	}
	blob := resumeCue + "\n" + resumeNotice
	for _, leak := range []string{
		"summarize", "researcher", "reviewer", "notes.md", "notes/",
		"look into this", "compare the two",
	} {
		if strings.Contains(strings.ToLower(blob), strings.ToLower(leak)) {
			t.Fatalf("the resume copy hardcodes example-specific text %q", leak)
		}
	}
}

// Quitting the app is not a user Stop. The turn stays unfinished so the next
// start can continue it.
func TestShutdownAbandonsALiveTurn(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.ManagerMaxIterations = 1
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	defer sub.Close()

	turn, err := e.StartTurn(th.ID, "paused at shutdown")
	if err != nil {
		t.Fatal(err)
	}
	waitLive(t, sub, KindMaxIterations, 15*time.Second)
	e.Shutdown()

	got, err := e.Store().GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.TurnRunning {
		t.Fatalf("shutdown cancelled a turn the user did not stop: %+v", got)
	}
}

func TestShutdownDoesNotCancelATurnThisProcessNeverRan(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "left by a previous process")
	e.Shutdown()

	got, err := e.Store().GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.TurnRunning {
		t.Fatalf("shutdown closed a leftover it did not own: %+v", got)
	}
}
