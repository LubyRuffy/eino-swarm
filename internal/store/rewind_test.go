package store

import (
	"errors"
	"sync"
	"testing"
)

func TestTruncateFromEventSeqKeepsEarlierTurns(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	first := seedTurn(t, s, th.ID, "user_message", "first")
	second := seedTurn(t, s, th.ID, "user_message", "second")
	if _, err := s.EnqueueFollowup(th.ID, "waiting"); err != nil {
		t.Fatal(err)
	}

	user, err := firstUserEvent(s, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TruncateFromEventSeq(th.ID, user.Seq); err != nil {
		t.Fatalf("TruncateFromEventSeq: %v", err)
	}

	events, err := s.ListEvents(th.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.TurnID == second.ID {
			t.Fatalf("a later turn's event survived: %+v", ev)
		}
	}
	if len(events) == 0 {
		t.Fatal("the earlier turn's events were wiped")
	}

	turns, err := s.ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || turns[0].ID != first.ID {
		t.Fatalf("want only the earlier turn, got %+v", turns)
	}

	msgs, _ := s.ListMessages(th.ID)
	for _, m := range msgs {
		if m.TurnID == second.ID {
			t.Fatalf("a later turn's model message survived: %+v", m)
		}
	}
	if calls, _ := s.ListLLMCalls(second.ID); len(calls) != 0 {
		t.Fatalf("later turn's model calls survived: %+v", calls)
	}
	if followups, _ := s.ListFollowups(th.ID); len(followups) != 0 {
		t.Fatalf("queued follow-ups survived a rewind: %+v", followups)
	}

	next := &Event{ThreadID: th.ID, TurnID: first.ID, Kind: "notice", Text: "after"}
	if err := s.AppendEvent(next); err != nil {
		t.Fatal(err)
	}
	if next.Seq <= user.Seq {
		t.Fatalf("rewind reused a deleted seq: got %d, cut was %d", next.Seq, user.Seq)
	}
}

func TestGetEventLoadsTheRow(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := seedTurn(t, s, th.ID, "user_message", "first")
	want, err := firstUserEvent(s, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetEvent(th.ID, want.Seq)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "first" || got.Kind != "user_message" {
		t.Fatalf("got %+v", got)
	}
	if _, err := s.GetEvent(th.ID, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestTruncateFromEventSeqMissingTurnIsNotFound(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	ev := &Event{ThreadID: th.ID, TurnID: "tn_missing", Kind: "user_message", Text: "x"}
	if err := s.AppendEvent(ev); err != nil {
		t.Fatal(err)
	}
	if err := s.TruncateFromEventSeq(th.ID, ev.Seq); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestTruncateFromEventSeqMissingThreadIsNotFound(t *testing.T) {
	s := open(t)
	if err := s.TruncateFromEventSeq("th_missing", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestTruncateFromEventSeqMissingIsNotFound(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	if err := s.TruncateFromEventSeq(th.ID, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := s.TruncateFromEventSeq(th.ID, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("seq 0: want ErrNotFound, got %v", err)
	}
}

func TestTruncateFromEventSeqClearsABriefingThatCoveredDeletedMessages(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := seedTurn(t, s, th.ID, "user_message", "first")
	msgs, _ := s.ListMessages(th.ID)
	events, _ := s.ListTurnEvents(turn.ID)
	var eventThrough int64
	for _, ev := range events {
		if ev.Seq > eventThrough {
			eventThrough = ev.Seq
		}
	}
	if err := s.UpdateThread(th.ID, map[string]any{
		"compact_summary":            "briefing",
		"compact_through_seq":        msgs[len(msgs)-1].Seq,
		"session_memory":             "session briefing",
		"session_memory_through_seq": eventThrough,
		"session_memory_tokens":      12_000,
	}); err != nil {
		t.Fatal(err)
	}
	user, err := firstUserEvent(s, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TruncateFromEventSeq(th.ID, user.Seq); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetThread(th.ID)
	if got.CompactSummary != "" || got.CompactThroughSeq != 0 {
		t.Fatalf("briefing still points at deleted messages: %+v", got)
	}
	if got.SessionMemory != "" || got.SessionMemoryThroughSeq != 0 || got.SessionMemoryTokens != 0 {
		t.Fatalf("session briefing still points at deleted events: %+v", got)
	}
}

func TestTruncateFromEventSeqKeepsABriefingOfEarlierTurns(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	first := seedTurn(t, s, th.ID, "user_message", "first")
	second := seedTurn(t, s, th.ID, "user_message", "second")
	msgs, _ := s.ListMessages(th.ID)
	var through int64
	for _, m := range msgs {
		if m.TurnID == first.ID && m.Seq > through {
			through = m.Seq
		}
	}
	events, _ := s.ListTurnEvents(first.ID)
	var eventThrough int64
	for _, ev := range events {
		if ev.Seq > eventThrough {
			eventThrough = ev.Seq
		}
	}
	if err := s.UpdateThread(th.ID, map[string]any{
		"compact_summary":            "briefing",
		"compact_through_seq":        through,
		"session_memory":             "session briefing",
		"session_memory_through_seq": eventThrough,
		"session_memory_tokens":      12_000,
	}); err != nil {
		t.Fatal(err)
	}
	user, err := firstUserEvent(s, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TruncateFromEventSeq(th.ID, user.Seq); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetThread(th.ID)
	if got.CompactSummary != "briefing" || got.CompactThroughSeq != through {
		t.Fatalf("an earlier briefing was cleared: %+v", got)
	}
	if got.SessionMemory != "session briefing" || got.SessionMemoryThroughSeq != eventThrough {
		t.Fatalf("an earlier session briefing was cleared: %+v", got)
	}
}

func TestTruncateFromEventSeqClearsSessionMemoryWhenCompactWasNeverSet(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := seedTurn(t, s, th.ID, "user_message", "first")
	events, _ := s.ListTurnEvents(turn.ID)
	var through int64
	for _, ev := range events {
		if ev.Seq > through {
			through = ev.Seq
		}
	}
	if err := s.UpdateThread(th.ID, map[string]any{
		"session_memory":             "session briefing",
		"session_memory_through_seq": through,
		"session_memory_tokens":      12_000,
	}); err != nil {
		t.Fatal(err)
	}
	user, err := firstUserEvent(s, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TruncateFromEventSeq(th.ID, user.Seq); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetThread(th.ID)
	if got.SessionMemory != "" || got.SessionMemoryThroughSeq != 0 || got.SessionMemoryTokens != 0 {
		t.Fatalf("session briefing still points at deleted events: %+v", got)
	}
}

func TestTruncateFromEventSeqIgnoresAnEmptyCompactStamp(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := seedTurn(t, s, th.ID, "user_message", "first")
	events, _ := s.ListTurnEvents(turn.ID)
	var through int64
	for _, ev := range events {
		if ev.Seq > through {
			through = ev.Seq
		}
	}
	if err := s.UpdateThread(th.ID, map[string]any{
		"compact_through_seq":        99,
		"compact_summary":            "",
		"session_memory":             "session briefing",
		"session_memory_through_seq": through,
		"session_memory_tokens":      12_000,
	}); err != nil {
		t.Fatal(err)
	}
	user, err := firstUserEvent(s, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TruncateFromEventSeq(th.ID, user.Seq); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetThread(th.ID)
	if got.CompactThroughSeq != 99 {
		t.Fatalf("an empty compact stamp must not be rewritten: %+v", got)
	}
	if got.SessionMemory != "" || got.SessionMemoryThroughSeq != 0 {
		t.Fatalf("session briefing still points at deleted events: %+v", got)
	}
}

func TestTruncateFromEventSeqKeepsCompactWhenOnlySessionMemoryBroke(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	first := seedTurn(t, s, th.ID, "user_message", "first")
	second := seedTurn(t, s, th.ID, "user_message", "second")
	msgs, _ := s.ListMessages(th.ID)
	var through int64
	for _, m := range msgs {
		if m.TurnID == first.ID && m.Seq > through {
			through = m.Seq
		}
	}
	events, _ := s.ListTurnEvents(second.ID)
	var eventThrough int64
	for _, ev := range events {
		if ev.Seq > eventThrough {
			eventThrough = ev.Seq
		}
	}
	if err := s.UpdateThread(th.ID, map[string]any{
		"compact_summary":            "briefing",
		"compact_through_seq":        through,
		"session_memory":             "session briefing",
		"session_memory_through_seq": eventThrough,
		"session_memory_tokens":      12_000,
	}); err != nil {
		t.Fatal(err)
	}
	user, err := firstUserEvent(s, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TruncateFromEventSeq(th.ID, user.Seq); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetThread(th.ID)
	if got.CompactSummary != "briefing" || got.CompactThroughSeq != through {
		t.Fatalf("an earlier compact briefing was cleared: %+v", got)
	}
	if got.SessionMemory != "" || got.SessionMemoryThroughSeq != 0 {
		t.Fatalf("session briefing still points at deleted events: %+v", got)
	}
}

func TestTruncateFromEventSeqIgnoresAZeroSessionMemoryStamp(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := seedTurn(t, s, th.ID, "user_message", "first")
	if err := s.UpdateThread(th.ID, map[string]any{
		"session_memory":             "stale text without a stamp",
		"session_memory_through_seq": 0,
		"session_memory_tokens":      12,
	}); err != nil {
		t.Fatal(err)
	}
	user, err := firstUserEvent(s, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TruncateFromEventSeq(th.ID, user.Seq); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetThread(th.ID)
	if got.SessionMemory != "stale text without a stamp" || got.SessionMemoryThroughSeq != 0 {
		t.Fatalf("an unstamped leftover must not be treated as a broken briefing: %+v", got)
	}
}

func seedTurn(t *testing.T, s *Store, threadID, userKind, text string) *Turn {
	t.Helper()
	turn := &Turn{ThreadID: threadID, UserText: text}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(&Event{ThreadID: threadID, TurnID: turn.ID, Kind: userKind, Text: text}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(&Event{ThreadID: threadID, TurnID: turn.ID, Kind: "agent_message", Text: "answer"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessages(threadID, turn.ID, []Message{
		{Role: "user", Content: text},
		{Role: "assistant", Content: "answer"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLLMCall(&LLMCall{ThreadID: threadID, TurnID: turn.ID, Model: "m"}); err != nil {
		t.Fatal(err)
	}
	return turn
}

func TestGetEventAfterCloseIsAnError(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := seedTurn(t, s, th.ID, "user_message", "first")
	user, err := firstUserEvent(s, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetEvent(th.ID, user.Seq); err == nil {
		t.Fatal("a closed store must not pretend the event is there")
	}
	if _, err := s.ListFollowups(th.ID); err == nil {
		t.Fatal("a closed store must not list follow-ups")
	}
	if err := s.TruncateFromEventSeq(th.ID, user.Seq); err == nil {
		t.Fatal("a closed store must not truncate")
	}
}

// Rewind used to SQLITE_BUSY when a turn was still flushing events. One
// connection is the lock; this is the user-visible failure it prevented.
func TestTruncateDoesNotBusyAgainstAWriter(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := seedTurn(t, s, th.ID, "user_message", "first")
	user, err := firstUserEvent(s, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			_ = s.AppendEvent(&Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "delta"})
		}
	}()
	if err := s.TruncateFromEventSeq(th.ID, user.Seq); err != nil {
		t.Fatalf("truncate against a writer: %v", err)
	}
	wg.Wait()
}

func firstUserEvent(s *Store, turnID string) (*Event, error) {
	events, err := s.ListTurnEvents(turnID)
	if err != nil {
		return nil, err
	}
	for i := range events {
		if events[i].Kind == "user_message" {
			return &events[i], nil
		}
	}
	return nil, ErrNotFound
}
