package store

import (
	"errors"
	"testing"

	"gorm.io/gorm"
)

// A cancellation admitted for an independent task cannot silently become a
// cancellation of a retargeted conversation wait without human wake authority.
func TestCancellationRefusesAChangedDestination(t *testing.T) {
	for _, change := range []string{"kind", "thread"} {
		t.Run(change, func(t *testing.T) {
			s, row, _ := humanCancelFixture(t)
			expectedThreadID := row.ThreadID
			fields := map[string]any{"kind": ScheduleStandalone, "thread_id": ""}
			if change == "thread" {
				fields["kind"] = ScheduleThread
				fields["thread_id"] = "another-target"
			}
			if err := s.DB().Model(row).Updates(fields).Error; err != nil {
				t.Fatal(err)
			}
			if changed, err := s.CancelScheduleAtDestination(row.ID, ScheduleThread, expectedThreadID); err == nil || changed != nil {
				t.Fatalf("retargeted cancellation accepted: %+v %v", changed, err)
			}
			stored, err := s.GetSchedule(row.ID)
			if err != nil || stored.Status != ScheduleActive {
				t.Fatalf("retargeted wait was cancelled: %+v %v", stored, err)
			}
		})
	}
}

func TestIndependentCancellationCannotReachARetargetedThread(t *testing.T) {
	s, row, _ := humanCancelFixture(t)
	threadID := row.ThreadID
	if err := s.DB().Model(row).Updates(map[string]any{"kind": ScheduleStandalone, "thread_id": ""}).Error; err != nil {
		t.Fatal(err)
	}
	admitted, err := s.GetSchedule(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DB().Model(row).Updates(map[string]any{"kind": ScheduleThread, "thread_id": threadID}).Error; err != nil {
		t.Fatal(err)
	}
	if changed, err := s.CancelScheduleAtDestination(row.ID, admitted.Kind, admitted.ThreadID); err == nil || changed != nil {
		t.Fatalf("independent cancellation reached a new thread target: %+v %v", changed, err)
	}
	stored, err := s.GetSchedule(row.ID)
	if err != nil || stored.Status != ScheduleActive {
		t.Fatalf("retargeted thread was cancelled without authority: %+v %v", stored, err)
	}
}

func TestCancellationAtDestinationKeepsNormalLifecycle(t *testing.T) {
	for _, status := range []string{ScheduleActive, SchedulePaused, ScheduleDone, ScheduleCancelled} {
		t.Run(status, func(t *testing.T) {
			s, row, _ := humanCancelFixture(t)
			if err := s.DB().Model(row).Updates(map[string]any{"kind": ScheduleStandalone, "thread_id": nil, "status": status}).Error; err != nil {
				t.Fatal(err)
			}
			changed, err := s.CancelScheduleAtDestination(row.ID, ScheduleStandalone, "")
			wantChange := status == ScheduleActive || status == SchedulePaused
			if err != nil || (changed != nil) != wantChange {
				t.Fatalf("matching cancellation: %+v %v", changed, err)
			}
			if changed != nil && changed.Status != ScheduleCancelled {
				t.Fatalf("changed row is not cancelled: %+v", changed)
			}
		})
	}
	s, row, _ := humanCancelFixture(t)
	if changed, err := s.CancelScheduleAtDestination("missing", ScheduleStandalone, ""); changed != nil || !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing cancellation: %+v %v", changed, err)
	}
	// A matching thread destination uses the same atomic predicate. Human callers
	// still use CancelScheduleByHuman to commit the accompanying authority.
	if changed, err := s.CancelScheduleAtDestination(row.ID, row.Kind, row.ThreadID); err != nil || changed == nil {
		t.Fatalf("matching thread cancellation: %+v %v", changed, err)
	}
}

func TestCancellationAtDestinationPreservesStorageErrors(t *testing.T) {
	for _, action := range []string{"write", "read"} {
		t.Run(action, func(t *testing.T) {
			s, row, _ := humanCancelFixture(t)
			cause := errors.New("private destination cancellation fault")
			if action == "read" {
				if err := s.DB().Model(row).Update("status", ScheduleDone).Error; err != nil {
					t.Fatal(err)
				}
				if err := s.DB().Callback().Query().Before("gorm:query").Register("test_destination_cancel_read", func(tx *gorm.DB) { tx.AddError(cause) }); err != nil {
					t.Fatal(err)
				}
			} else if err := s.DB().Callback().Update().Before("gorm:update").Register("test_destination_cancel_write", func(tx *gorm.DB) { tx.AddError(cause) }); err != nil {
				t.Fatal(err)
			}
			if changed, err := s.CancelScheduleAtDestination(row.ID, row.Kind, row.ThreadID); changed != nil || !errors.Is(err, cause) {
				t.Fatalf("lost storage failure: %+v %v", changed, err)
			}
		})
	}
}
