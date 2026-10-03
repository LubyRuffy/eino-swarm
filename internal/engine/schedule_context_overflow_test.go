package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestContextOverflowPausesOnlyTheFailingRecurringSchedule(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	makeSchedule := func() (*store.Schedule, *store.Turn) {
		t.Helper()
		sch, err := e.CreateSchedule(ScheduleInput{
			Kind: store.ScheduleThread, ThreadID: th.ID,
			Prompt: scheduleWaitPrompt, EveryS: 60,
			CreatedBy: store.ScheduleCreatedHuman,
		})
		if err != nil {
			t.Fatal(err)
		}
		run := &store.ScheduleRun{ScheduleID: sch.ID, ThreadID: th.ID, Status: store.ScheduleRunRunning}
		if err := e.Store().CreateRun(run); err != nil {
			t.Fatal(err)
		}
		return sch, &store.Turn{ThreadID: th.ID, ScheduleContinue: true, ScheduleRunID: run.ID}
	}
	failing, turn := makeSchedule()
	healthy, otherTurn := makeSchedule()
	overflow := errors.New("maximum context length is 262144 tokens")
	if e.pauseScheduledContextOverflow(nil, overflow) ||
		e.pauseScheduledContextOverflow(&store.Turn{}, overflow) ||
		e.pauseScheduledContextOverflow(&store.Turn{ScheduleContinue: true, ScheduleRunID: "missing"}, overflow) {
		t.Fatal("unrelated or missing scheduled runs must not be paused")
	}
	if e.pauseScheduledContextOverflow(otherTurn, errors.New("rate limited")) {
		t.Fatal("unrelated errors must not pause recurring checks")
	}
	if !e.pauseScheduledContextOverflow(turn, overflow) {
		t.Fatal("a terminal context overflow must pause the failing schedule")
	}
	if e.pauseScheduledContextOverflow(turn, overflow) {
		t.Fatal("the already paused schedule must not be paused again")
	}
	got, err := e.GetSchedule(failing.ID)
	if err != nil || got.Status != store.SchedulePaused {
		t.Fatalf("failing schedule = %+v, %v", got, err)
	}
	got, err = e.GetSchedule(healthy.ID)
	if err != nil || got.Status != store.ScheduleActive {
		t.Fatalf("unrelated schedule = %+v, %v", got, err)
	}
}

func TestContextOverflowLeavesOneShotScheduleCompleted(t *testing.T) {
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
	turn := &store.Turn{ThreadID: th.ID, ScheduleContinue: true, ScheduleRunID: run.ID}
	if e.pauseScheduledContextOverflow(turn, errors.New("maximum context length is 128000 tokens")) {
		t.Fatal("a one-shot wake has no repeat fire to pause")
	}
}

func TestContextOverflowDoesNotClaimPauseWhenStorageRejectsIt(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	run := &store.ScheduleRun{ScheduleID: sch.ID, ThreadID: th.ID, Status: store.ScheduleRunRunning}
	if err := e.Store().CreateRun(run); err != nil {
		t.Fatal(err)
	}
	db, err := e.Store().DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err := e.Store().DB().Exec("PRAGMA query_only = ON").Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Store().DB().Exec("PRAGMA query_only = OFF").Error })
	turn := &store.Turn{ThreadID: th.ID, ScheduleContinue: true, ScheduleRunID: run.ID}
	if e.pauseScheduledContextOverflow(turn, errors.New("maximum context length is 128000 tokens")) {
		t.Fatal("the user must not be told the schedule paused when storage rejected it")
	}
}

func TestScheduledContextOverflowRecordsAdviceAndStopsRepeatFires(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sch := armDueWake(t, e, clk, th.ID, 60)
	provider.SetMockFailure(errors.New("maximum context length is 128000 tokens; requested 64000 output tokens"))
	t.Cleanup(func() { provider.SetMockFailure(nil) })
	e.fireDueSchedules()
	turns, err := e.Store().ListTurns(th.ID)
	if err != nil || len(turns) != 1 {
		t.Fatalf("turns = %+v, %v", turns, err)
	}
	turn := waitForTurn(t, e, turns[0].ID)
	if turn.Status != store.TurnError || !strings.Contains(turn.Error, "Settings") || !strings.Contains(turn.Error, "paused") {
		t.Fatalf("turn has no actionable, paused error: %+v", turn)
	}
	run := waitForScheduleRun(t, e, turns[0].ScheduleRunID)
	if run.Status != store.ScheduleRunError || !strings.Contains(run.Summary, "paused") {
		t.Fatalf("failed run = %+v", run)
	}
	got, err := e.GetSchedule(sch.ID)
	if err != nil || got.Status != store.SchedulePaused {
		t.Fatalf("schedule = %+v, %v", got, err)
	}
	e.fireDueSchedules()
	more, err := e.Store().ListTurns(th.ID)
	if err != nil || len(more) != 1 {
		t.Fatalf("paused schedule fired again: %+v, %v", more, err)
	}
}
