package store

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
)

// A manual send and automatic dequeue may both have read the same row.
// Reading it cannot grant delivery ownership; only its successful removal can.
func TestPopFollowupDoesNotReturnARowAlreadyClaimedForSteering(t *testing.T) {
	for _, remaining := range []bool{false, true} {
		t.Run(fmt.Sprintf("next_row_%t", remaining), func(t *testing.T) {
			s, thread, first := claimQueue(t)
			var next *Followup
			if remaining {
				var err error
				next, err = s.EnqueueFollowup(thread.ID, "the next request")
				if err != nil {
					t.Fatal(err)
				}
			}
			selected, release := pauseFirstFollowupRead(t, s)
			done := popInBackground(s, thread.ID)
			waitFollowupSelection(t, selected)

			claimed, err := s.TakeFollowup(thread.ID, first.ID)
			if err != nil || claimed.ID != first.ID {
				t.Fatalf("steering did not claim the selected request: %+v, %v", claimed, err)
			}
			release()
			got := <-done
			if got.err != nil {
				t.Fatal(got.err)
			}
			if !remaining && got.row != nil {
				t.Fatalf("automatic dequeue redelivered the steered request: %+v", got.row)
			}
			if remaining && (got.row == nil || got.row.ID != next.ID || got.row.Text != next.Text) {
				t.Fatalf("dequeue did not advance to the next request: %+v, want %+v", got.row, next)
			}
		})
	}
}

func TestConcurrentPopsClaimDifferentRowsInFIFOOrder(t *testing.T) {
	s, thread, first := claimQueue(t)
	second, err := s.EnqueueFollowup(thread.ID, "the next request")
	if err != nil {
		t.Fatal(err)
	}
	other := &Thread{Title: "another conversation"}
	if err := s.CreateThread(other); err != nil {
		t.Fatal(err)
	}
	untouched, err := s.EnqueueFollowup(other.ID, "another conversation's request")
	if err != nil {
		t.Fatal(err)
	}
	selected, release := pauseFirstFollowupRead(t, s)
	paused := popInBackground(s, thread.ID)
	waitFollowupSelection(t, selected)
	claimed, err := s.PopFollowup(thread.ID)
	if err != nil || claimed == nil || claimed.ID != first.ID {
		t.Fatalf("competing dequeue did not claim the oldest request: %+v, %v", claimed, err)
	}
	release()
	got := <-paused
	if got.err != nil || got.row == nil || got.row.ID != second.ID {
		t.Fatalf("paused dequeue redelivered or skipped a request: %+v, %v", got.row, got.err)
	}
	if _, err := s.GetFollowup(other.ID, untouched.ID); err != nil {
		t.Fatalf("dequeue consumed another conversation's request: %v", err)
	}
	if err := s.DeleteFollowup(thread.ID, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("steering must not claim a request already dequeued: %v", err)
	}
}

func TestPopFollowupReselectsTheQueueAfterTheSelectedRequestIsEdited(t *testing.T) {
	s, thread, selected := claimQueue(t)
	next, err := s.EnqueueFollowup(thread.ID, "the next request")
	if err != nil {
		t.Fatal(err)
	}
	read, release := pauseFirstFollowupRead(t, s)
	paused := popInBackground(s, thread.ID)
	waitFollowupSelection(t, read)
	edited, err := s.RequeueFollowup(thread.ID, selected.ID, "the revised request")
	if err != nil {
		t.Fatal(err)
	}
	release()
	got := <-paused
	if got.err != nil || got.row == nil || got.row.ID != next.ID {
		t.Fatalf("dequeue delivered the old edit or skipped the older request: %+v, %v", got.row, got.err)
	}
	got.row, got.err = s.PopFollowup(thread.ID)
	if got.err != nil || got.row == nil || got.row.ID != edited.ID || got.row.Seq != edited.Seq || got.row.Text != edited.Text {
		t.Fatalf("dequeue lost the revised request: %+v, want %+v, %v", got.row, edited, got.err)
	}
}

