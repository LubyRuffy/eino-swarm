package engine

import (
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestPendingWakeSuppressesGoalAutoContinue(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 8
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedManager,
	}); err != nil {
		t.Fatal(err)
	}
	first, err := e.StartTurn(th.ID, "start the work")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	waitSettled(t, e, th.ID)

	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("a pending wake must pause goal auto-continue, got %d turns", len(turns))
	}
	if turns[0].GoalContinue {
		t.Fatalf("the pursuing turn must stay human-originated: %+v", turns[0])
	}
	if hasKind(t, e, th.ID, KindGoalContinued) {
		t.Fatal("a pending wake must not record goal_continued")
	}
	got, err := e.Store().GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.GoalAutoTurns != 0 {
		t.Fatalf("a pending wake must not spend the auto-continue budget, got %d", got.GoalAutoTurns)
	}
}

func TestCancelWakeRestoresGoalAutoContinue(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 1
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedManager,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.StartTurn(th.ID, "start the work")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	waitSettled(t, e, th.ID)
	if hasKind(t, e, th.ID, KindGoalContinued) {
		t.Fatal("the armed wake must still be suppressing auto-continue")
	}

	if err := e.CancelSchedule(sch.ID); err != nil {
		t.Fatal(err)
	}
	waitKind(t, e, th.ID, KindGoalContinued)
	waitSettled(t, e, th.ID)
	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) < 2 || !turns[1].GoalContinue {
		t.Fatalf("cancelling an idle wake must start the next pursuing turn: %+v", turns)
	}
}

func TestCancelWakeWithoutStandingObjectiveStaysIdle(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedManager,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.CancelSchedule(sch.ID); err != nil {
		t.Fatal(err)
	}
	waitSettled(t, e, th.ID)
	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 0 {
		t.Fatalf("cancel without a standing objective must not start a turn, got %d", len(turns))
	}
	if hasKind(t, e, th.ID, KindGoalContinued) {
		t.Fatal("cancel without a standing objective must not record goal_continued")
	}
}

func TestContinueGoalAfterWakeCancelNoopsWhenNotAnIdleThreadWake(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	e.continueGoalAfterWakeCancel(nil)
	e.continueGoalAfterWakeCancel(&store.Schedule{
		Kind: store.ScheduleStandalone, ThreadID: th.ID,
	})
	e.continueGoalAfterWakeCancel(&store.Schedule{Kind: store.ScheduleThread})
	if hasKind(t, e, th.ID, KindGoalContinued) {
		t.Fatal("a non-thread or empty-target cancel must not start a pursuing turn")
	}

	rt := e.runtimeFor(th.ID)
	rt.mu.Lock()
	rt.running = true
	rt.mu.Unlock()
	e.continueGoalAfterWakeCancel(&store.Schedule{
		Kind: store.ScheduleThread, ThreadID: th.ID,
	})
	rt.mu.Lock()
	rt.running = false
	rt.mu.Unlock()
	if hasKind(t, e, th.ID, KindGoalContinued) {
		t.Fatal("cancel must not steal a live turn")
	}
}

func TestClaimedOneShotStillSuppressesGoalAutoContinue(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })

	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 8
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, DelayS: 30,
		CreatedBy: store.ScheduleCreatedManager,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Claim writes the running run and marks a delay done *before*
	// StartTurn. Looking only at status=active lets continueGoal steal
	// that slot; the fire then hits ErrBusy and does not resurrect.
	if err := e.Store().CreateRun(&store.ScheduleRun{
		ScheduleID: sch.ID, ThreadID: th.ID, Status: store.ScheduleRunRunning,
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(sch.ID, map[string]any{
		"status": store.ScheduleDone,
	}); err != nil {
		t.Fatal(err)
	}

	first, err := e.StartTurn(th.ID, "start the work")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, first.ID)
	waitSettled(t, e, th.ID)

	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("a claimed one-shot must pause goal auto-continue, got %d turns", len(turns))
	}
	if turns[0].GoalContinue {
		t.Fatalf("the pursuing turn must stay human-originated: %+v", turns[0])
	}
	if hasKind(t, e, th.ID, KindGoalContinued) {
		t.Fatal("a claimed one-shot must not record goal_continued")
	}
	got, err := e.Store().GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.GoalAutoTurns != 0 {
		t.Fatalf("a claimed one-shot must not spend the auto-continue budget, got %d", got.GoalAutoTurns)
	}
}

func TestHasFutureWakeSeesDueAndIgnoresNonTargetRows(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	other, _ := e.CreateThread("", "", "")

	if e.hasFutureWake(th.ID) {
		t.Fatal("no row must not suppress")
	}

	future, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedManager,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !e.hasFutureWake(th.ID) {
		t.Fatal("a future active thread wake must suppress")
	}
	if e.hasFutureWake(other.ID) {
		t.Fatal("a wake on another conversation must not leak")
	}

	due, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: other.ID,
		Prompt: scheduleWaitPrompt, DelayS: 30,
		CreatedBy: store.ScheduleCreatedManager,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(due.ID, map[string]any{
		"next_run_at": time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if !e.hasFutureWake(other.ID) {
		t.Fatal("a one-shot still due must suppress even when next_run_at is past")
	}

	if err := e.Store().UpdateSchedule(future.ID, map[string]any{
		"status": store.SchedulePaused,
	}); err != nil {
		t.Fatal(err)
	}
	if e.hasFutureWake(th.ID) {
		t.Fatal("a paused wake must not suppress")
	}

	if err := e.CancelSchedule(due.ID); err != nil {
		t.Fatal(err)
	}
	if e.hasFutureWake(other.ID) {
		t.Fatal("a cancelled wake must not suppress")
	}

	claimed, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, DelayS: 30,
		CreatedBy: store.ScheduleCreatedManager,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateSchedule(claimed.ID, map[string]any{
		"status": store.ScheduleDone,
	}); err != nil {
		t.Fatal(err)
	}
	if e.hasFutureWake(th.ID) {
		t.Fatal("a done one-shot without a running run must not suppress")
	}
	run := &store.ScheduleRun{
		ScheduleID: claimed.ID, ThreadID: th.ID, Status: store.ScheduleRunRunning,
	}
	if err := e.Store().CreateRun(run); err != nil {
		t.Fatal(err)
	}
	if !e.hasFutureWake(th.ID) {
		t.Fatal("a done one-shot with a running run must still suppress")
	}
	if err := e.Store().FinishRun(run.ID, store.ScheduleRunQuiet, "", false); err != nil {
		t.Fatal(err)
	}
	if e.hasFutureWake(th.ID) {
		t.Fatal("a finished claimed fire must not suppress")
	}

	originOnly, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, OriginThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if originOnly.ThreadID != "" {
		t.Fatalf("standalone must not target the origin: %+v", originOnly)
	}
	if e.hasFutureWake(th.ID) {
		t.Fatal("a standalone origin-only row must not suppress")
	}

	if e.hasFutureWake("") {
		t.Fatal("empty thread id is not a wake")
	}

	if err := e.Store().DB().Migrator().DropTable(&store.Schedule{}); err != nil {
		t.Fatal(err)
	}
	if e.hasFutureWake(th.ID) {
		t.Fatal("a list error must not invent a wake")
	}
}
