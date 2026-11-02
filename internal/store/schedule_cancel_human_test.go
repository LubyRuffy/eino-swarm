package store

import (
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
)

func humanCancelFixture(t *testing.T) (*Store, *Schedule, Event) {
	t.Helper()
	s := open(t)
	thread := mustThread(t, s)
	row := mustSchedule(t, s, &Schedule{Kind: ScheduleThread, ThreadID: thread.ID,
		Title: "explicit cancellation fixture", Prompt: "inspect outstanding work", Status: ScheduleActive,
		NextRunAt: time.Now().UTC().Add(time.Hour)})
	return s, row, humanCancelEvent(row)
}

func humanCancelEvent(row *Schedule) Event {
	return Event{ThreadID: row.ThreadID, Kind: "schedule_cancelled", AgentID: "manager", Role: "human", Text: row.ID}
}

// Deleting a wait must persist the action and its authority together, rather
// than merely hiding a card that the next manager turn can create again.
func TestHumanCancelStoresOneDurableDenial(t *testing.T) {
	for _, status := range []string{ScheduleActive, SchedulePaused, ScheduleCancelled, ScheduleDone} {
		t.Run(status, func(t *testing.T) {
			s, row, event := humanCancelFixture(t)
			if err := s.DB().Model(row).Update("status", status).Error; err != nil {
				t.Fatal(err)
			}
			changed, err := s.CancelScheduleByHuman(row.ID, &event)
			if err != nil {
				t.Fatal(err)
			}
			if changed == nil {
				t.Fatalf("change=%+v for old status %s", changed, status)
			}
			got, err := s.GetSchedule(row.ID)
			if err != nil || got.Status != ScheduleCancelled {
				t.Fatalf("cancelled row=%+v err=%v", got, err)
			}
			policy, inputs, err := s.ReadThreadWakePolicy(row.ThreadID)
			if err != nil || !policy.Denied || policy.PendingSeq != 0 {
				t.Fatalf("policy=%+v inputs=%+v err=%v", policy, inputs, err)
			}
			if event.ID == 0 || event.Seq == 0 || event.CreatedAt.IsZero() || len(inputs) != 1 || inputs[0].Decision != WakeDecisionDeny || policy.SourceKind != "schedule_cancelled" || policy.SourceText != row.ID || policy.SourceSeq != event.Seq || policy.Quote != "" {
				t.Fatalf("cancellation authority lost: event=%+v policy=%+v inputs=%+v", event, policy, inputs)
			}
			repeat := humanCancelEvent(row)
			if changed, err := s.CancelScheduleByHuman(row.ID, &repeat); err != nil || changed != nil {
				t.Fatalf("duplicate cancellation changed state: %+v %v", changed, err)
			}
			events, err := s.ListEvents(row.ThreadID, 0, 0)
			if err != nil || len(events) != 1 {
				t.Fatalf("duplicate cancellation receipt: %+v %v", events, err)
			}
		})
	}
}

func TestRepeatedHumanDeletionKeepsLaterExplicitAllowance(t *testing.T) {
	s, row, first := humanCancelFixture(t)
	if changed, err := s.CancelScheduleByHuman(row.ID, &first); err != nil || changed == nil {
		t.Fatalf("first cancellation: %+v %v", changed, err)
	}
	allow := wakeHuman(t, s, row.ThreadID, "user_message", "You may schedule the next check in this conversation.")
	saveWakeDecision(t, s, row.ThreadID, allow, WakeDecisionAllow, allow.Text)
	repeat := humanCancelEvent(row)
	if changed, err := s.CancelScheduleByHuman(row.ID, &repeat); err != nil || changed != nil {
		t.Fatalf("replayed deletion became a new action: %+v %v", changed, err)
	}
	policy, _, err := s.ReadThreadWakePolicy(row.ThreadID)
	if err != nil || policy.Denied || policy.SourceSeq != allow.Seq {
		t.Fatalf("old deletion revoked a later human allowance: %+v %v", policy, err)
	}
	var count int64
	if err := s.DB().Model(&Event{}).Where("thread_id = ? AND kind = ? AND role = ?", row.ThreadID, "schedule_cancelled", "human").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("duplicate human receipt: %d %v", count, err)
	}
}

