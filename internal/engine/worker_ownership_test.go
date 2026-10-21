package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
)

func TestWorkerOwnerRequiresAnExecutableGoalOrActiveThreadWake(t *testing.T) {
	for _, boundary := range []string{"none", "goal", "blocked", "complete", "capped", "idle", "plan", "plan-with-wake", "budget", "wake", "paused", "cancelled", "standalone", "paused-goal-with-wake", "missing"} {
		t.Run(boundary, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("", "", "")
			fields := map[string]any{"goal": "continue assigned work"}
			wantKind, wantID := "", ""
			switch boundary {
			case "goal":
				wantKind, wantID = "goal", th.ID
			case "blocked", "complete", "capped", "idle":
				fields["goal_"+boundary] = true
			case "plan", "plan-with-wake":
				fields["plan_mode"] = true
			case "budget":
				fields["goal_auto_turns"] = e.cfg.Swarm.GoalAutoTurns()
			case "paused-goal-with-wake":
				fields["goal_capped"] = true
			default:
				fields["goal"] = ""
			}
			if err := e.store.UpdateThread(th.ID, fields); err != nil {
				t.Fatal(err)
			}
			if boundary == "wake" || boundary == "paused" || boundary == "cancelled" || boundary == "paused-goal-with-wake" || boundary == "standalone" || boundary == "plan-with-wake" {
				wake := mustCreateWake(t, e, th.ID)
				switch boundary {
				case "paused":
					_ = e.store.UpdateSchedule(wake.ID, map[string]any{"status": store.SchedulePaused})
				case "cancelled":
					_, _ = e.store.CancelSchedule(wake.ID)
				case "standalone":
					_ = e.store.UpdateSchedule(wake.ID, map[string]any{"kind": store.ScheduleStandalone})
				case "plan-with-wake":
				default:
					wantKind, wantID = "thread_wake", wake.ID
				}
			}
			rt := e.runtimeFor(th.ID)
			if boundary == "missing" {
				rt = newRuntime(e, "missing")
			}
			kind, id := rt.workerOwner()
			if kind != wantKind || id != wantID {
				t.Fatalf("owner=%s/%s, want %s/%s", kind, id, wantKind, wantID)
			}
		})
	}
}

func TestScheduledFailureIsolationDoesNotHideResourceOrHumanStops(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		scheduled    bool
		err          error
		want         bool
	}{
		{"check evidence", store.TurnError, true, errors.New("no work recorded"), true},
		{"model", store.TurnError, true, errors.New("request rejected"), true},
		{"ordinary", store.TurnError, false, errors.New("request rejected"), false},
		{"done", store.TurnDone, true, nil, false},
		{"stop", store.TurnCancelled, true, context.Canceled, false},
		{"context budget", store.TurnError, true, errors.New("maximum context length exceeded"), false},
		{"worker watchdog", store.TurnError, true, fmt.Errorf("run: %w", &swarm.WorkerTimeoutLimitError{}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isolatedScheduledFailure(&store.Turn{ScheduleContinue: tc.scheduled}, tc.status, tc.err); got != tc.want {
				t.Fatalf("isolation=%v want %v", got, tc.want)
			}
		})
	}
}

func TestFailedCheckOwnershipReceiptIsRequiredBeforeParking(t *testing.T) {
	for _, boundary := range []string{"retained", "ordinary-error", "cancelled", "no-owner", "human-stop", "handoff", "storage-failure", "done-owned", "done-unowned"} {
		t.Run(boundary, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("", "", "")
			if boundary != "no-owner" && boundary != "done-unowned" {
				mustCreateWake(t, e, th.ID)
			}
			turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: boundary != "ordinary-error"})
			rt := e.runtimeFor(th.ID)
			reg := swarm.NewRegistry()
			t.Cleanup(reg.Close)
			status := store.TurnError
			switch boundary {
			case "human-stop":
				rt.userStopped = true
			case "handoff":
				rt.abandoned = true
			case "cancelled":
				status = store.TurnCancelled
			case "done-owned", "done-unowned":
				status = store.TurnDone
			case "storage-failure":
				if err := e.store.DB().Exec("CREATE TRIGGER refuse_worker_retention BEFORE INSERT ON events WHEN NEW.kind = 'workers_retained' BEGIN SELECT RAISE(ABORT, 'fixture failure'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			want := boundary == "retained" || boundary == "done-owned"
			if got := rt.parkAfterTurn(turn, status, errors.New("manager failed"), reg); got != want {
				t.Fatalf("park=%v want %v", got, want)
			}
			if (rt.parked == reg) != want {
				t.Fatal("parking outcome and owned registry disagree")
			}
			if ids := e.retainedWorkerIDs(&store.Turn{ID: turn.ID, Status: store.TurnError, ScheduleContinue: true}); (ids != nil) != (boundary == "retained") {
				t.Fatalf("retention receipt=%v", ids)
			}
		})
	}
}

