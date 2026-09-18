package engine

import (
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestDelaySkipDoesNotResurrectACancelledRow(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now
	th, _ := e.CreateThread("", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, DelayS: 90,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.CancelSchedule(sch.ID); err != nil {
		t.Fatal(err)
	}
	spec, err := parseScheduleSpec(90, 0, "", e.cfg.Swarm.ScheduleMinInterval())
	if err != nil {
		t.Fatal(err)
	}
	row, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.skipBusy(*row, spec, clk.Now(), th.ID)
	got, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.ScheduleCancelled {
		t.Fatalf("status=%q, a delay skip must not write status=active over cancelled", got.Status)
	}
}

func TestBusyDelaySkipIsNotRecordedTwice(t *testing.T) {
	provider.SetMockAskUser(true)
	t.Cleanup(func() { provider.SetMockAskUser(false) })

	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now
	th, _ := e.CreateThread("", "", "")
	if _, err := e.StartTurn(th.ID, "work on this"); err != nil {
		t.Fatal(err)
	}
	waitUntilRunning(t, e, th.ID)

	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, DelayS: 90,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{"next_run_at": clk.Now()}); err != nil {
		t.Fatal(err)
	}

	e.fireDueSchedules()
	e.fireDueSchedules()

	if n := countThreadKind(t, e, th.ID, KindScheduleSkipped); n != 1 {
		t.Fatalf("skipped events=%d, the same due window is one chip not one per tick", n)
	}
	runs := scheduleRuns(t, e, sch.ID)
	if len(runs) != 1 || runs[0].Status != store.ScheduleRunSkippedBusy {
		t.Fatalf("runs=%+v, one skipped_busy for the due window", runs)
	}
	_ = e.Interrupt(th.ID)
	waitSettled(t, e, th.ID)
}

func TestFailedStartDoesNotLeaveAStaleDue(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now
	th, _ := e.CreateThread("", "", "")
	sch := armDueWake(t, e, clk, th.ID, 60)
	spec, err := parseScheduleSpec(0, 60, "", e.cfg.Swarm.ScheduleMinInterval())
	if err != nil {
		t.Fatal(err)
	}
	row, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.startScheduledTurn(*row, spec, clk.Now(), "th_gone")

	got, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	wantNext := clk.Now().Add(60 * time.Second)
	if !got.NextRunAt.Equal(wantNext) {
		t.Fatalf("next_run_at=%s want %s; claim must bump before StartTurn so a failed start is not still due",
			got.NextRunAt, wantNext)
	}
	runs := scheduleRuns(t, e, sch.ID)
	if len(runs) != 1 || runs[0].Status != store.ScheduleRunError {
		t.Fatalf("runs=%+v", runs)
	}
}

func TestStandaloneBrokenProviderDoesNotMintOnRetry(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, Title: "job",
		Prompt: scheduleWaitPrompt, EveryS: 60,
		ProviderID: "provider-missing", CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{"next_run_at": clk.Now()}); err != nil {
		t.Fatal(err)
	}
	e.fireDueSchedules()
	e.fireDueSchedules()
	threads, err := e.Store().ListThreads(true, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 0 {
		t.Fatalf("threads=%d, ResolveModel must fail before minting", len(threads))
	}
	got, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.ScheduleActive || got.RunCount != 0 {
		t.Fatalf("leave due without claiming: %+v", got)
	}
}

func TestStartSchedulerFiresOverdueImmediately(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now
	e.cfg.Swarm.ScheduleTickMS = 3_600_000
	th, _ := e.CreateThread("", "", "")
	_ = armDueWake(t, e, clk, th.ID, 60)
	e.StartScheduler()
	t.Cleanup(e.StopScheduler)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		turns, err := e.Store().ListTurns(th.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(turns) == 1 {
			waitForTurn(t, e, turns[0].ID)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("StartScheduler must fire overdue rows immediately, not wait a full tick")
}

func TestCancelledScheduleIsNotClaimedForAFire(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now
	th, _ := e.CreateThread("", "", "")
	sch := armDueWake(t, e, clk, th.ID, 60)
	if err := e.CancelSchedule(sch.ID); err != nil {
		t.Fatal(err)
	}
	spec, err := parseScheduleSpec(0, 60, "", e.cfg.Swarm.ScheduleMinInterval())
	if err != nil {
		t.Fatal(err)
	}
	row, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.startScheduledTurn(*row, spec, clk.Now(), th.ID)
	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 0 {
		t.Fatalf("turns=%d, a cancelled wait must not start", len(turns))
	}
	got, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.ScheduleCancelled {
		t.Fatalf("status=%q", got.Status)
	}
	runs := scheduleRuns(t, e, sch.ID)
	if len(runs) != 1 || runs[0].Status != store.ScheduleRunError {
		t.Fatalf("runs=%+v, a cancelled claim must drop the occupied run", runs)
	}
}

func TestClaimStandaloneUsesFrozenDefaultProviderWhenTickerIsLive(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now
	e.cfg.Swarm.ScheduleTickMS = 3_600_000
	e.StartScheduler()
	t.Cleanup(e.StopScheduler)
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, Title: "job",
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := parseScheduleSpec(0, 60, "", e.scheduleMinInterval())
	if err != nil {
		t.Fatal(err)
	}
	e.cfg.Models.Default = "nope"
	c := e.claimStandalone(*row, spec, clk.Now())
	if c == nil {
		t.Fatal("claim must use the frozen default provider, not a later cfg write")
	}
	if err := e.Store().FinishRun(c.run.ID, store.ScheduleRunError, "test", false); err != nil {
		t.Fatal(err)
	}
	e.ApplyLiveSwarmLimits()
	if got := e.claimStandalone(*row, spec, clk.Now()); got != nil {
		t.Fatal("settings must refresh the frozen default provider")
	}
}

func TestStandaloneMintFailureFinishesTheClaim(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, Title: "job",
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{"next_run_at": clk.Now()}); err != nil {
		t.Fatal(err)
	}
	row, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := parseScheduleSpec(0, 60, "", e.cfg.Swarm.ScheduleMinInterval())
	if err != nil {
		t.Fatal(err)
	}
	c := e.claimStandalone(*row, spec, clk.Now())
	if c == nil {
		t.Fatal("claim must succeed before mint")
	}
	_ = e.Store().Close()
	e.startClaimedFire(*c)
}

func TestStartSchedulerStopLeavesALaterDueUnfired(t *testing.T) {
	e := newTestEngine(t)
	clk := newScheduleClock()
	e.now = clk.Now
	e.cfg.Swarm.ScheduleTickMS = 3_600_000
	e.StartScheduler()
	e.StopScheduler()

	th, _ := e.CreateThread("", "", "")
	_ = armDueWake(t, e, clk, th.ID, 60)
	time.Sleep(30 * time.Millisecond)
	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 0 {
		t.Fatalf("turns=%d after StopScheduler; the ticker must be dead", len(turns))
	}
}