func TestTerminalHumanCancellationDoesNotTrustOtherCancellationActors(t *testing.T) {
	for _, prior := range []Event{
		{Kind: "schedule_cancelled", Role: "manager"},
		{Kind: "schedule_cancelled"},
		{Kind: "schedule_cancelled", Role: "human", SourceThreadID: "peer"},
		{Kind: "schedule_cancelled", Role: "human", SourceTurnID: "peer"},
		{Kind: "schedule_cancelled", Role: "human", Text: "another-wait"},
	} {
		t.Run(prior.Role+prior.SourceThreadID+prior.SourceTurnID+prior.Text, func(t *testing.T) {
			s, row, event := humanCancelFixture(t)
			if _, err := s.CancelSchedule(row.ID); err != nil {
				t.Fatal(err)
			}
			prior.ThreadID = row.ThreadID
			if prior.Text == "" {
				prior.Text = row.ID
			}
			if err := s.AppendEvent(&prior); err != nil {
				t.Fatal(err)
			}
			if changed, err := s.CancelScheduleByHuman(row.ID, &event); err != nil || changed == nil {
				t.Fatalf("another actor masked the first real human cancellation: %+v %v", changed, err)
			}
			policy, _, err := s.ReadThreadWakePolicy(row.ThreadID)
			if err != nil || !policy.Denied || policy.SourceSeq != event.Seq || policy.SourceText != row.ID {
				t.Fatalf("human action was not the authority: %+v %v", policy, err)
			}
		})
	}
}

func TestHumanCancellationCannotIgnoreReceiptReadFailure(t *testing.T) {
	s, row, event := humanCancelFixture(t)
	cause := errors.New("human receipt query unavailable")
	if err := s.DB().Callback().Query().Before("gorm:query").Register("cancel_receipt_read_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "events" {
			tx.AddError(cause)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.CancelScheduleByHuman(row.ID, &event); changed != nil || !errors.Is(err, cause) {
		t.Fatalf("unknown prior authority was accepted: %+v %v", changed, err)
	}
	var status string
	if err := s.DB().Raw("SELECT status FROM schedules WHERE id = ?", row.ID).Scan(&status).Error; err != nil || status != ScheduleActive {
		t.Fatalf("failed receipt lookup partly cancelled the wait: %s %v", status, err)
	}
}

func TestHumanCancelRollsBackWithoutItsReceipt(t *testing.T) {
	for _, failure := range []string{"event", "status", "schedule-query", "thread-query"} {
		t.Run(failure, func(t *testing.T) {
			s, row, event := humanCancelFixture(t)
			cause := errors.New("private cancellation storage fault")
			fail := func(tx *gorm.DB) {
				if failure == "event" && tx.Statement.Table == "events" || failure == "status" && tx.Statement.Table == "schedules" || failure == "schedule-query" && tx.Statement.Table == "schedules" || failure == "thread-query" && tx.Statement.Table == "threads" {
					tx.AddError(cause)
				}
			}
			switch failure {
			case "event":
				if err := s.DB().Callback().Create().Before("gorm:create").Register("test_human_cancel_failure", fail); err != nil {
					t.Fatal(err)
				}
			case "status":
				if err := s.DB().Callback().Update().Before("gorm:update").Register("test_human_cancel_failure", fail); err != nil {
					t.Fatal(err)
				}
			default:
				if err := s.DB().Callback().Query().Before("gorm:query").Register("test_human_cancel_failure", fail); err != nil {
					t.Fatal(err)
				}
			}
			if changed, err := s.CancelScheduleByHuman(row.ID, &event); changed != nil || !errors.Is(err, cause) {
				t.Fatalf("failed write reported cancellation: %+v %v", changed, err)
			}
			// Raw reads leave the private fault callback installed for the store's
			// lifetime while checking both sides of the rolled-back transaction.
			var status string
			var count int64
			if err := s.DB().Raw("SELECT status FROM schedules WHERE id = ?", row.ID).Scan(&status).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.DB().Raw("SELECT COUNT(*) FROM events WHERE thread_id = ?", row.ThreadID).Scan(&count).Error; err != nil {
				t.Fatal(err)
			}
			if status != ScheduleActive || count != 0 {
				t.Fatalf("partial cancellation survived: status=%s events=%d", status, count)
			}
		})
	}
}

func TestHumanCancelRequiresTheExactTargetAndLocalAction(t *testing.T) {
	for _, change := range []string{"nil", "thread", "kind", "role", "id", "peer-thread", "peer-turn", "retarget-thread", "retarget-kind", "missing-schedule", "missing-thread", "seed-failure"} {
		t.Run(change, func(t *testing.T) {
			s, row, event := humanCancelFixture(t)
			ptr := &event
			id := row.ID
			var want error
			switch change {
			case "nil":
				ptr = nil
			case "thread":
				event.ThreadID = ""
			case "kind":
				event.Kind = "user_message"
			case "role":
				event.Role = "manager"
			case "id":
				event.Text = "another-schedule"
			case "peer-thread":
				event.SourceThreadID = "peer"
			case "peer-turn":
				event.SourceTurnID = "peer"
			case "retarget-thread":
				if err := s.DB().Model(row).Update("thread_id", "other-target").Error; err != nil {
					t.Fatal(err)
				}
			case "retarget-kind":
				if err := s.DB().Model(row).Update("kind", ScheduleStandalone).Error; err != nil {
					t.Fatal(err)
				}
			case "missing-schedule":
				id, event.Text, want = "missing", "missing", ErrNotFound
			case "missing-thread":
				if err := s.DB().Delete(&Thread{}, "id = ?", row.ThreadID).Error; err != nil {
					t.Fatal(err)
				}
				want = ErrNotFound
			case "seed-failure":
				if err := s.DB().Migrator().DropTable(&Event{}); err != nil {
					t.Fatal(err)
				}
			}
			changed, err := s.CancelScheduleByHuman(id, ptr)
			if changed != nil || err == nil || want != nil && !errors.Is(err, want) {
				t.Fatalf("invalid action was accepted: %+v %v", changed, err)
			}
			got, getErr := s.GetSchedule(row.ID)
			if getErr != nil || got.Status != ScheduleActive {
				t.Fatalf("refusal cancelled the schedule: %+v %v", got, getErr)
			}
		})
	}
}

func TestConcurrentHumanCancellationWritesOneAction(t *testing.T) {
	s, row, _ := humanCancelFixture(t)
	var changed atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			event := humanCancelEvent(row)
			got, err := s.CancelScheduleByHuman(row.ID, &event)
			if err != nil {
				t.Errorf("cancel: %v", err)
			}
			if got != nil {
				changed.Add(1)
			}
		}()
	}
	wg.Wait()
	events, err := s.ListEvents(row.ThreadID, 0, 0)
	if err != nil || changed.Load() != 1 || len(events) != 1 || events[0].Role != "human" {
		t.Fatalf("duplicate actions: changes=%d events=%+v err=%v", changed.Load(), events, err)
	}
}

