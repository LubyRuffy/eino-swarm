package store

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"gorm.io/gorm"
)

func TestTakeFollowupReturnsTheContentsDeletedAfterAConcurrentEdit(t *testing.T) {
	s, thread, selected := claimQueue(t)
	var edited *Followup
	var updated atomic.Bool
	if err := s.db.Callback().Delete().Before("gorm:delete").Register("test:edit_before_followup_take", func(tx *gorm.DB) {
		if tx.Statement.Table != "followups" || !updated.CompareAndSwap(false, true) {
			return
		}
		var err error
		edited, err = s.RequeueFollowup(thread.ID, selected.ID, "the revised request")
		if err != nil {
			_ = tx.AddError(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.TakeFollowup(thread.ID, selected.ID)
	if err != nil || got == nil || edited == nil {
		t.Fatalf("claim failed: %+v, %v", got, err)
	}
	if got.ID != edited.ID || got.ThreadID != edited.ThreadID || got.Seq != edited.Seq ||
		got.Text != edited.Text || !got.CreatedAt.Equal(edited.CreatedAt) {
		t.Fatalf("claim returned stale contents or identity: %+v, want %+v", got, edited)
	}
	if _, err := s.GetFollowup(thread.ID, selected.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("claimed request remained queued: %v", err)
	}
}

func TestTakeFollowupRejectsMissingOrWrongConversationRows(t *testing.T) {
	s, thread, selected := claimQueue(t)
	other := &Thread{Title: "another conversation"}
	if err := s.CreateThread(other); err != nil {
		t.Fatal(err)
	}
	for _, request := range [][2]string{{other.ID, selected.ID}, {thread.ID, "fu_missing"}} {
		if got, err := s.TakeFollowup(request[0], request[1]); got != nil || !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing row was claimed: %+v, %v", got, err)
		}
	}
	if got, err := s.GetFollowup(thread.ID, selected.ID); err != nil || got.Text != selected.Text {
		t.Fatalf("wrong-conversation claim consumed the request: %+v, %v", got, err)
	}
}

func TestConcurrentTakesGrantOnlyOneDeliveryOwner(t *testing.T) {
	s, thread, selected := claimQueue(t)
	const count = 8
	start := make(chan struct{})
	results := make(chan popResult, count)
	for i := 0; i < count; i++ {
		go func() {
			<-start
			row, err := s.TakeFollowup(thread.ID, selected.ID)
			results <- popResult{row, err}
		}()
	}
	close(start)
	owners := 0
	for i := 0; i < count; i++ {
		got := <-results
		if got.err == nil && got.row != nil && got.row.ID == selected.ID && got.row.Text == selected.Text {
			owners++
		} else if got.row != nil || !errors.Is(got.err, ErrNotFound) {
			t.Errorf("losing claim changed the missing-row semantics: %+v, %v", got.row, got.err)
		}
	}
	if owners != 1 {
		t.Fatalf("one queued request had %d delivery owners", owners)
	}
}

func TestTakeFollowupPreservesTheRequestWhenDeletionFails(t *testing.T) {
	s, thread, selected := claimQueue(t)
	failure := errors.New("queue storage unavailable")
	if err := s.db.Callback().Delete().Before("gorm:delete").Register("test:refuse_followup_take", func(tx *gorm.DB) {
		if tx.Statement.Table == "followups" {
			_ = tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.TakeFollowup(thread.ID, selected.ID)
	if got != nil || !errors.Is(err, failure) || !strings.Contains(err.Error(), "store: take follow-up:") {
		t.Fatalf("failed claim delivered a request or lost its cause: %+v, %v", got, err)
	}
	if kept, err := s.GetFollowup(thread.ID, selected.ID); err != nil || kept.Text != selected.Text {
		t.Fatalf("failed claim lost the queued request: %+v, %v", kept, err)
	}
}
