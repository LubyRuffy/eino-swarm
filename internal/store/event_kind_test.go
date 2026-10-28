package store

import (
	"fmt"
	"path/filepath"
	"testing"
)

// History and inbox state must commit together even when a SQLite write fails.
func TestRewindDatabaseFailuresKeepTimelineAndInboxCursorTogether(t *testing.T) {
	for _, table := range []string{"events", "messages", "llm_calls", "turns", "followups", "threads"} {
		t.Run(table, func(t *testing.T) {
			s := open(t)
			th := &Thread{ID: "reader"}
			if err := s.CreateThread(th); err != nil {
				t.Fatal(err)
			}
			turn := seedTurn(t, s, th.ID, "user_message", "request")
			cut, err := firstUserEvent(s, turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			peer := &Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "task_message"}
			if err := s.AppendEvent(peer); err != nil {
				t.Fatal(err)
			}
			if err := s.AdvanceTaskMessageCursor(th.ID, peer.Seq); err != nil {
				t.Fatal(err)
			}
			if _, err := s.EnqueueFollowup(th.ID, "followup"); err != nil {
				t.Fatal(err)
			}
			op := "DELETE"
			if table == "threads" {
				op = "UPDATE"
			}
			if err := s.db.Exec(fmt.Sprintf("CREATE TRIGGER fail_rewind BEFORE %s ON %s BEGIN SELECT RAISE(ABORT, 'fixture write failure'); END", op, table)).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.TruncateFromEventSeq(th.ID, cut.Seq); err == nil {
				t.Fatal("failed write reported a successful rewind")
			}
			if _, err := s.GetEvent(th.ID, peer.Seq); err != nil {
				t.Fatalf("rollback lost peer message: %v", err)
			}
			got, err := s.GetThread(th.ID)
			if err != nil || got.TaskMessageThroughSeq != peer.Seq {
				t.Fatalf("rollback moved cursor: %+v %v", got, err)
			}
			if _, err := s.GetTurn(turn.ID); err != nil {
				t.Fatalf("rollback lost turn: %v", err)
			}
		})
	}
}

func TestInboxCursorExcludesOtherThreadsAndKinds(t *testing.T) {
	s := open(t)
	th := &Thread{ID: "a"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	withThreads(t, s, "other")
	for _, ev := range []Event{{ThreadID: "a", Kind: "task_message", SourceThreadID: "sender", SourceTurnID: "source", Text: "first"}, {ThreadID: "a", Kind: "tool_result"}, {ThreadID: "other", Kind: "task_message"}, {ThreadID: "a", Kind: "task_message", Text: "second"}} {
		if err := s.AppendEvent(&ev); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.ListEventsOfKind("a", "task_message", 0, 1)
	if err != nil || len(rows) != 1 || rows[0].Text != "first" || rows[0].SourceThreadID != "sender" || rows[0].SourceTurnID != "source" {
		t.Fatalf("page=%+v err=%v", rows, err)
	}
	rows, err = s.ListEventsOfKind("a", "task_message", rows[0].Seq, 1)
	if err != nil || len(rows) != 1 || rows[0].Text != "second" {
		t.Fatalf("cursor=%+v err=%v", rows, err)
	}
	s.Close()
	if err := s.AdvanceTaskMessageCursor("a", 2); err == nil {
		t.Fatal("closed database accepted a read cursor")
	}
	if _, err = s.ListEventsOfKind("a", "task_message", 0, 1); err == nil {
		t.Fatal("closed database claimed an empty inbox")
	}
}

func TestTaskMessageReadCursorOnlyMovesForward(t *testing.T) {
	s := open(t)
	th := &Thread{ID: "reader"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	for _, seq := range []int64{8, 3, 8} {
		if err := s.AdvanceTaskMessageCursor(th.ID, seq); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.GetThread(th.ID)
	if err != nil || got.TaskMessageThroughSeq != 8 {
		t.Fatalf("cursor=%+v err=%v", got, err)
	}
	if !got.UpdatedAt.Equal(th.UpdatedAt) {
		t.Fatal("reading the inbox changed the conversation's last activity")
	}
}

func TestExistingConversationMigrationCreatesAWorkingInboxCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateThread(&Thread{ID: "existing"}); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Exec("ALTER TABLE threads DROP COLUMN task_message_through_seq").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.AdvanceTaskMessageCursor("existing", 8); err != nil {
		t.Fatal(err)
	}
	th, err := s.GetThread("existing")
	if err != nil || th.TaskMessageThroughSeq != 8 {
		t.Fatalf("legacy inbox cursor cannot advance: %+v %v", th, err)
	}
}

func TestRewindThenRestartDoesNotHideNewPeerMessages(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "zwai.db")
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	th := &Thread{ID: "reader"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ID: "turn", ThreadID: th.ID}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	cut := &Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "user_message"}
	if err := s.AppendEvent(cut); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := s.AppendEvent(&Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "task_message"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AdvanceTaskMessageCursor(th.ID, 4); err != nil {
		t.Fatal(err)
	}
	if err := s.TruncateFromEventSeq(th.ID, cut.Seq); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.AppendEvent(&Event{ThreadID: th.ID, Kind: "task_message", Text: "new after restart"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListEventsOfKind(th.ID, "task_message", got.TaskMessageThroughSeq, 20)
	if err != nil || len(rows) != 1 || rows[0].Text != "new after restart" {
		t.Fatalf("new inbox hidden: cursor=%d rows=%+v err=%v", got.TaskMessageThroughSeq, rows, err)
	}
}
