package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

// A later completion grant or human decision wins over an old checkpoint;
// otherwise a second process death resurrects an already settled wait.
func TestColdRecoveryUsesWaitDecisionsAfterItsOldCheckpoint(t *testing.T) {
	for _, decision := range []string{"completed", "continue", "decline"} {
		t.Run(decision, func(t *testing.T) {
			e, captured := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, req scheduleCompletionRequest) {
				if decision == "decline" {
					t.Error("acknowledged decline reached the manager after cold recovery")
				}
				if step == 1 {
					writeScheduleCompletionTool(w, req, "wait_agents", `{"agent_ids":["original-worker"],"timeout_s":1}`)
					return
				}
				writeContextFlowReply(w, req.Stream, map[string]any{"content": "The original terminal result was collected."}, "stop")
			})
			e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
			th, _ := e.CreateThread("cold collection wait", "", "")
			turn := plantUnfinishedTurn(t, e, th.ID, "Collect the assigned result.")
			worker := swarm.PendingAgent{AgentID: "original-worker", Generation: 2, Running: true}
			p := workerCollectionPause{Reason: "workers_uncollected", WaitID: "original-wait", Limit: 3, Extend: 4, Workers: []swarm.PendingAgent{worker}, WorkerIDs: []string{worker.AgentID}, AutomaticResume: true}
			rt := e.runtimeFor(th.ID)
			reg := swarm.NewRegistry()
			t.Cleanup(reg.Close)
			if err := reg.PlantFinished(swarm.FinishedWorker{ID: worker.AgentID, Role: "reviewer", Generation: 2, Result: "The assigned state was verified."}); err != nil {
				t.Fatal(err)
			}
			_, cancel := context.WithCancel(context.Background())
			idle := make(chan struct{})
			if !rt.occupy(reg, cancel, turn.ID, idle) {
				t.Fatal("fixture runtime was not claimed")
			}
			rt.mu.Lock()
			rt.collectionPause, rt.continueCh = &p, make(chan bool, 1)
			if err := rt.saveWorkerPauseEvent(turn, KindMaxIterations, &p); err != nil {
				t.Fatal(err)
			}
			rt.mu.Unlock()
			cp := restartCheckpoint{Version: 1, Pause: "worker_collection", CollectionPause: &p, CapUsed: 3, CapExtend: 4, ManagerHistory: []*schema.Message{schema.UserMessage(turn.UserText)}, Workers: reg.SnapshotWorkers()}
			body, _ := json.Marshal(cp)
			if err := e.store.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindRestartCheckpoint, Text: string(body)}); err != nil {
				t.Fatal(err)
			}
			if decision == "completed" {
				rt.mu.Lock()
				err := rt.saveWorkerPauseEvent(turn, KindWorkersCollectionResumed, workerCollectionReceipt{p.WaitID, p.WorkerIDs, reg.PendingAgents()})
				rt.mu.Unlock()
				if err != nil {
					t.Fatal(err)
				}
			} else if err := e.ContinueTurn(th.ID, decision == "continue"); err != nil {
				t.Fatal(err)
			}
			rt.mu.Lock()
			rt.continueCh, rt.collectionPause = nil, nil
			rt.mu.Unlock()
			rt.release(cancel, idle, turn.ID)
			e.Shutdown()
			if err := e.store.Close(); err != nil {
				t.Fatal(err)
			}
			next := openRestartReplayEngine(t, e.cfg)
			if count, err := next.ResumeOrphanedTurns(); err != nil || count != 1 {
				t.Fatalf("cold resume=%d %v", count, err)
			}
			got := waitForTurn(t, next, turn.ID)
			if decision == "decline" {
				if got.Status != store.TurnCancelled || len(captured.snapshot()) != 0 {
					t.Fatalf("decline was lost after cold recovery: %+v", got)
				}
			} else if got.Status != store.TurnDone || len(captured.snapshot()) != 2 {
				t.Fatalf("old checkpoint resurrected a settled wait: %+v requests=%d", got, len(captured.snapshot()))
			}
		})
	}
}

// Stop owns recordMu before it can update the in-memory runtime. Completion
// must recheck that durable intent while holding the same publication lock.
func TestWorkerCompletionReceiptCannotOvertakeAnAcknowledgedStop(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("Stop completion race", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "Collect the result.")
	reg := swarm.NewRegistry()
	defer reg.Close()
	worker := swarm.PendingAgent{AgentID: "original-worker", Generation: 1, Running: true}
	if err := reg.PlantFinished(swarm.FinishedWorker{ID: worker.AgentID, Generation: 1, Result: "Terminal result."}); err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(th.ID)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	idle := make(chan struct{})
	if !rt.occupy(reg, cancel, turn.ID, idle) {
		t.Fatal("runtime was not claimed")
	}
	defer rt.release(cancel, idle, turn.ID)
	p := workerCollectionPause{Reason: "workers_uncollected", WaitID: "stop-race", Extend: 2, WorkerIDs: []string{worker.AgentID}, Workers: []swarm.PendingAgent{worker}, AutomaticResume: true}
	if err := rt.saveWorkerPauseEvent(turn, KindMaxIterations, &p); err != nil {
		t.Fatal(err)
	}
	stopEntered, releaseStop := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(releaseStop) }) }
	defer unblock()
	if err := e.store.DB().Callback().Create().Before("gorm:create").Register("worker_wait_stop_barrier", func(tx *gorm.DB) {
		if ev, ok := tx.Statement.Dest.(*store.Event); ok && ev.Kind == KindUserStopRequested {
			close(stopEntered)
			<-releaseStop
		}
	}); err != nil {
		t.Fatal(err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- e.Interrupt(th.ID) }()
	ordinaryAwait(t, stopEntered, "Stop did not reach its durable event write")
	rt.mu.Lock()
	unblock()
	err := rt.saveWorkerPauseEvent(turn, KindWorkersCollectionResumed, workerCollectionReceipt{p.WaitID, p.WorkerIDs, reg.PendingAgents()})
	continueErr := rt.saveWorkerCollectionContinue(turn, &p)
	stoppedInMemory := rt.userStopped
	rt.mu.Unlock()
	if !errors.Is(err, context.Canceled) || !errors.Is(continueErr, context.Canceled) || !stoppedInMemory {
		t.Fatalf("completion overtook durable Stop: %v stopped=%v", err, stoppedInMemory)
	}
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not finish after completion refusal")
	}
	if ctx.Err() == nil {
		t.Fatal("real Stop did not cancel its runtime")
	}
	events, _ := e.store.ListTurnEvents(turn.ID)
	for _, ev := range events {
		if ev.Kind == KindWorkersCollectionResumed {
			t.Fatal("completion grant was appended after Stop")
		}
	}
}

