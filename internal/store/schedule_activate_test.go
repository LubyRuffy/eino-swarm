package store

import (
	"errors"
	"testing"
	"time"
)

func TestActivateDoneUnderCapRearmsAFinishedRow(t *testing.T) {
	s := openTestStore(t)
	row := activeSchedule()
	row.Status = ScheduleDone
	if err := s.CreateSchedule(row); err != nil {
		t.Fatal(err)
	}
	next := time.Now().UTC().Add(time.Minute)
	if err := s.ActivateDoneUnderCap(row.ID, 1, map[string]any{
		"every_s": 60, "delay_s": 0, "cron": "", "next_run_at": next,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSchedule(row.ID)
	if err != nil || got.Status != ScheduleActive || got.EveryS != 60 || got.DelayS != 0 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestActivateDoneUnderCapRejectsWhenFull(t *testing.T) {
	s := openTestStore(t)
	if err := s.CreateScheduleUnderCap(activeSchedule(), 1); err != nil {
		t.Fatal(err)
	}
	done := activeSchedule()
	done.Status = ScheduleDone
	if err := s.CreateSchedule(done); err != nil {
		t.Fatal(err)
	}
	if err := s.ActivateDoneUnderCap(done.ID, 1, map[string]any{"every_s": 60}); !errors.Is(err, ErrScheduleCap) {
		t.Fatalf("err=%v", err)
	}
	got, err := s.GetSchedule(done.ID)
	if err != nil || got.Status != ScheduleDone {
		t.Fatalf("status=%v err=%v", got, err)
	}
}

func TestActivateDoneUnderCapRejectsCancelled(t *testing.T) {
	s := openTestStore(t)
	row := activeSchedule()
	if err := s.CreateSchedule(row); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelSchedule(row.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.ActivateDoneUnderCap(row.ID, 1, nil); err == nil {
		t.Fatal("cancelled is not done")
	}
}

func TestActivateDoneUnderCapUnknownIsNotFound(t *testing.T) {
	s := openTestStore(t)
	if err := s.ActivateDoneUnderCap("sch_missing", 1, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestActivateDoneUnderCapFailsWithoutTheTable(t *testing.T) {
	s := openTestStore(t)
	if err := s.DB().Migrator().DropTable(&Schedule{}); err != nil {
		t.Fatal(err)
	}
	if err := s.ActivateDoneUnderCap("sch_x", 1, nil); err == nil {
		t.Fatal("ActivateDoneUnderCap must fail without the table")
	}
}

func TestActivateDoneUnderCapRejectsABadPatch(t *testing.T) {
	s := openTestStore(t)
	row := activeSchedule()
	row.Status = ScheduleDone
	if err := s.CreateSchedule(row); err != nil {
		t.Fatal(err)
	}
	if err := s.ActivateDoneUnderCap(row.ID, 1, map[string]any{"no_such_column": 1}); err == nil {
		t.Fatal("unknown column must fail the patch")
	}
}
