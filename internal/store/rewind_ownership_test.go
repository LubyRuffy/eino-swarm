package store

import (
	"path/filepath"
	"testing"
)

// A fast title request may finish before the turn publishes its user event.
// Removing that turn must remove its evidence even when the cut seq is later.
func TestRewindDeletesMetadataOwnedByTheRemovedTurnBeforeItsUserEvent(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "rewind ownership"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	kept := seedTurn(t, s, th.ID, "user_message", "earlier input")
	dropped := &Turn{ThreadID: th.ID, UserText: "later input"}
	if err := s.CreateTurn(dropped); err != nil {
		t.Fatal(err)
	}
	title := &Event{ThreadID: th.ID, TurnID: dropped.ID, Kind: "title", AgentID: "title-namer", Text: "early metadata"}
	cut := &Event{ThreadID: th.ID, TurnID: dropped.ID, Kind: "user_message", AgentID: "manager", Text: dropped.UserText}
	for _, event := range []*Event{title, cut} {
		if err := s.AppendEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	if title.Seq >= cut.Seq {
		t.Fatal("metadata did not precede the cut")
	}
	if err := s.TruncateFromEventSeq(th.ID, cut.Seq); err != nil {
		t.Fatal(err)
	}
	events, err := s.ListEvents(th.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	keptEvidence := false
	for _, event := range events {
		if event.TurnID == dropped.ID {
			t.Fatalf("removed turn retained earlier evidence: %+v", event)
		}
		keptEvidence = keptEvidence || event.TurnID == kept.ID
	}
	if !keptEvidence {
		t.Fatal("rewind removed an earlier retained turn's evidence")
	}
}

// A peer can reach a newly admitted turn before its user event is recorded.
// Deleting that earlier owned event must not hide a reused seq after restart.
func TestRewindOwnedPreCutMessageDoesNotHideTheRestartInbox(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rewind.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	th := &Thread{Title: "rewind early peer"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: th.ID}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	peer := &Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "task_message", Text: "early peer"}
	cut := &Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "user_message"}
	for _, event := range []*Event{peer, cut} {
		if err := s.AppendEvent(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AdvanceTaskMessageCursor(th.ID, peer.Seq); err != nil {
		t.Fatal(err)
	}
	if err := s.TruncateFromEventSeq(th.ID, cut.Seq); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	next := &Event{ThreadID: th.ID, Kind: "task_message", Text: "new after restart"}
	if err := s.AppendEvent(next); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListEventsOfKind(th.ID, "task_message", got.TaskMessageThroughSeq, 20)
	if err != nil || len(rows) != 1 || rows[0].Text != next.Text || got.TaskMessageThroughSeq != 0 {
		t.Fatalf("new inbox hidden: cursor=%d newseq=%d rows=%+v err=%v", got.TaskMessageThroughSeq, next.Seq, rows, err)
	}
}

// Clamping a removed cursor may move it backwards, never past retained unread
// peers. Interleaved events can belong to different turns in one conversation.
func TestRewindCursorClampPreservesUnreadRetainedPeers(t *testing.T) {
	for _, read := range []string{"retained", "removed-before-unread", "removed-after-unread"} {
		t.Run(read, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rewind.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Close() })
			th := &Thread{Title: "rewind retained peers"}
			if err := s.CreateThread(th); err != nil {
				t.Fatal(err)
			}
			kept := seedTurn(t, s, th.ID, "user_message", "earlier input")
			dropped := &Turn{ThreadID: th.ID}
			if err := s.CreateTurn(dropped); err != nil {
				t.Fatal(err)
			}
			retained := &Event{ThreadID: th.ID, TurnID: kept.ID, Kind: "task_message", Text: "read retained peer"}
			early := &Event{ThreadID: th.ID, TurnID: dropped.ID, Kind: "task_message", Text: "removed before unread"}
			unread := &Event{ThreadID: th.ID, TurnID: kept.ID, Kind: "task_message", Text: "unread retained peer"}
			late := &Event{ThreadID: th.ID, TurnID: dropped.ID, Kind: "task_message", Text: "removed after unread"}
			cut := &Event{ThreadID: th.ID, TurnID: dropped.ID, Kind: "user_message"}
			for _, event := range []*Event{retained, early, unread, late, cut} {
				if err := s.AppendEvent(event); err != nil {
					t.Fatal(err)
				}
			}
			cursor := map[string]*Event{"retained": retained, "removed-before-unread": early, "removed-after-unread": late}[read].Seq
			if err := s.AdvanceTaskMessageCursor(th.ID, cursor); err != nil {
				t.Fatal(err)
			}
			if err := s.TruncateFromEventSeq(th.ID, cut.Seq); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			next := &Event{ThreadID: th.ID, Kind: "task_message", Text: "new after restart"}
			if err := s.AppendEvent(next); err != nil {
				t.Fatal(err)
			}
			got, err := s.GetThread(th.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantCursor, want := cursor, []string{unread.Text, next.Text}
			if read == "removed-after-unread" {
				wantCursor, want = unread.Seq, []string{next.Text}
			}
			rows, err := s.ListEventsOfKind(th.ID, "task_message", got.TaskMessageThroughSeq, 20)
			if err != nil || got.TaskMessageThroughSeq != wantCursor || len(rows) != len(want) {
				t.Fatalf("cursor=%d want=%d rows=%+v want=%v err=%v", got.TaskMessageThroughSeq, wantCursor, rows, want, err)
			}
			for i := range want {
				if rows[i].Text != want[i] {
					t.Fatalf("inbox[%d]=%q want=%q", i, rows[i].Text, want[i])
				}
			}
		})
	}
}
