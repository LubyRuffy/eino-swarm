package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A manager check is shorter lived than its worker's model request. Keep the
// worker HTTP stream open so these tests detect cancellation, not just a
// registry pointer that survived after its worker had already been stopped.
type scheduleOwnershipFixture struct {
	mode, steps, workers atomic.Int32
	policyCalls          atomic.Int32
	policyUnavailable    atomic.Bool
	entered, cancelled   chan struct{}
	stop, finish         chan struct{}
	enterOnce, stopOnce  sync.Once
	mu                   sync.Mutex
	managerInputs        []string
}

func newScheduleOwnershipEngine(t *testing.T) (*Engine, *scheduleOwnershipFixture) {
	t.Helper()
	f := &scheduleOwnershipFixture{entered: make(chan struct{}), cancelled: make(chan struct{}, 8), stop: make(chan struct{}), finish: make(chan struct{})}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Stream   bool            `json:"stream"`
			Messages json.RawMessage `json:"messages"`
			Tools    []struct {
				Function struct{ Name string } `json:"function"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode offline model request: %v", err)
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		manager := false
		for _, tool := range req.Tools {
			manager = manager || tool.Function.Name == "spawn_agent"
		}
		var messages []struct{ Role, Content string }
		if err := json.Unmarshal(req.Messages, &messages); err != nil {
			t.Errorf("decode offline messages: %v", err)
			return
		}
		if len(messages) > 0 && messages[0].Content == scheduleAnswerPrompt {
			f.policyCalls.Add(1)
			if f.policyUnavailable.Load() {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":{"message":"The offline classification service is unavailable.","type":"service_unavailable"}}`))
				return
			}
			// The failure modes below belong to the scheduled check. A broken
			// classifier response would instead keep its answer awaiting service
			// recovery, and cannot prove terminal worker-ownership behavior.
			replyNoScheduleClaimsFixture(t, w, messages[0].Content, req.Stream, len(req.Tools))
			return
		}
		worker := len(messages) > 0 && messages[0].Role == "system" && strings.HasSuffix(messages[0].Content, "inspect the assigned work")
		if worker && req.Stream {
			f.workers.Add(1)
			f.enterOnce.Do(func() { close(f.entered) })
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				f.cancelled <- struct{}{}
			case <-f.stop:
			case <-f.finish:
				writeContextFlowReply(w, true, map[string]any{"content": "The assigned inspection finished."}, "stop")
			}
			return
		}
		if !manager {
			writeContextFlowReply(w, req.Stream, map[string]any{"content": "The inspection state is unchanged."}, "stop")
			return
		}
		f.mu.Lock()
		f.managerInputs = append(f.managerInputs, string(req.Messages))
		f.mu.Unlock()
		step, mode := f.steps.Add(1), f.mode.Load()
		if mode == 3 || mode == 4 {
			status, message := http.StatusBadRequest, "The offline manager request was refused."
			if mode == 4 {
				status, message = http.StatusTooManyRequests, "The offline manager request was rate limited."
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": message, "type": "invalid_request_error", "code": "invalid_request"}})
			return
		}
		name, args := "", ""
		if mode == 0 && step == 1 {
			name, args = "spawn_agent", `{"role":"verifier","task":"inspect the assigned work"}`
		} else if (mode == 0 && step == 2) || (mode == 2 && step == 1) {
			name, args = ToolReportSchedule, `{"findings":"The check was performed."}`
		}
		message, finish := map[string]any{"content": ""}, "stop"
		if name != "" {
			message = map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "ownership-call-" + name, "type": "function", "function": map[string]any{"name": name, "arguments": args}}}}
			finish = "tool_calls"
		}
		writeContextFlowReply(w, req.Stream, message, finish)
	}))
	t.Cleanup(endpoint.Close)
	t.Cleanup(func() { f.stopOnce.Do(func() { close(f.stop) }) })
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "offline-ownership-fixture"
	e.pool = provider.New(e.cfg)
	return e, f
}

func (f *scheduleOwnershipFixture) setMode(mode int32) {
	f.steps.Store(0)
	f.mode.Store(mode)
}

