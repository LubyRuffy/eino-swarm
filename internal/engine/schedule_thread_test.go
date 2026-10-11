package engine

import (
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestListSchedulesForThreadReadsTheStore(t *testing.T) {
	e := newTestEngine(t)
	th := &store.Thread{Title: "t"}
	if err := e.store.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	row := &store.Schedule{
		Kind:           store.ScheduleThread,
		ThreadID:       th.ID,
		OriginThreadID: th.ID,
		Prompt:         "Continue the wait.",
		EveryS:         60,
		NextRunAt:      time.Now().Add(time.Hour),
	}
	if err := e.store.CreateSchedule(row); err != nil {
		t.Fatal(err)
	}
	got, err := e.ListSchedulesForThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Schedules) != 1 || got.Schedules[0].ID != row.ID {
		t.Fatalf("schedules=%+v", got.Schedules)
	}
	if len(got.CreatedBy) != 0 {
		t.Fatalf("created=%v", got.CreatedBy)
	}
}
