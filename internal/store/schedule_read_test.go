package store

import (
	"errors"
	"testing"
	"time"
)

func TestMarkRunReadClearsUnread(t *testing.T) {
	s := openTestStore(t)
	sch := activeSchedule()
	if err := s.CreateSchedule(sch); err != nil {
		t.Fatal(err)
	}
	run := &ScheduleRun{
		ScheduleID: sch.ID, Status: ScheduleRunFindings,
		Summary: "something changed", Unread: true,
	}
	if err := s.CreateRun(run); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkRunRead(run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Unread {
		t.Fatalf("unread still set: %+v", got)
	}
	if got.Status != ScheduleRunFindings || got.Summary != "something changed" {
		t.Fatalf("mark-read must not rewrite the outcome: %+v", got)
	}
	// A second click on the same row is not a 404.
	if err := s.MarkRunRead(run.ID); err != nil {
		t.Fatalf("already-read: %v", err)
	}
}

func TestMarkRunReadUnknownIsNotFound(t *testing.T) {
	s := openTestStore(t)
	if err := s.MarkRunRead("srun_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestCountUnreadRunsIgnoresQuiet(t *testing.T) {
	s := openTestStore(t)
	sch := activeSchedule()
	if err := s.CreateSchedule(sch); err != nil {
		t.Fatal(err)
	}
	n, err := s.CountUnreadRuns()
	if err != nil || n != 0 {
		t.Fatalf("empty unread=%d err=%v", n, err)
	}
	findings := &ScheduleRun{ScheduleID: sch.ID, Status: ScheduleRunFindings, Unread: true}
	if err := s.CreateRun(findings); err != nil {
		t.Fatal(err)
	}
	quiet := &ScheduleRun{ScheduleID: sch.ID, Status: ScheduleRunQuiet, Unread: false}
	if err := s.CreateRun(quiet); err != nil {
		t.Fatal(err)
	}
	failed := &ScheduleRun{ScheduleID: sch.ID, Status: ScheduleRunError, Unread: true}
	if err := s.CreateRun(failed); err != nil {
		t.Fatal(err)
	}
	n, err = s.CountUnreadRuns()
	if err != nil || n != 2 {
		t.Fatalf("unread=%d err=%v, quiet must not count", n, err)
	}
	if err := s.MarkRunRead(findings.ID); err != nil {
		t.Fatal(err)
	}
	n, err = s.CountUnreadRuns()
	if err != nil || n != 1 {
		t.Fatalf("after read unread=%d err=%v", n, err)
	}
}

func TestMarkRunReadFailsWhenTheTableIsGone(t *testing.T) {
	s := openTestStore(t)
	if err := s.DB().Migrator().DropTable(&ScheduleRun{}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkRunRead("srun_x"); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("MarkRunRead err=%v, want a storage error", err)
	}
	if _, err := s.CountUnreadRuns(); err == nil {
		t.Fatal("CountUnreadRuns must fail without the table")
	}
}

func TestMarkRunReadWritesFalseThroughGorm(t *testing.T) {
	// Struct Updates drop bool false. The inbox uses a map so a findings
	// run actually clears. If this ever flips back, unread sticks forever.
	s := openTestStore(t)
	sch := activeSchedule()
	if err := s.CreateSchedule(sch); err != nil {
		t.Fatal(err)
	}
	run := &ScheduleRun{ScheduleID: sch.ID, Status: ScheduleRunFindings, Unread: true}
	if err := s.CreateRun(run); err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC().Add(-time.Second)
	if err := s.MarkRunRead(run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Unread {
		t.Fatal("gorm dropped unread=false")
	}
	if !got.UpdatedAt.After(before) {
		t.Fatalf("updated_at=%s", got.UpdatedAt)
	}
}