func ownedBlockingRegistry(t *testing.T) (*swarm.Registry, *swarm.Handle) {
	entered := make(chan struct{})
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	reg.ModelBuilder = func(_, _ string) model.BaseChatModel { return &holdModel{entered: entered} }
	h, err := reg.Spawn(context.Background(), "inspector", "inspect assigned work", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not enter its cancellable model")
	}
	return reg, h
}

// These interleavings used to let a revoker observe a running manager and
// return before that manager parked its now-unowned workers.
func TestFinalizationRechecksOwnerAndStopAfterItBecomesIdle(t *testing.T) {
	for _, boundary := range []string{"cancel", "pause", "stop", "keep-owner", "running", "handoff", "absent"} {
		t.Run(boundary, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("", "", "")
			wake := mustCreateWake(t, e, th.ID)
			turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true})
			rt := e.runtimeFor(th.ID)
			reg, h := ownedBlockingRegistry(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			idle := make(chan struct{})
			if !rt.occupy(reg, cancel, turn.ID, idle) || !rt.parkAfterTurn(turn, store.TurnError, errors.New("manager failed"), reg) {
				t.Fatal("fixture could not park owned workers")
			}
			switch boundary {
			case "cancel":
				if err := e.CancelSchedule(wake.ID); err != nil {
					t.Fatal(err)
				}
			case "pause":
				if _, err := e.PatchSchedule(wake.ID, store.SchedulePaused); err != nil {
					t.Fatal(err)
				}
			case "stop":
				if err := e.Interrupt(th.ID); err != nil {
					t.Fatal(err)
				}
			case "handoff":
				rt.abandon()
			case "absent":
				rt.takeParkedRegistry()
			}
			if boundary != "running" {
				rt.release(cancel, idle, turn.ID)
			}
			rt.reapUnownedParked()
			wantStopped := boundary == "cancel" || boundary == "pause" || boundary == "stop"
			if wantStopped {
				select {
				case <-h.Done():
				case <-time.After(5 * time.Second):
					t.Fatal("revoked owner left its worker running")
				}
				if rt.parked != nil || !hasKind(t, e, th.ID, KindCleanup) {
					t.Fatal("revoked ownership lacks cleanup receipt")
				}
			} else {
				select {
				case <-h.Done():
					t.Fatal("live owner lost its worker")
				default:
				}
			}
			_ = ctx
			rt.release(cancel, idle, turn.ID)
		})
	}
}

