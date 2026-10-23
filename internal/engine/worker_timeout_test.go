package engine

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestWorkerTimeoutLimitPausesRunNowAndAutomaticSchedules(t *testing.T) {
	for _, now := range []bool{false, true} {
		t.Run(map[bool]string{false: "automatic", true: "run-now"}[now], func(t *testing.T) {
			e := newTestEngine(t)
			clk := newScheduleClock()
			e.now = clk.Now
			th, err := e.CreateThread("", "", "")
			if err != nil {
				t.Fatal(err)
			}
			sch := armDueWake(t, e, clk, th.ID, 60)
			provider.SetMockFailure(&swarm.WorkerTimeoutLimitError{AgentID: "worker-1", Failures: 3})
			t.Cleanup(func() { provider.SetMockFailure(nil) })
			var turn *store.Turn
			if now {
				turn, err = e.RunScheduleNow(sch.ID)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				e.fireDueSchedules()
				turns, err := e.Store().ListTurns(th.ID)
				if err != nil || len(turns) != 1 {
					t.Fatalf("turns=%v err=%v", turns, err)
				}
				turn = &turns[0]
			}
			turn = waitForTurn(t, e, turn.ID)
			got, err := e.GetSchedule(sch.ID)
			if err != nil || got.Status != store.SchedulePaused || turn.Status != store.TurnError || !strings.Contains(turn.Error, "paused") {
				t.Fatalf("unbounded recovery: schedule=%+v turn=%+v err=%v", got, turn, err)
			}
			run := waitForScheduleRun(t, e, turn.ScheduleRunID)
			if run.Status != store.ScheduleRunError || !strings.Contains(run.Summary, "worker-1") {
				t.Fatalf("missing failure receipt: %+v", run)
			}
			events, err := e.Store().ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			sawError := false
			for _, ev := range events {
				if ev.Kind == swarm.NotifyError.String() && strings.Contains(ev.Err, "worker-1") {
					sawError = true
				}
				if ev.Kind == KindModelRetry {
					t.Fatal("worker recovery limit retried as a provider failure")
				}
			}
			if !sawError {
				t.Fatal("Trace has no terminal worker failure")
			}
			clk.Advance(time.Minute * 2)
			e.fireDueSchedules()
			more, _ := e.Store().ListTurns(th.ID)
			if len(more) != 1 {
				t.Fatal("paused schedule fired again")
			}
		})
	}
}

// Old recorded watchdog errors must retain their budget on a process restart.
func TestOrphanedWorkersRetainsWatchdogFailuresUntilHumanRetry(t *testing.T) {
	var events []store.Event
	for i := 0; i < 3; i++ {
		events = append(events, store.Event{Kind: swarm.NotifySpawned.String(), AgentID: "worker-1", Role: "worker"}, store.Event{Kind: swarm.NotifyFinished.String(), AgentID: "worker-1", Err: "swarm: agent worker-1 exceeded timeout 30m0s: context deadline exceeded"})
	}
	_, finished := orphanedWorkers(events)
	if len(finished) != 1 || finished[0].TimeoutFailures != 3 {
		t.Fatalf("lost watchdog history: %+v", finished)
	}
	reg := swarm.NewRegistry()
	defer reg.Close()
	if err := reg.PlantFinished(finished[0]); err != nil {
		t.Fatal(err)
	}
	_, err := reg.Resume(t.Context(), "worker-1", "continue", nil)
	var exhausted *swarm.WorkerTimeoutLimitError
	if !errors.As(err, &exhausted) {
		t.Fatalf("restart bypassed timeout guard: %v", err)
	}
	events = append(events, store.Event{Kind: KindUser, AgentID: swarm.DefaultManagerID})
	_, finished = orphanedWorkers(events)
	if finished[0].TimeoutFailures != 0 {
		t.Fatal("explicit human retry did not reset the worker budget")
	}
}

