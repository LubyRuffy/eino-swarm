package engine

import (
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestContextOverflowPausesARepeatingSchedule(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 300,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	run := &store.ScheduleRun{ScheduleID: sch.ID, ThreadID: th.ID, Status: store.ScheduleRunRunning}
	if err := e.Store().CreateRun(run); err != nil {
		t.Fatal(err)
	}
	turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true, ScheduleRunID: run.ID})
	got := e.noteContextOverflow(turn, publicContextOverflow)
	if got != publicContextOverflowPaused {
		t.Fatalf("transcript line = %q", got)
	}
	row, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != store.SchedulePaused {
		t.Fatalf("status=%q, a repeating check must stop after the window rejection", row.Status)
	}
}

func TestContextOverflowLeavesAOneShotSchedule(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, DelayS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	run := &store.ScheduleRun{ScheduleID: sch.ID, ThreadID: th.ID, Status: store.ScheduleRunRunning}
	if err := e.Store().CreateRun(run); err != nil {
		t.Fatal(err)
	}
	turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true, ScheduleRunID: run.ID})
	got := e.noteContextOverflow(turn, publicContextOverflow)
	if got != publicContextOverflow {
		t.Fatalf("a one-shot must keep the plain line, got %q", got)
	}
	row, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != store.ScheduleActive {
		t.Fatalf("status=%q, a one-shot has no next tick to pause", row.Status)
	}
}

func TestContextOverflowNoticeIgnoresOtherErrors(t *testing.T) {
	e := newTestEngine(t)
	if got := e.noteContextOverflow(nil, publicContextOverflow); got != publicContextOverflow {
		t.Fatalf("got %q", got)
	}
	if got := e.noteContextOverflow(&store.Turn{}, "the endpoint refused the connection"); got != "the endpoint refused the connection" {
		t.Fatalf("got %q", got)
	}
}
