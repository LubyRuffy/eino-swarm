package store

import (
	"errors"
	"fmt"
	"testing"
)

// A follow-up is what you typed while a turn was already running: it waits
// for that turn to finish rather than injecting into it. Losing the row on
// delete, or popping the second item first, is the user watching their own
// words vanish or fire out of order.
func TestFollowupsQueueInOrderAndLeaveWithTheConversation(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}

	first, err := s.EnqueueFollowup(th.ID, "after this finishes")
	if err != nil {
		t.Fatalf("EnqueueFollowup: %v", err)
	}
	if first.ID == "" || first.ThreadID != th.ID {
		t.Fatalf("enqueue did not fill the row: %+v", first)
	}
	second, err := s.EnqueueFollowup(th.ID, "then this")
	if err != nil {
		t.Fatal(err)
	}

	list, err := s.ListFollowups(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Text != "after this finishes" || list[1].ID != second.ID {
		t.Fatalf("want FIFO order, got %+v", list)
	}

	popped, err := s.PopFollowup(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if popped.ID != first.ID {
		t.Fatalf("popped %+v, want the oldest", popped)
	}
	left, _ := s.ListFollowups(th.ID)
	if len(left) != 1 || left[0].ID != second.ID {
		t.Fatalf("pop removed the wrong row: %+v", left)
	}

	if err := s.DeleteFollowup(th.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetFollowup(th.ID, second.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted follow-up still readable: %v", err)
	}
	if err := s.DeleteFollowup(th.ID, second.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting a missing follow-up: %v", err)
	}

	if _, err := s.EnqueueFollowup("missing", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("enqueue on a missing conversation: %v", err)
	}
	empty, err := s.PopFollowup(th.ID)
	if err != nil || empty != nil {
		t.Fatalf("pop on an empty queue should be nil, got %v %+v", err, empty)
	}

	_, _ = s.EnqueueFollowup(th.ID, "leftover")
	if err := s.DeleteThread(th.ID); err != nil {
		t.Fatal(err)
	}
	left, err = s.ListFollowups(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("deleting the conversation left follow-ups behind: %+v", left)
	}
}

func TestUnshiftPutsAFollowupBackAtTheFront(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	older, _ := s.EnqueueFollowup(th.ID, "older")
	newer, _ := s.EnqueueFollowup(th.ID, "newer")
	if err := s.DeleteFollowup(th.ID, older.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.UnshiftFollowup(older); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListFollowups(th.ID)
	if len(list) != 2 || list[0].ID != older.ID || list[1].ID != newer.ID {
		t.Fatalf("unshift did not restore FIFO: %+v", list)
	}
}

func TestRetryBusyRetriesALockedWriteThenSucceeds(t *testing.T) {
	n := 0
	err := retryBusy(5, func() error {
		n++
		if n == 1 {
			return fmt.Errorf("database is locked (5) (SQLITE_BUSY)")
		}
		return nil
	})
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}

	n = 0
	err = retryBusy(3, func() error {
		n++
		return fmt.Errorf("SQLITE_BUSY")
	})
	if err == nil || n != 3 {
		t.Fatalf("persistent lock n=%d err=%v", n, err)
	}

	n = 0
	err = retryBusy(5, func() error {
		n++
		return fmt.Errorf("no such table")
	})
	if n != 1 || err == nil {
		t.Fatalf("other errors must not retry n=%d err=%v", n, err)
	}
}

func TestUnshiftNilIsANoOp(t *testing.T) {
	s := open(t)
	if err := s.UnshiftFollowup(nil); err != nil {
		t.Fatal(err)
	}
	if isBusy(nil) {
		t.Fatal("a nil error is not a locked database")
	}
}

func TestUnshiftFillsMissingFieldsAndRejectsAMissingConversation(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "t"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	f := &Followup{ThreadID: th.ID, Text: "restored"}
	if err := s.UnshiftFollowup(f); err != nil {
		t.Fatal(err)
	}
	if f.ID == "" || f.CreatedAt.IsZero() {
		t.Fatalf("unshift left the row blank: %+v", f)
	}
	if err := s.UnshiftFollowup(&Followup{ThreadID: "missing", Text: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing conversation: %v", err)
	}
}