func TestWorkerTimeoutPauseIsBoundToTheActualRecurringRun(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	makeRun := func(recurring bool) (*store.Schedule, *store.Turn) {
		in := ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID, Title: "Scheduled work", Prompt: scheduleWaitPrompt, CreatedBy: store.ScheduleCreatedHuman, DelayS: 60}
		if recurring {
			in.DelayS = 0
			in.EveryS = 60
		}
		sch, err := e.CreateSchedule(in)
		if err != nil {
			t.Fatal(err)
		}
		run := &store.ScheduleRun{ScheduleID: sch.ID, ThreadID: th.ID, Status: store.ScheduleRunRunning}
		if err := e.Store().CreateRun(run); err != nil {
			t.Fatal(err)
		}
		return sch, &store.Turn{ThreadID: th.ID, ScheduleContinue: true, ScheduleRunID: run.ID}
	}
	failing, turn := makeRun(true)
	healthy, other := makeRun(true)
	_, oneShot := makeRun(false)
	exhausted := &swarm.WorkerTimeoutLimitError{AgentID: "worker-1", Failures: 3}
	for _, bad := range []*store.Turn{nil, {}, {ScheduleContinue: true, ScheduleRunID: "missing"}, oneShot} {
		if e.pauseScheduledWorkerTimeout(bad, exhausted) {
			t.Fatalf("unrelated pause: %+v", bad)
		}
	}
	if e.pauseScheduledWorkerTimeout(other, errors.New("provider i/o timeout")) {
		t.Fatal("ordinary model error paused a schedule")
	}
	if !e.pauseScheduledWorkerTimeout(turn, fmt.Errorf("wrapped: %w", exhausted)) || e.pauseScheduledWorkerTimeout(turn, exhausted) {
		t.Fatal("expected one idempotent actual pause")
	}
	a, _ := e.GetSchedule(failing.ID)
	b, _ := e.GetSchedule(healthy.ID)
	if a.Status != store.SchedulePaused || b.Status != store.ScheduleActive {
		t.Fatal("wrong plan paused")
	}
	db, _ := e.Store().DB().DB()
	db.SetMaxOpenConns(1)
	if err := e.Store().DB().Exec("PRAGMA query_only = ON").Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Store().DB().Exec("PRAGMA query_only = OFF").Error })
	if e.pauseScheduledWorkerTimeout(other, exhausted) {
		t.Fatal("pause claimed success after storage failed")
	}
}

func TestWatchdogRestoreDistinguishesHumanAndModelRecovery(t *testing.T) {
	base := []store.Event{{Kind: swarm.NotifySpawned.String(), AgentID: "worker-1", Role: "worker"}, {Kind: swarm.NotifyFinished.String(), AgentID: "worker-1", Err: "swarm: agent worker-1 exceeded timeout 30m0s: deadline"}}
	for _, ev := range []store.Event{{Kind: KindGoalResumed, AgentID: swarm.DefaultManagerID}, {Kind: swarm.NotifyAgentMessage.String(), AgentID: "worker-1", Text: "timeout"}, {Kind: KindScheduleFired, AgentID: swarm.DefaultManagerID}} {
		_, done := orphanedWorkers(append(append([]store.Event{}, base...), ev))
		if done[0].TimeoutFailures != 1 {
			t.Fatalf("model/automatic event reset guard: %+v", ev)
		}
	}
	_, done := orphanedWorkers(append(base, store.Event{Kind: KindGoalResumed, AgentID: swarm.DefaultManagerID, Role: "human"}))
	if done[0].TimeoutFailures != 0 {
		t.Fatal("explicit human goal resume did not reset guard")
	}
	for _, errText := range []string{"", "context canceled", "context deadline exceeded", "i/o timeout", "swarm: agent other-2 exceeded timeout 1m0s: deadline"} {
		events := append(append([]store.Event{}, base...), store.Event{Kind: swarm.NotifySpawned.String(), AgentID: "worker-1", Role: "worker"}, store.Event{Kind: swarm.NotifyFinished.String(), AgentID: "worker-1", Err: errText})
		_, done := orphanedWorkers(events)
		if done[0].TimeoutFailures != 0 {
			t.Fatalf("non-watchdog result kept budget: %+v", done)
		}
	}
	running, _ := orphanedWorkers(append(append([]store.Event{}, base...), store.Event{Kind: swarm.NotifySpawned.String(), AgentID: "worker-1", Role: "worker"}))
	if len(running) != 1 || running[0].TimeoutFailures != 1 {
		t.Fatal("crashed resumed run lost the earlier budget")
	}
	_, done = orphanedWorkers(append(base, store.Event{Kind: swarm.NotifySpawned.String(), AgentID: "worker-1", Role: "worker"}, store.Event{Kind: KindCleanup}))
	if done[0].TimeoutFailures != 0 {
		t.Fatal("cleanup cancellation counted as a watchdog")
	}
	rt := &runtime{restore: running, planted: []swarm.FinishedWorker{{ID: "done-2", TimeoutFailures: 3}}}
	rt.resetRestoredWorkerTimeouts()
	if rt.restore[0].TimeoutFailures != 0 || rt.planted[0].TimeoutFailures != 0 {
		t.Fatal("human retry kept a stale restored budget")
	}
}