func startOwnedScheduleWorker(t *testing.T, e *Engine, f *scheduleOwnershipFixture) (*store.Thread, *store.Schedule, *swarm.Registry, string) {
	t.Helper()
	th, err := e.CreateThread("worker ownership", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sch, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID, Prompt: "Inspect the pending work.", EveryS: 60, CreatedBy: store.ScheduleCreatedHuman})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.RunScheduleNow(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("initial check failed: %+v", got)
	}
	waitSettled(t, e, th.ID)
	select {
	case <-f.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker never entered the real HTTP adapter")
	}
	rt := e.runtimeFor(th.ID)
	rt.mu.Lock()
	reg := rt.parked
	rt.mu.Unlock()
	if reg == nil {
		t.Fatal("successful check did not park the worker registry")
	}
	progress := reg.Progress()
	if len(progress) != 1 || !progress[0].Running || f.workers.Load() != 1 {
		t.Fatalf("initial worker is not live: progress=%+v HTTP=%d", progress, f.workers.Load())
	}
	return th, sch, reg, progress[0].AgentID
}

func failOwnedScheduleCheck(t *testing.T, e *Engine, f *scheduleOwnershipFixture, sch *store.Schedule) *store.Turn {
	t.Helper()
	f.setMode(1)
	turn, err := e.RunScheduleNow(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	run := waitForScheduleRun(t, e, turn.ScheduleRunID)
	waitSettled(t, e, turn.ThreadID)
	if got.Status != store.TurnError || !strings.Contains(got.Error, "scheduled check") || run.Status != store.ScheduleRunError || !run.Unread {
		t.Fatalf("unperformed check must still fail visibly: turn=%+v run=%+v", got, run)
	}
	if f.steps.Load() != 2 {
		t.Fatalf("manager requests=%d; scheduled evidence failure must have one bounded continuation", f.steps.Load())
	}
	// A failed scheduled check is already terminally ineligible. It must not
	// enter the ordinary successful-final collection/continuation workflow.
	if e.Status(turn.ThreadID).AwaitingContinue {
		t.Fatal("failed scheduled check paused for an ordinary worker-collection continuation")
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		var retry struct{ Reason string }
		if event.Kind == KindMaxIterations || event.Kind == KindMaxIterationsContinued || event.Kind == KindModelRetry && json.Unmarshal([]byte(event.Text), &retry) == nil && retry.Reason == "workers_uncollected" {
			t.Fatalf("failed scheduled check entered ordinary worker collection: kind=%s seq=%d", event.Kind, event.Seq)
		}
	}
	return got
}

func assertScheduleWorkerStillOwned(t *testing.T, e *Engine, f *scheduleOwnershipFixture, th *store.Thread, reg *swarm.Registry, id string) {
	t.Helper()
	rt := e.runtimeFor(th.ID)
	rt.mu.Lock()
	parked := rt.parked
	rt.mu.Unlock()
	progress := reg.Progress()
	if parked != reg || len(progress) != 1 || progress[0].AgentID != id || !progress[0].Running || progress[0].Err != "" {
		t.Fatalf("failed manager check cancelled its still-owned worker: same registry=%v progress=%+v", parked == reg, progress)
	}
	select {
	case <-f.cancelled:
		t.Fatal("failed manager check cancelled the worker's HTTP request")
	default:
	}
	if hasKind(t, e, th.ID, KindCleanup) {
		t.Fatal("failed manager check recorded global worker cleanup despite a surviving owner")
	}
}

func TestFailedScheduledCheckKeepsWorkerForTheNextRecurringWake(t *testing.T) {
	e, f := newScheduleOwnershipEngine(t)
	th, sch, reg, id := startOwnedScheduleWorker(t, e, f)
	failed := failOwnedScheduleCheck(t, e, f, sch)
	assertScheduleWorkerStillOwned(t, e, f, th, reg, id)
	assertScheduledWorkerRetention(t, e, failed.ID, "thread_wake", sch.ID, id)
	f.setMode(2)
	turn, err := e.RunScheduleNow(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("next scheduled check failed: %+v", got)
	}
	waitSettled(t, e, th.ID)
	assertScheduleWorkerStillOwned(t, e, f, th, reg, id)
	events, _ := e.Replay(th.ID, 0)
	spawns := 0
	for _, ev := range events {
		if ev.Kind == swarm.NotifySpawned.String() {
			spawns++
			if ev.AgentID != id {
				t.Fatalf("wake changed worker identity: %+v", ev)
			}
		}
	}
	if spawns != 1 || f.workers.Load() != 1 {
		t.Fatalf("next wake restarted a live worker: spawned=%d worker HTTP requests=%d", spawns, f.workers.Load())
	}
	f.mu.Lock()
	lastInput := f.managerInputs[len(f.managerInputs)-1]
	f.mu.Unlock()
	if !strings.Contains(lastInput, id) || !strings.Contains(lastInput, "running") {
		t.Fatalf("next check's HTTP input lost its live worker: %s", lastInput)
	}
	close(f.finish)
	waitRestartState(t, func() bool {
		events, _ := e.Replay(th.ID, 0)
		for _, ev := range events {
			if ev.Kind == swarm.NotifyFinished.String() && ev.AgentID == id {
				if ev.Err != "" {
					t.Errorf("retained worker did not finish naturally: %s", ev.Err)
				}
				return true
			}
		}
		return false
	})
}

func TestFailedOneShotCheckKeepsGoalOwnedWorkerWithoutBlockingOrAutoRetry(t *testing.T) {
	e, f := newScheduleOwnershipEngine(t)
	th, sch, reg, id := startOwnedScheduleWorker(t, e, f)
	if err := e.SetThreadGoal(th.ID, "Finish the assigned inspection."); err != nil {
		t.Fatal(err)
	}
	delay := 60
	if _, err := e.PatchScheduleFields(sch.ID, ScheduleFields{DelayS: &delay}); err != nil {
		t.Fatal(err)
	}
	failed := failOwnedScheduleCheck(t, e, f, sch)
	assertScheduleWorkerStillOwned(t, e, f, th, reg, id)
	assertScheduledWorkerRetention(t, e, failed.ID, "goal", th.ID, id)
	goal, _ := e.Store().GetThread(th.ID)
	if goal.GoalBlocked || goal.GoalComplete || goal.GoalCapped || !pursuingGoal(goal) {
		t.Fatalf("failed check changed its valid standing owner: %+v", goal)
	}
	wake, err := e.Store().ActiveThreadWake(th.ID)
	if err != nil || wake != nil {
		t.Fatalf("one-shot was not consumed: wake=%+v err=%v", wake, err)
	}
	// Waiting briefly catches an accidental automatic third manager attempt;
	// unlike a pending wake, this goal has no scheduler row to suppress it.
	time.Sleep(100 * time.Millisecond)
	turns, _ := e.Store().ListTurns(th.ID)
	if len(turns) != 2 || f.steps.Load() != 2 || hasKind(t, e, th.ID, KindGoalBlocked) || hasKind(t, e, th.ID, KindGoalContinued) {
		t.Fatalf("failed check blocked or immediately retried its goal: turns=%d manager calls=%d", len(turns), f.steps.Load())
	}
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatal(err)
	}
	assertScheduleWorkerCancelled(t, f)
}