func TestRetainedWorkerRecoveryRequiresValidDurableEvidence(t *testing.T) {
	for _, boundary := range []string{"valid", "terminal-after-receipt", "stop-after-terminal", "no-owner", "human-stop", "cleanup", "other-agent", "late-receipt", "malformed", "wrong-reason", "wrong-kind", "missing-owner", "blank-owner", "invalid-worker", "no-ids", "different-worker", "ordinary", "done", "read-failure"} {
		t.Run(boundary, func(t *testing.T) {
			e := newTestEngine(t)
			th, turn, wake := plantParkedRestart(t, e)
			if err := e.store.UpdateTurn(turn.ID, map[string]any{"schedule_continue": boundary != "ordinary"}); err != nil {
				t.Fatal(err)
			}
			if err := e.store.FinishTurn(turn.ID, store.TurnError, "", "scheduled check failed"); err != nil {
				t.Fatal(err)
			}
			receipt := workerRetention{Reason: "scheduled_check_failed", OwnerKind: "thread_wake", OwnerID: wake.ID, WorkerIDs: []string{"worker-7"}}
			switch boundary {
			case "wrong-reason":
				receipt.Reason = "success"
			case "wrong-kind":
				receipt.OwnerKind = "standalone"
			case "missing-owner":
				receipt.OwnerID = ""
			case "blank-owner":
				receipt.OwnerID = " "
			case "invalid-worker":
				receipt.WorkerIDs = []string{"worker-7", "", " manager", "manager"}
			case "no-ids":
				receipt.WorkerIDs = nil
			case "different-worker":
				receipt.WorkerIDs = []string{"unknown"}
			}
			body, _ := json.Marshal(receipt)
			if boundary == "malformed" {
				body = []byte("{")
			}
			agentID := swarm.DefaultManagerID
			if boundary == "other-agent" {
				agentID = "worker-7"
			}
			if boundary == "late-receipt" {
				e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyError.String(), AgentID: swarm.DefaultManagerID, Err: "scheduled check failed"})
			}
			e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindWorkersRetained, AgentID: agentID, Text: string(body)})
			if boundary == "terminal-after-receipt" || boundary == "stop-after-terminal" {
				e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: swarm.NotifyError.String(), AgentID: swarm.DefaultManagerID, Err: "scheduled check failed"})
			}
			switch boundary {
			case "no-owner":
				_, _ = e.store.CancelSchedule(wake.ID)
			case "human-stop", "stop-after-terminal":
				e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUserStopRequested})
			case "cleanup":
				e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindCleanup})
			case "done":
				_ = e.store.FinishTurn(turn.ID, store.TurnDone, "", "")
			case "read-failure":
				_ = e.store.Close()
			}
			stored, err := e.store.GetTurn(turn.ID)
			if boundary == "read-failure" {
				if ids := e.retainedWorkerIDs(&store.Turn{ID: turn.ID, Status: store.TurnError, ScheduleContinue: true}); ids != nil {
					t.Fatal("failed read invented ownership")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			n, err := e.resumeParkedThread(th)
			want := 0
			if boundary == "valid" || boundary == "done" || boundary == "terminal-after-receipt" {
				want = 1
			}
			if err != nil || n != want {
				t.Fatalf("recovered=%d want=%d err=%v", n, want, err)
			}
			if want > 0 {
				waitRestartState(t, func() bool { return e.Status(th.ID).Workers == 0 })
				if got, _ := e.store.GetTurn(turn.ID); got.Status != stored.Status || got.Error != stored.Error {
					t.Fatal("worker recovery changed manager failure")
				}
				if turns, _ := e.store.ListTurns(th.ID); len(turns) != 1 {
					t.Fatal("worker recovery restarted the manager")
				}
			}
		})
	}
}

func TestFailedScheduledCheckRestoresItsWorkerThroughActualEngineHandoff(t *testing.T) {
	e, fixture := newScheduleOwnershipEngine(t)
	th, wake, _, id := startOwnedScheduleWorker(t, e, fixture)
	failed := failOwnedScheduleCheck(t, e, fixture, wake)
	beforeWake, err := e.GetSchedule(wake.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status, err := e.PrepareRestart(ctx)
	if err != nil || !status.Ready {
		t.Fatalf("failed-check handoff=%+v err=%v", status, err)
	}
	cp := e.checkpointForTurn(failed.ID)
	if cp == nil || len(cp.Workers) != 1 || cp.Workers[0].ID != id || cp.Workers[0].Finished {
		t.Fatalf("handoff lost the owned worker: %+v", cp)
	}
	next := restartedTestEngine(t, e)
	if count, err := next.ResumeParkedWorkers(); err != nil || count != 1 {
		t.Fatalf("restore after failed check=%d err=%v", count, err)
	}
	waitRestartState(t, func() bool { return fixture.workers.Load() == 2 })
	close(fixture.finish)
	waitRestartState(t, func() bool {
		events, _ := next.store.ListTurnEvents(failed.ID)
		for _, ev := range events {
			if ev.Kind == swarm.NotifyFinished.String() && ev.AgentID == id {
				if ev.Err != "" {
					t.Errorf("restored worker failed: %s", ev.Err)
				}
				return true
			}
		}
		return false
	})
	got, _ := next.store.GetTurn(failed.ID)
	run, _ := next.store.GetRun(failed.ScheduleRunID)
	if got.Status != store.TurnError || got.Error != failed.Error || run.Status != store.ScheduleRunError || !run.Unread {
		t.Fatal("worker recovery changed the failed manager or unread check")
	}
	if turns, _ := next.store.ListTurns(th.ID); len(turns) != 2 || next.Status(th.ID).Running {
		t.Fatal("worker handoff reran the failed manager")
	}
	if afterWake, _ := next.GetSchedule(wake.ID); afterWake.Status != beforeWake.Status || !afterWake.NextRunAt.Equal(beforeWake.NextRunAt) {
		t.Fatal("worker recovery changed the next wake")
	}
}
