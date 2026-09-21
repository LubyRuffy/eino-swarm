package store

import (
	"testing"
	"time"
)

// The phone banner and Run now / Cancel wait target one parked row.
// Returning every schedule (or a later due slot) would show the wrong clock.
func TestActiveThreadWakeReturnsTheSoonestArmedRow(t *testing.T) {
	s := openTestStore(t)
	th := &Thread{Title: "t"}
	other := &Thread{Title: "o"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateThread(other); err != nil {
		t.Fatal(err)
	}
	got, err := s.ActiveThreadWake("")
	if err != nil || got != nil {
		t.Fatalf("empty thread: got=%v err=%v", got, err)
	}
	got, err = s.ActiveThreadWake(th.ID)
	if err != nil || got != nil {
		t.Fatalf("no wake: got=%v err=%v", got, err)
	}
	later := time.Now().UTC().Add(2 * time.Hour)
	sooner := time.Now().UTC().Add(time.Hour)
	late := &Schedule{
		Kind: ScheduleThread, ThreadID: th.ID, OriginThreadID: th.ID,
		Title: "later", Prompt: "Continue the wait.", EveryS: 60,
		Status: ScheduleActive, NextRunAt: later, CreatedBy: ScheduleCreatedManager,
	}
	early := &Schedule{
		Kind: ScheduleThread, ThreadID: th.ID, OriginThreadID: th.ID,
		Title: "soon", Prompt: "Continue the wait.", EveryS: 60,
		Status: ScheduleActive, NextRunAt: sooner, CreatedBy: ScheduleCreatedManager,
	}
	paused := &Schedule{
		Kind: ScheduleThread, ThreadID: th.ID, OriginThreadID: th.ID,
		Title: "paused", Prompt: "Continue the wait.", EveryS: 60,
		Status: SchedulePaused, NextRunAt: sooner.Add(-time.Minute), CreatedBy: ScheduleCreatedManager,
	}
	alien := &Schedule{
		Kind: ScheduleThread, ThreadID: other.ID, OriginThreadID: other.ID,
		Title: "other", Prompt: "Continue the wait.", EveryS: 60,
		Status: ScheduleActive, NextRunAt: sooner.Add(-time.Minute), CreatedBy: ScheduleCreatedManager,
	}
	for _, row := range []*Schedule{late, early, paused, alien} {
		if err := s.CreateSchedule(row); err != nil {
			t.Fatal(err)
		}
	}
	got, err = s.ActiveThreadWake(th.ID)
	if err != nil || got == nil {
		t.Fatalf("armed: got=%v err=%v", got, err)
	}
	if got.ID != early.ID || got.Title != "soon" {
		t.Fatalf("want soonest armed row, got %+v", got)
	}
	_ = s.Close()
	if _, err := s.ActiveThreadWake(th.ID); err == nil {
		t.Fatal("closed store must fail")
	}
}