func TestTypedContinueRollsBackRenewalWhenItsConsumptionCannotBeSaved(t *testing.T) {
	f := newOrdinaryLifecycleFixture(t, "continue")
	f.start(t)
	f.awaitPause(t)
	if err := f.e.store.DB().Exec("CREATE TRIGGER refuse_consumption BEFORE INSERT ON events WHEN NEW.kind='model_retry' BEGIN SELECT RAISE(ABORT,'consumption unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := f.e.ContinueTurn(f.thread.ID, true); err == nil {
		t.Fatal("partial continuation decision was acknowledged")
	}
	f.releaseWorker(0)
	if got := waitForTurn(t, f.e, f.turn.ID); got.Status != store.TurnError || f.managers.Load() != 3 {
		t.Fatalf("failed atomic decision later granted work: %+v manager=%d", got, f.managers.Load())
	}
	events, _ := f.e.store.ListTurnEvents(f.turn.ID)
	for _, ev := range events {
		if ev.Kind == KindMaxIterationsContinued || ev.Kind == KindWorkersCollectionResumed {
			t.Fatalf("partial renewal was published: %s", ev.Kind)
		}
	}
	if ordinaryCorrectionCount(t, f.e, f.turn.ID) != 1 {
		t.Fatal("rollback changed the original consumed allowance")
	}
}

// The ACK transaction consumes the newly granted attempt before the process
// can die. Live and cold paths must permit the same single early-final try.
func TestColdHumanContinueCannotGrantAnExtraEarlyFinalAttempt(t *testing.T) {
	e, captured := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, req scheduleCompletionRequest) {
		writeContextFlowReply(w, req.Stream, map[string]any{"content": "The current state was checked."}, "stop")
	})
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	th, _ := e.CreateThread("continued collection", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "Collect the assigned result.")
	p := testCollectionPause()
	rt := e.runtimeFor(th.ID)
	reg := swarm.NewRegistry()
	defer reg.Close()
	if err := reg.PlantFinished(swarm.FinishedWorker{ID: p.WorkerIDs[0], Generation: 1, Result: "Verified terminal result."}); err != nil {
		t.Fatal(err)
	}
	_, cancel := context.WithCancel(context.Background())
	idle := make(chan struct{})
	if !rt.occupy(reg, cancel, turn.ID, idle) {
		t.Fatal("runtime was not claimed")
	}
	if err := rt.recordWorkerCollectionRetry(turn, p.WorkerIDs); err != nil {
		t.Fatal(err)
	}
	rt.mu.Lock()
	rt.collectionPause, rt.continueCh = p, make(chan bool, 1)
	if err := rt.saveWorkerPauseEvent(turn, KindMaxIterations, p); err != nil {
		t.Fatal(err)
	}
	rt.mu.Unlock()
	cp := restartCheckpoint{Version: 1, Pause: "worker_collection", CollectionPause: p, ManagerHistory: []*schema.Message{schema.UserMessage(turn.UserText)}, Workers: reg.SnapshotWorkers()}
	body, _ := json.Marshal(cp)
	if err := e.store.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindRestartCheckpoint, Text: string(body)}); err != nil {
		t.Fatal(err)
	}
	if err := e.ContinueTurn(th.ID, true); err != nil {
		t.Fatal(err)
	}
	rt.mu.Lock()
	rt.collectionPause, rt.continueCh = nil, nil
	rt.mu.Unlock()
	rt.release(cancel, idle, turn.ID)
	e.Shutdown()
	if err := e.store.Close(); err != nil {
		t.Fatal(err)
	}
	next := openRestartReplayEngine(t, e.cfg)
	if n, err := next.ResumeOrphanedTurns(); err != nil || n != 1 {
		t.Fatalf("cold resume=%d %v", n, err)
	}
	ordinaryEventually(t, func() bool { return next.Status(th.ID).AwaitingContinue }, "cold continuation did not return to its collection pause")
	if got := len(captured.snapshot()); got != 1 {
		t.Fatalf("one human continuation granted %d early-final requests after crash", got)
	}
	if ordinaryCorrectionCount(t, next, turn.ID) != 2 {
		t.Fatal("cold continuation lost or duplicated its atomic consumption receipt")
	}
}