func TestFailedOneShotCheckCleansWorkerWithoutRemainingOwner(t *testing.T) {
	e, f := newScheduleOwnershipEngine(t)
	th, sch, _, _ := startOwnedScheduleWorker(t, e, f)
	delay := 60
	if _, err := e.PatchScheduleFields(sch.ID, ScheduleFields{DelayS: &delay}); err != nil {
		t.Fatal(err)
	}
	failOwnedScheduleCheck(t, e, f, sch)
	if !hasKind(t, e, th.ID, KindCleanup) {
		t.Fatal("failed consumed one-shot left an unowned worker running")
	}
	assertScheduleWorkerCancelled(t, f)
}

func TestScheduledClassifierOutageKeepsTheOriginalWorkerUntilRecovery(t *testing.T) {
	e, f := newScheduleOwnershipEngine(t)
	th, sch, reg, id := startOwnedScheduleWorker(t, e, f)
	f.setMode(2)
	f.policyUnavailable.Store(true)
	beforePolicies := f.policyCalls.Load()
	turn, err := e.RunScheduleNow(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	waitRestartState(t, func() bool {
		rows, err := e.store.ListPendingScheduleAnswers(turn.ID)
		if err != nil {
			return false
		}
		for _, row := range rows {
			if row.Kind == "report" && row.Attempts == 1 {
				return true
			}
		}
		return false
	})
	rt := e.runtimeFor(th.ID)
	rt.mu.Lock()
	active := rt.reg
	rt.mu.Unlock()
	progress := reg.Progress()
	status := e.Status(th.ID)
	if !status.Running || status.TurnID != turn.ID || active != reg || len(progress) != 1 || progress[0].AgentID != id || !progress[0].Running || f.workers.Load() != 1 || f.steps.Load() != 2 || f.policyCalls.Load() != beforePolicies+1 {
		t.Fatalf("classification outage changed the original turn or worker: status=%+v same_registry=%v progress=%+v worker_requests=%d manager_requests=%d policy_requests=%d", status, active == reg, progress, f.workers.Load(), f.steps.Load(), f.policyCalls.Load()-beforePolicies)
	}
	select {
	case <-f.cancelled:
		t.Fatal("pending classification cancelled the actual worker HTTP request")
	default:
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == KindCleanup || ev.Kind == KindWorkersRetained || ev.Kind == KindScheduleReport || ev.Kind == swarm.NotifyDone.String() || ev.Kind == swarm.NotifyError.String() || ev.Kind == KindModelRetry {
			t.Fatalf("classification outage became a terminal check or published unverified findings: %+v", ev)
		}
	}
	f.policyUnavailable.Store(false)
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone || got.ID != turn.ID || f.steps.Load() != 2 || f.workers.Load() != 1 || f.policyCalls.Load() != beforePolicies+2 {
		t.Fatalf("classification recovery repeated manager or worker work: turn=%+v manager_requests=%d worker_requests=%d policy_requests=%d", got, f.steps.Load(), f.workers.Load(), f.policyCalls.Load()-beforePolicies)
	}
	waitSettled(t, e, th.ID)
	assertScheduleWorkerStillOwned(t, e, f, th, reg, id)
	if err := e.Interrupt(th.ID); err != nil {
		t.Fatal(err)
	}
	assertScheduleWorkerCancelled(t, f)
}

func TestFailedScheduledCheckPreservationStillHonorsStopAndLastWakeCancellation(t *testing.T) {
	for _, action := range []string{"stop", "cancel-last-wake", "pause-last-wake"} {
		t.Run(action, func(t *testing.T) {
			e, f := newScheduleOwnershipEngine(t)
			th, sch, reg, id := startOwnedScheduleWorker(t, e, f)
			failOwnedScheduleCheck(t, e, f, sch)
			assertScheduleWorkerStillOwned(t, e, f, th, reg, id)
			var err error
			switch action {
			case "stop":
				err = e.Interrupt(th.ID)
			case "cancel-last-wake":
				err = e.CancelSchedule(sch.ID)
			case "pause-last-wake":
				_, err = e.PatchSchedule(sch.ID, store.SchedulePaused)
			}
			if err != nil {
				t.Fatal(err)
			}
			assertScheduleWorkerCancelled(t, f)
			rt := e.runtimeFor(th.ID)
			rt.mu.Lock()
			parked := rt.parked
			rt.mu.Unlock()
			if parked != nil || e.Status(th.ID).Workers != 0 {
				t.Fatal("human stop or removal of the last wake retained an unowned worker")
			}
		})
	}
}

func assertScheduleWorkerCancelled(t *testing.T, f *scheduleOwnershipFixture) {
	t.Helper()
	select {
	case <-f.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("worker model request survived termination of its final owner")
	}
}

func assertScheduledWorkerRetention(t *testing.T, e *Engine, turnID, ownerKind, ownerID, workerID string) {
	t.Helper()
	events, err := e.Store().ListTurnEvents(turnID)
	if err != nil {
		t.Fatal(err)
	}
	retained, terminal := -1, -1
	for i, event := range events {
		if event.Kind == "workers_retained" {
			if retained != -1 {
				t.Fatal("failed check emitted duplicate worker retention receipts")
			}
			retained = i
			var receipt struct {
				Reason    string   `json:"reason"`
				OwnerKind string   `json:"owner_kind"`
				OwnerID   string   `json:"owner_id"`
				WorkerIDs []string `json:"worker_ids"`
			}
			if err := json.Unmarshal([]byte(event.Text), &receipt); err != nil {
				t.Fatal(err)
			}
			if receipt.Reason != "scheduled_check_failed" || receipt.OwnerKind != ownerKind || receipt.OwnerID != ownerID || len(receipt.WorkerIDs) != 1 || receipt.WorkerIDs[0] != workerID {
				t.Fatalf("Trace lost the surviving owner and worker identity: %+v", receipt)
			}
		}
		if event.Kind == swarm.NotifyError.String() && event.AgentID == swarm.DefaultManagerID {
			terminal = i
		}
	}
	if retained < 0 || terminal <= retained {
		t.Fatalf("retention must be durably recorded before manager failure: retained=%d terminal=%d", retained, terminal)
	}
}

func TestScheduledManagerModelErrorPreservesOwnedWorkerWithoutImmediateRetry(t *testing.T) {
	for _, tc := range []struct {
		owner, failure string
		mode, requests int32
	}{
		{"thread_wake", "refused", 3, 1},
		{"goal", "refused", 3, 1},
		{"thread_wake", "rate limited", 4, modelErrorRetries + 1},
		{"goal", "rate limited", 4, modelErrorRetries + 1},
	} {
		t.Run(tc.owner+"/"+tc.failure, func(t *testing.T) {
			e, f := newScheduleOwnershipEngine(t)
			useEngineBackoffClock(t, e)
			th, sch, reg, id := startOwnedScheduleWorker(t, e, f)
			ownerID := sch.ID
			if tc.owner == "goal" {
				if err := e.SetThreadGoal(th.ID, "Finish the assigned inspection."); err != nil {
					t.Fatal(err)
				}
				delay := 60
				if _, err := e.PatchScheduleFields(sch.ID, ScheduleFields{DelayS: &delay}); err != nil {
					t.Fatal(err)
				}
				ownerID = th.ID
			}
			f.setMode(tc.mode)
			turn, err := e.RunScheduleNow(sch.ID)
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			run := waitForScheduleRun(t, e, turn.ScheduleRunID)
			waitSettled(t, e, th.ID)
			if got.Status != store.TurnError || !strings.Contains(got.Error, tc.failure) || run.Status != store.ScheduleRunError || !run.Unread {
				t.Fatalf("model failure was hidden instead of scoped: turn=%+v run=%+v", got, run)
			}
			assertScheduleWorkerStillOwned(t, e, f, th, reg, id)
			assertScheduledWorkerRetention(t, e, turn.ID, tc.owner, ownerID, id)
			goal, _ := e.Store().GetThread(th.ID)
			turns, _ := e.Store().ListTurns(th.ID)
			if goal.GoalBlocked || hasKind(t, e, th.ID, KindGoalContinued) || f.steps.Load() != tc.requests || len(turns) != 2 {
				t.Fatalf("scheduled model failure mutated pursuit or exceeded its retry budget: goal=%+v requests=%d turns=%d", goal, f.steps.Load(), len(turns))
			}
			if tc.failure == "rate limited" {
				assertFixtureBackoffWaits(t, e, 5*time.Second, 10*time.Second)
			}
		})
	}
}

func TestFailedScheduledCheckDoesNotRetainWorkersForInactiveGoals(t *testing.T) {
	for _, state := range []string{"goal_blocked", "goal_capped", "goal_idle", "goal_complete"} {
		t.Run(state, func(t *testing.T) {
			e, f := newScheduleOwnershipEngine(t)
			th, sch, _, _ := startOwnedScheduleWorker(t, e, f)
			if err := e.SetThreadGoal(th.ID, "Finish the assigned inspection."); err != nil {
				t.Fatal(err)
			}
			if err := e.Store().UpdateThread(th.ID, map[string]any{state: true}); err != nil {
				t.Fatal(err)
			}
			delay := 60
			if _, err := e.PatchScheduleFields(sch.ID, ScheduleFields{DelayS: &delay}); err != nil {
				t.Fatal(err)
			}
			failed := failOwnedScheduleCheck(t, e, f, sch)
			if !hasKind(t, e, th.ID, KindCleanup) {
				t.Fatal("inactive goal retained an orphaned worker after the one-shot was consumed")
			}
			assertScheduleWorkerCancelled(t, f)
			events, _ := e.Store().ListTurnEvents(failed.ID)
			for _, event := range events {
				if event.Kind == "workers_retained" {
					t.Fatalf("inactive goal incorrectly supplied an owner: %+v", event)
				}
			}
		})
	}
}