// A failed manual delivery restores the row at the front. That can reuse
// its former FIFO position, but must not revive a dequeuer's obsolete text.
func TestPopFollowupReturnsTheEditedRequestRestoredAtItsOldPosition(t *testing.T) {
	s, thread, selected := claimQueue(t)
	next, err := s.EnqueueFollowup(thread.ID, "the next request")
	if err != nil {
		t.Fatal(err)
	}
	read, release := pauseFirstFollowupRead(t, s)
	paused := popInBackground(s, thread.ID)
	waitFollowupSelection(t, read)
	if _, err := s.RequeueFollowup(thread.ID, selected.ID, "the revised request"); err != nil {
		t.Fatal(err)
	}
	restored, err := s.TakeFollowup(thread.ID, selected.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UnshiftFollowup(restored); err != nil {
		t.Fatal(err)
	}
	if restored.Seq != selected.Seq {
		t.Fatalf("fixture did not reuse the selected FIFO position: %d, want %d", restored.Seq, selected.Seq)
	}
	release()
	got := <-paused
	if got.err != nil || got.row == nil || got.row.ID != restored.ID || got.row.Text != restored.Text {
		t.Fatalf("dequeue discarded the restored edit: %+v, want %+v, %v", got.row, restored, got.err)
	}
	got.row, got.err = s.PopFollowup(thread.ID)
	if got.err != nil || got.row == nil || got.row.ID != next.ID {
		t.Fatalf("restored delivery changed the remaining FIFO order: %+v, %v", got.row, got.err)
	}
}

func TestRequeueFollowupDoesNotRestoreARequestAlreadyConsumed(t *testing.T) {
	for _, consumer := range []string{"pop", "manual"} {
		t.Run(consumer, func(t *testing.T) {
			s, thread, selected := claimQueue(t)
			read := make(chan struct{})
			resume := make(chan struct{})
			var paused atomic.Bool
			var once sync.Once
			release := func() { once.Do(func() { close(resume) }) }
			t.Cleanup(release)
			if err := s.db.Callback().Query().After("gorm:after_query").Register("test:pause_followup_edit", func(tx *gorm.DB) {
				if _, ok := tx.Statement.Dest.(*Followup); ok && tx.Error == nil && paused.CompareAndSwap(false, true) {
					close(read)
					<-resume
				}
			}); err != nil {
				t.Fatal(err)
			}
			done := make(chan popResult, 1)
			go func() {
				row, err := s.RequeueFollowup(thread.ID, selected.ID, "the revised request")
				done <- popResult{row, err}
			}()
			waitFollowupSelection(t, read)
			if consumer == "pop" {
				if row, err := s.PopFollowup(thread.ID); err != nil || row == nil || row.ID != selected.ID {
					t.Fatalf("dequeue did not consume the original request: %+v, %v", row, err)
				}
			} else if row, err := s.TakeFollowup(thread.ID, selected.ID); err != nil || row == nil || row.ID != selected.ID {
				t.Fatalf("manual send did not consume the original request: %+v, %v", row, err)
			}
			release()
			got := <-done
			if got.row != nil || !errors.Is(got.err, ErrNotFound) {
				t.Fatalf("editing a consumed request returned a fictional queued row: %+v, %v", got.row, got.err)
			}
			if left, err := s.ListFollowups(thread.ID); err != nil || len(left) != 0 {
				t.Fatalf("edit recreated a consumed request: %+v, %v", left, err)
			}
		})
	}
}

func TestConcurrentPopsDeliverEachQueuedRequestOnce(t *testing.T) {
	s, thread, first := claimQueue(t)
	want := map[string]bool{first.ID: true}
	const count = 16
	for i := 1; i < count; i++ {
		row, err := s.EnqueueFollowup(thread.ID, fmt.Sprintf("queued request %d", i))
		if err != nil {
			t.Fatal(err)
		}
		want[row.ID] = true
	}
	start := make(chan struct{})
	results := make(chan popResult, count)
	for i := 0; i < count; i++ {
		go func() {
			<-start
			row, err := s.PopFollowup(thread.ID)
			results <- popResult{row, err}
		}()
	}
	close(start)
	for i := 0; i < count; i++ {
		got := <-results
		if got.err != nil || got.row == nil || !want[got.row.ID] {
			t.Errorf("dequeue returned a duplicate, missing or unknown request: %+v, %v", got.row, got.err)
			continue
		}
		delete(want, got.row.ID)
	}
	if len(want) != 0 {
		t.Fatalf("requests were not delivered: %v", want)
	}
	left, err := s.ListFollowups(thread.ID)
	if err != nil || len(left) != 0 {
		t.Fatalf("delivered requests remained queued: %+v, %v", left, err)
	}
}

func TestPopFollowupDoesNotDeliverAfterStorageFailure(t *testing.T) {
	for _, phase := range []string{"read", "delete"} {
		t.Run(phase, func(t *testing.T) {
			s, thread, first := claimQueue(t)
			failure := errors.New("queue storage unavailable")
			if phase == "read" {
				if err := s.db.Callback().Query().Before("gorm:query").Register("test:refuse_followup_read", func(tx *gorm.DB) {
					if _, ok := tx.Statement.Dest.(*[]Followup); ok {
						_ = tx.AddError(failure)
					}
				}); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := s.db.Callback().Delete().Before("gorm:delete").Register("test:refuse_followup_delete", func(tx *gorm.DB) {
					if tx.Statement.Table == "followups" {
						_ = tx.AddError(failure)
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			got, err := s.PopFollowup(thread.ID)
			if got != nil || !errors.Is(err, failure) || !strings.Contains(err.Error(), "store: pop follow-up:") {
				t.Fatalf("failed storage became a delivery or lost its cause: %+v, %v", got, err)
			}
			if kept, err := s.GetFollowup(thread.ID, first.ID); err != nil || kept.Text != first.Text {
				t.Fatalf("failed dequeue lost the request: %+v, %v", kept, err)
			}
		})
	}
}

func TestPopFollowupRetriesABusyDeleteWithoutLosingTheRequest(t *testing.T) {
	s, thread, first := claimQueue(t)
	var attempts atomic.Int32
	if err := s.db.Callback().Delete().Before("gorm:delete").Register("test:busy_followup_delete", func(tx *gorm.DB) {
		if tx.Statement.Table == "followups" && attempts.Add(1) < 3 {
			_ = tx.AddError(errors.New("SQLITE_BUSY"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.PopFollowup(thread.ID)
	if err != nil || got == nil || got.ID != first.ID || attempts.Load() != 3 {
		t.Fatalf("busy retry lost or duplicated the request: %+v, %v, attempts=%d", got, err, attempts.Load())
	}
	if _, err := s.GetFollowup(thread.ID, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delivered request remained queued: %v", err)
	}
}

type popResult struct {
	row *Followup
	err error
}

func popInBackground(s *Store, threadID string) <-chan popResult {
	done := make(chan popResult, 1)
	go func() {
		row, err := s.PopFollowup(threadID)
		done <- popResult{row, err}
	}()
	return done
}

func claimQueue(t *testing.T) (*Store, *Thread, *Followup) {
	t.Helper()
	s := open(t)
	thread := &Thread{Title: "queued work"}
	if err := s.CreateThread(thread); err != nil {
		t.Fatal(err)
	}
	first, err := s.EnqueueFollowup(thread.ID, "the selected request")
	if err != nil {
		t.Fatal(err)
	}
	return s, thread, first
}

func pauseFirstFollowupRead(t *testing.T, s *Store) (<-chan struct{}, func()) {
	t.Helper()
	selected := make(chan struct{})
	resume := make(chan struct{})
	var paused atomic.Bool
	var once sync.Once
	release := func() { once.Do(func() { close(resume) }) }
	t.Cleanup(release)
	if err := s.db.Callback().Query().After("gorm:after_query").Register("test:pause_selected_followup", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*[]Followup); ok && tx.Error == nil && paused.CompareAndSwap(false, true) {
			close(selected)
			<-resume
		}
	}); err != nil {
		t.Fatal(err)
	}
	return selected, release
}

func waitFollowupSelection(t *testing.T, selected <-chan struct{}) {
	t.Helper()
	select {
	case <-selected:
	case <-time.After(5 * time.Second):
		t.Fatal("automatic dequeue did not reach its selected row")
	}
}