func TestHumanCancellationSurvivesRestartAndOrdinaryContinue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "human-cancellation.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	thread := mustThread(t, s)
	row := mustSchedule(t, s, &Schedule{Kind: ScheduleThread, ThreadID: thread.ID,
		Title: "restart cancellation fixture", Status: ScheduleActive, NextRunAt: time.Now().UTC().Add(time.Hour)})
	event := humanCancelEvent(row)
	if _, err := s.CancelScheduleByHuman(row.ID, &event); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateThread(row.ThreadID, map[string]any{"compact_through_seq": 999, "compact_summary": "Continue the outstanding work."}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	inputs, err := reopened.WakePolicyInputs(row.ThreadID)
	if err != nil || len(inputs) != 1 || inputs[0].Decision != WakeDecisionDeny {
		t.Fatalf("restart erased cancellation: %+v %v", inputs, err)
	}
	for _, decision := range []string{WakeDecisionAllow, WakeDecisionUnchanged, WakeDecisionDeny} {
		if err := reopened.SaveWakeDecision(row.ThreadID, inputs[0], decision, ""); !errors.Is(err, ErrWakeDecisionConflict) {
			t.Fatalf("classifier rewrote UI action: decision=%s err=%v", decision, err)
		}
	}
	continued := wakeHuman(t, reopened, row.ThreadID, "user_message", "Continue the current work.")
	saveWakeDecision(t, reopened, row.ThreadID, continued, WakeDecisionUnchanged, "")
	policy, _, err := reopened.ReadThreadWakePolicy(row.ThreadID)
	if err != nil || !policy.Denied || policy.SourceSeq != event.Seq || policy.SourceKind != event.Kind || policy.PendingSeq != 0 {
		t.Fatalf("ordinary continue revived waiting: %+v %v", policy, err)
	}
	allow := wakeHuman(t, reopened, row.ThreadID, "steer", "You may set another scheduled check for this conversation.")
	saveWakeDecision(t, reopened, row.ThreadID, allow, WakeDecisionAllow, allow.Text)
	policy, _, err = reopened.ReadThreadWakePolicy(row.ThreadID)
	if err != nil || policy.Denied || policy.SourceSeq != allow.Seq || policy.SourceKind != "steer" {
		t.Fatalf("later explicit permission did not win: %+v %v", policy, err)
	}
}

func TestOnlyLocalHumanCancellationRevokesWakePermission(t *testing.T) {
	s, row, _ := humanCancelFixture(t)
	for _, event := range []Event{
		{Role: "", Kind: "schedule_cancelled"},
		{Role: "manager", Kind: "schedule_cancelled"},
		{Role: "runtime", Kind: "schedule_cancelled"},
		{Role: "human", Kind: "schedule_cancelled", SourceThreadID: "peer"},
		{Role: "human", Kind: "schedule_cancelled", SourceTurnID: "peer"},
		{Role: "human", Kind: "assistant"},
	} {
		event.ThreadID, event.Text = row.ThreadID, row.ID
		if err := s.AppendEvent(&event); err != nil {
			t.Fatal(err)
		}
	}
	policy, inputs, err := s.ReadThreadWakePolicy(row.ThreadID)
	if err != nil || policy.Denied || policy.PendingSeq != 0 || len(inputs) != 0 {
		t.Fatalf("model or peer cancellation became human authority: %+v %+v %v", policy, inputs, err)
	}
}
