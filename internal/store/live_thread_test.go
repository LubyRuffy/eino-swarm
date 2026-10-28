package store

import (
	"errors"
	"path/filepath"
	"testing"
)

// withThreads creates conversations under readable fixed ids. Owned rows are
// refused for a conversation that does not exist, so fixtures name theirs.
func withThreads(t testing.TB, s *Store, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if err := s.CreateThread(&Thread{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
}

// Deleting a conversation must remove its text from disk. A worker, a
// finishing turn or a post-turn review can still write after the delete
// commits; each such write has to be refused rather than recreate rows that
// no list, page or delete can reach again.
func TestWritesAfterDeleteLeaveNothingBehind(t *testing.T) {
	s := openTestStore(t)
	th := &Thread{Title: "doomed"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	tn := &Turn{ThreadID: th.ID}
	if err := s.CreateTurn(tn); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(&Event{ThreadID: th.ID, TurnID: tn.ID, Kind: "user_message", Text: "before"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteThread(th.ID); err != nil {
		t.Fatal(err)
	}

	writes := map[string]error{
		"turn":           s.CreateTurn(&Turn{ThreadID: th.ID}),
		"event":          s.AppendEvent(&Event{ThreadID: th.ID, TurnID: tn.ID, Kind: "reasoning", Text: "late"}),
		"terminal event": s.AppendEvent(&Event{ThreadID: th.ID, TurnID: tn.ID, Kind: "done", AgentID: "manager"}),
		"receipts":       s.AppendEventsAtomic([]*Event{{ThreadID: th.ID, TurnID: tn.ID, Kind: "tool_call"}, {ThreadID: th.ID, TurnID: tn.ID, Kind: "tool_result"}}),
		"messages":       s.AppendMessages(th.ID, tn.ID, []Message{{Role: "assistant", Content: "late answer"}}),
		"llm call":       s.AppendLLMCall(&LLMCall{ThreadID: th.ID, TurnID: tn.ID}),
		"attachment":     s.AddAttachment(&Attachment{ThreadID: th.ID, Name: "late.txt", RelPath: "late.txt"}),
	}
	for name, err := range writes {
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("%s written after delete: err = %v, want ErrNotFound", name, err)
		}
	}
	for _, table := range []string{"turns", "events", "messages", "llm_calls", "attachments"} {
		var n int64
		if err := s.db.Table(table).Where("thread_id = ?", th.ID).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%d %s rows outlived the deleted conversation", n, table)
		}
	}
	s.mu.Lock()
	_, evt := s.evtSeq[th.ID]
	_, msg := s.msgSeq[th.ID]
	_, turn := s.turnSeq[th.ID]
	s.mu.Unlock()
	if evt || msg || turn {
		t.Errorf("refused writes left sequence counters behind: event=%v message=%v turn=%v", evt, msg, turn)
	}
}

// legacySchema rewinds a store to a schema from before the ownership guards,
// so a test can rebuild tables the way an old database looked.
func legacySchema(t testing.TB, s *Store) {
	t.Helper()
	if err := s.dropThreadOwnership(); err != nil {
		t.Fatal(err)
	}
}

// A schema change that rebuilds threads must not stop the app from starting.
// SQLite checks every trigger when the rebuilt table is renamed into place,
// so Open lifts the guards for its migration and puts them back after.
func TestSchemaRebuildOfThreadsStillOpens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rebuild.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DB().Migrator().DropColumn(&Thread{}, "goal_idle"); err == nil {
		t.Fatal("rebuilding threads under the guards succeeded; Open's guard lift may be unnecessary")
	}
	legacySchema(t, s)
	if err := s.DB().Migrator().DropColumn(&Thread{}, "goal_idle"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatalf("open after a threads rebuild: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if !s.DB().Migrator().HasColumn(&Thread{}, "goal_idle") {
		t.Fatal("migration did not restore the column")
	}
	err = s.DB().Create(&Turn{ID: "tn_gone", ThreadID: "gone"}).Error
	if !errors.Is(s.threadWriteErr("gone", err), ErrNotFound) {
		t.Fatalf("guards not restored after migration: %v", err)
	}
}

// A rewind deletes the removed turns with their model calls. A reviewer call
// that finishes afterwards must not store a row under a turn that is gone,
// while calls under a kept turn still land.
func TestModelCallUnderARewoundTurnIsRefused(t *testing.T) {
	s := openTestStore(t)
	th := &Thread{Title: "rewound"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	kept, removed := &Turn{ThreadID: th.ID}, &Turn{ThreadID: th.ID}
	for _, turn := range []*Turn{kept, removed} {
		if err := s.CreateTurn(turn); err != nil {
			t.Fatal(err)
		}
		if err := s.AppendEvent(&Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "user_message"}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := s.ListTurnEvents(removed.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%v err=%v", events, err)
	}
	if err := s.TruncateFromEventSeq(th.ID, events[0].Seq); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendLLMCall(&LLMCall{ThreadID: th.ID, TurnID: removed.ID, AgentID: "memory-reviewer"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("model call under a rewound turn: %v", err)
	}
	if err := s.AppendLLMCall(&LLMCall{ThreadID: th.ID, TurnID: kept.ID, AgentID: "memory-reviewer"}); err != nil {
		t.Fatalf("model call under a kept turn: %v", err)
	}
	if calls, _ := s.ListLLMCalls(removed.ID); len(calls) != 0 {
		t.Fatalf("rows under the rewound turn: %+v", calls)
	}
}

// A standalone schedule's namer records its model call with no conversation.
// That row belongs to nobody's deletion and must still be stored.
func TestModelCallWithoutAConversationIsStillRecorded(t *testing.T) {
	s := openTestStore(t)
	call := &LLMCall{AgentID: "title", Model: "namer"}
	if err := s.AppendLLMCall(call); err != nil {
		t.Fatalf("conversation-less model call refused: %v", err)
	}
	if call.ID == 0 {
		t.Fatal("model call was not stored")
	}
}

// The guard lives in the database file, so it holds for a reopened store and
// for a write that bypasses the Store API.
func TestOwnershipGuardSurvivesReopenAndRawInserts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "guard.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen with existing guards: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	err = s.DB().Create(&Event{ThreadID: "gone", Seq: 1, Kind: "reasoning"}).Error
	if !errors.Is(s.threadWriteErr("gone", err), ErrNotFound) {
		t.Fatalf("raw insert under a missing conversation: %v", err)
	}
	if _, err := s.EnqueueFollowup("gone", "late"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("follow-up for a missing conversation: %v", err)
	}
}

// The guard must not cost a live conversation its writes, including a
// terminal event whose watermark update shares the transaction.
func TestLiveConversationWritesStillLand(t *testing.T) {
	s := openTestStore(t)
	th := &Thread{Title: "alive"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	tn := &Turn{ThreadID: th.ID}
	if err := s.CreateTurn(tn); err != nil {
		t.Fatal(err)
	}
	done := &Event{ThreadID: th.ID, TurnID: tn.ID, Kind: "done", AgentID: "manager"}
	if err := s.AppendEvent(done); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessages(th.ID, tn.ID, []Message{{Role: "assistant", Content: "answer"}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TerminalSeqHighWater != done.Seq || got.CompletedSeq != done.Seq {
		t.Fatalf("terminal watermarks = %d/%d, want %d", got.TerminalSeqHighWater, got.CompletedSeq, done.Seq)
	}
	if msgs := mustMessages(t, s, th.ID); len(msgs) != 1 {
		t.Fatalf("messages = %d, want 1", len(msgs))
	}
}