func TestCleanupAfterRefusedLegacyRestoreKeepsExhaustedWorkerBudget(t *testing.T) {
	var events []store.Event
	for i := 0; i < 3; i++ {
		events = append(events, store.Event{Kind: swarm.NotifySpawned.String(), AgentID: "worker-1", Role: "worker"}, store.Event{Kind: swarm.NotifyFinished.String(), AgentID: "worker-1", Err: "swarm: agent worker-1 exceeded timeout 30m0s: deadline"})
	}
	// A pre-upgrade process may have started a fourth run. The new process
	// refuses its Restore, then performs ordinary turn cleanup without a worker.
	events = append(events, store.Event{Kind: swarm.NotifySpawned.String(), AgentID: "worker-1", Role: "worker"}, store.Event{Kind: KindCleanup})
	running, finished := orphanedWorkers(events)
	if len(running) != 0 || len(finished) != 1 || finished[0].TimeoutFailures != 3 {
		t.Fatalf("cleanup bypassed the exhausted budget: running=%v finished=%v", running, finished)
	}
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	if err := reg.PlantFinished(finished[0]); err != nil {
		t.Fatal(err)
	}
	_, err := reg.Resume(t.Context(), "worker-1", "continue", nil)
	var exhausted *swarm.WorkerTimeoutLimitError
	if !errors.As(err, &exhausted) {
		t.Fatalf("cleanup allowed another run: %v", err)
	}
}

func TestWorkerTimeoutFailureBlocksTheGoalInsteadOfRetryingTheModel(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "perform the assigned work"); err != nil {
		t.Fatal(err)
	}
	provider.SetMockFailure(&swarm.WorkerTimeoutLimitError{AgentID: "worker-1", Failures: 3})
	t.Cleanup(func() { provider.SetMockFailure(nil) })
	turn, err := e.StartTurnInput(th.ID, UserInput{Text: GoalContinueText(), ContinueGoal: true})
	if err != nil {
		t.Fatal(err)
	}
	turn = waitForTurn(t, e, turn.ID)
	// Goal blocking follows FinishTurn; wait for the separately persisted state.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		got, _ := e.Store().GetThread(th.ID)
		if got.GoalBlocked {
			if !strings.Contains(got.GoalBlockReason, "worker-1") {
				t.Fatal("block has no worker reason")
			}
			return
		}
		time.Sleep(time.Millisecond * 5)
	}
	t.Fatalf("goal did not block after %s", turn.Error)
}

func TestRealWorkerWatchdogPausesTheScheduledLoopWithTraceEvidence(t *testing.T) {
	t.Setenv("ZWAI_MOCK_WORKER_TIMEOUT", "1")
	e := newTestEngine(t)
	e.cfg.Swarm.AgentTimeoutSeconds = 1
	th, _ := e.CreateThread("", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID, Prompt: scheduleWaitPrompt, EveryS: 60, CreatedBy: store.ScheduleCreatedHuman})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.RunScheduleNow(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn = waitForTurn(t, e, turn.ID)
	if turn.Status != store.TurnError || !strings.Contains(turn.Error, "3 consecutive watchdog timeouts") || !strings.Contains(turn.Error, "paused") {
		t.Fatalf("manager kept looping: %+v", turn)
	}
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	starts, finishes := 0, 0
	for _, ev := range events {
		if ev.Kind == swarm.NotifySpawned.String() {
			starts++
			if ev.AgentID != "timeout-worker-1" {
				t.Fatal("resume created a twin")
			}
		}
		if ev.Kind == swarm.NotifyFinished.String() {
			finishes++
			if !strings.HasPrefix(ev.Err, "swarm: agent timeout-worker-1 exceeded timeout ") {
				t.Fatal("failed receipt lost watchdog cause")
			}
		}
		if ev.Kind == KindModelRetry {
			t.Fatal("exhaustion was retried as a model call")
		}
	}
	if starts != 3 || finishes != 3 {
		t.Fatalf("starts=%d finishes=%d", starts, finishes)
	}
	run := waitForScheduleRun(t, e, turn.ScheduleRunID)
	if run.Status != store.ScheduleRunError {
		t.Fatalf("run=%+v", run)
	}
}
