package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

// The last automatic turn cannot promise that another turn will collect work.
func TestFinalGoalAutoTurnExplainsWorkerCollectionAtTheRealModelBoundary(t *testing.T) {
	e, captured := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, req scheduleCompletionRequest) {
		writeContextFlowReply(w, req.Stream, map[string]any{"content": "The current state was checked."}, "stop")
	})
	th, _ := e.CreateThread("final continuation prompt", "", "")
	if err := e.SetThreadGoal(th.ID, "Verify the assigned state."); err != nil {
		t.Fatal(err)
	}
	if err := e.store.UpdateThread(th.ID, map[string]any{"goal_auto_turns": e.cfg.Swarm.GoalAutoTurns()}); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurnInput(th.ID, UserInput{Text: GoalContinueText(), ContinueGoal: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("turn: %+v", got)
	}
	var system string
	for _, req := range captured.snapshot() {
		for _, m := range req.Messages {
			if m.Role == schema.System {
				system += m.Content
			}
		}
	}
	for _, want := range []string{"last automatic continuation", "wait_agents", "close_agent", "before ending this turn"} {
		if !strings.Contains(system, want) {
			t.Errorf("last continuation omitted %q", want)
		}
	}
}

func TestCompletionWakeWaitsForAllWorkersAndDoesNotRenewItsBudget(t *testing.T) {
	f := newOrdinaryLifecycleFixture(t, "wake-all")
	f.start(t)
	f.awaitPause(t)
	f.releaseWorker(0)
	ordinaryEventually(t, func() bool { p := f.pending(); return len(p) == 2 && !p[0].Running }, "first worker did not finish")
	if f.managers.Load() != 4 {
		t.Fatal("one worker's completion woke the all-worker wait")
	}
	f.releaseWorker(1)
	if got := waitForTurn(t, f.e, f.turn.ID); got.Status != store.TurnDone || f.managers.Load() != 6 || ordinaryCorrectionCount(t, f.e, f.turn.ID) != 1 {
		t.Fatalf("all-worker collection failed: %+v manager=%d", got, f.managers.Load())
	}

	g := newOrdinaryLifecycleFixture(t, "wake-ignore")
	g.start(t)
	g.awaitPause(t)
	g.releaseWorker(0)
	ordinaryEventually(t, func() bool {
		p, used, err := g.e.workerCollectionWaitState(g.turn)
		return err == nil && used && p != nil && !p.AutomaticResume
	}, "second premature completion renewed an automatic wake")
	if g.managers.Load() != 4 || ordinaryCorrectionCount(t, g.e, g.turn.ID) != 1 {
		t.Fatalf("completion wake looped or renewed correction: %d", g.managers.Load())
	}
	g.assertWithheld(t)
}

func TestOrdinaryIterationPauseDoesNotResumeWhenItsHTTPWorkerFinishes(t *testing.T) {
	f := newOrdinaryLifecycleFixture(t, "continue")
	f.e.cfg.Swarm.ManagerMaxIterations = 1
	f.start(t)
	f.awaitPause(t)
	ordinaryAwait(t, f.entered[0], "ordinary capped turn never started its real worker")
	f.releaseWorker(0)
	ordinaryEventually(t, func() bool { p := f.pending(); return len(p) == 1 && !p[0].Running }, "ordinary capped turn's worker did not finish")
	if !f.e.Status(f.thread.ID).AwaitingContinue || f.managers.Load() != 1 {
		t.Fatal("worker completion bypassed the ordinary human iteration pause")
	}
	events, _ := f.e.store.ListTurnEvents(f.turn.ID)
	for _, ev := range events {
		if ev.Kind == KindWorkersCollectionResumed {
			t.Fatal("ordinary iteration cap fabricated a worker-collection grant")
		}
	}
	if err := f.e.ContinueTurn(f.thread.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, f.e, f.turn.ID); got.Status != store.TurnCancelled {
		t.Fatalf("ordinary human refusal was lost: %+v", got)
	}
}

func TestCompletionWaitSurvivesActualRestartWithTheSameGenerations(t *testing.T) {
	f := newOrdinaryLifecycleFixture(t, "wake-restart")
	f.start(t)
	f.awaitPause(t)
	before := f.pending()
	next := coldRestartReplayEngine(t, f.e)
	f.e = next
	if count, err := next.ResumeOrphanedTurns(); err != nil || count != 1 {
		t.Fatalf("resume=%d %v", count, err)
	}
	ordinaryAwait(t, f.resumed, "same-ID worker did not resume its actual HTTP request")
	ordinaryEventually(t, func() bool { return next.Status(f.thread.ID).AwaitingContinue }, "typed collection wait did not restore")
	if f.managers.Load() != 3 || ordinaryCorrectionCount(t, next, f.turn.ID) != 1 {
		t.Fatal("restart granted a manager request or renewed correction")
	}
	if p := f.pending(); len(p) != 1 || p[0].AgentID != before[0].AgentID || p[0].Generation != before[0].Generation {
		t.Fatalf("generation changed after restart: %+v", p)
	}
	f.releaseWorker(0)
	if got := waitForTurn(t, next, f.turn.ID); got.Status != store.TurnDone || f.managers.Load() != 5 || ordinaryCorrectionCount(t, next, f.turn.ID) != 1 {
		t.Fatalf("restored completion wake failed: %+v manager=%d", got, f.managers.Load())
	}
}

func TestCompletionWaitRefusesFailedReadsWritesAndDeclineAcknowledgements(t *testing.T) {
	for _, kind := range []string{"max_iterations", KindWorkersCollectionResumed, KindWorkersCollectionDeclined} {
		t.Run(kind, func(t *testing.T) {
			f := newOrdinaryLifecycleFixture(t, "continue")
			if kind == "max_iterations" {
				if err := f.e.store.DB().Exec("CREATE TRIGGER refuse_wait BEFORE INSERT ON events WHEN NEW.kind='max_iterations' BEGIN SELECT RAISE(ABORT, 'wait write refused'); END").Error; err != nil {
					t.Fatal(err)
				}
				f.start(t)
				if got := waitForTurn(t, f.e, f.turn.ID); got.Status != store.TurnError || f.managers.Load() != 3 {
					t.Fatalf("missing wait receipt granted work: %+v", got)
				}
				return
			}
			f.start(t)
			f.awaitPause(t)
			query := "CREATE TRIGGER refuse_wait BEFORE INSERT ON events WHEN NEW.kind='" + kind + "' BEGIN SELECT RAISE(ABORT, 'wait write refused'); END"
			if err := f.e.store.DB().Exec(query).Error; err != nil {
				t.Fatal(err)
			}
			if kind == KindWorkersCollectionDeclined {
				if err := f.e.ContinueTurn(f.thread.ID, false); err == nil {
					t.Fatal("failed durable decline was acknowledged")
				}
				if f.managers.Load() != 3 {
					t.Fatal("failed decline granted a manager request")
				}
				f.releaseWorker(0)
				if got := waitForTurn(t, f.e, f.turn.ID); got.Status != store.TurnError || f.managers.Load() != 3 {
					t.Fatalf("failed decline later granted automatic work: %+v manager=%d", got, f.managers.Load())
				}
				return
			}
			f.releaseWorker(0)
			if got := waitForTurn(t, f.e, f.turn.ID); got.Status != store.TurnError || f.managers.Load() != 3 {
				t.Fatalf("failed completion receipt granted work: %+v", got)
			}
		})
	}
	e := newTestEngine(t)
	th, _ := e.CreateThread("read failure", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	if err := e.store.DB().Callback().Query().Before("gorm:query").Register("wait_read_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "events" {
			tx.AddError(errors.New("wait read refused"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.workerCollectionWaitState(turn); err == nil {
		t.Fatal("failed read granted a completion state")
	}
}

func TestCompletionWaitChecksExactDurablePauseAndNeverClearsFinishedPending(t *testing.T) {
	w := swarm.PendingAgent{AgentID: "worker-1", Generation: 2, Running: true}
	p := workerCollectionPause{Reason: "workers_uncollected", WaitID: "wait-1", Extend: 2, Workers: []swarm.PendingAgent{w}, WorkerIDs: []string{w.AgentID}, AutomaticResume: true}
	body, _ := json.Marshal(p)
	finished := w
	finished.Running = false
	receipt := workerCollectionReceipt{p.WaitID, p.WorkerIDs, []swarm.PendingAgent{finished}}
	resume, _ := json.Marshal(receipt)
	pauseEvent := store.Event{AgentID: "manager", Kind: KindMaxIterations, Text: string(body)}
	for _, events := range [][]store.Event{
		{pauseEvent, {AgentID: "manager", Kind: KindWorkersCollectionResumed, Text: string(resume)}},
		{pauseEvent, {AgentID: "manager", Kind: KindMaxIterationsContinued}},
	} {
		paused, _, err := workerCollectionWaitEvents(events)
		if err != nil || paused != nil {
			t.Fatalf("tail completion/human decision resurrected old checkpoint pause: %+v %v", paused, err)
		}
	}
	for _, bad := range []workerCollectionReceipt{{"other", p.WorkerIDs, receipt.Workers}, {p.WaitID, p.WorkerIDs, []swarm.PendingAgent{{AgentID: w.AgentID, Generation: 3}}}, {p.WaitID, p.WorkerIDs, []swarm.PendingAgent{w}}} {
		body, _ := json.Marshal(bad)
		if _, used, err := workerCollectionWaitEvents([]store.Event{pauseEvent, {AgentID: "manager", Kind: KindWorkersCollectionResumed, Text: string(body)}}); err == nil || used {
			t.Fatal("mismatched resume granted a request")
		}
	}
	for _, bad := range []workerCollectionPause{{Reason: "workers_uncollected", Workers: p.Workers}, {Reason: p.Reason, WaitID: p.WaitID, Extend: 2, WorkerIDs: p.WorkerIDs, Workers: []swarm.PendingAgent{{AgentID: w.AgentID, Generation: 0}}}} {
		body, _ := json.Marshal(bad)
		if _, _, err := workerCollectionWaitEvents([]store.Event{{AgentID: "manager", Kind: KindMaxIterations, Text: string(body)}}); err == nil {
			t.Fatal("malformed durable pause was accepted")
		}
	}
	e := newTestEngine(t)
	th, _ := e.CreateThread("finished pending", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	if err := reg.PlantFinished(swarm.FinishedWorker{ID: w.AgentID, Generation: 2, Result: "Verified retained result."}); err != nil {
		t.Fatal(err)
	}
	paused, err := e.runtimeFor(th.ID).newWorkerCollectionPause(turn, reg.PendingAgents(), 3, 2)
	if err != nil || paused.AutomaticResume || len(reg.PendingAgents()) != 1 {
		t.Fatalf("finished debt was cleared or auto admitted: %+v %v", paused, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if _, _, err := e.runtimeFor(th.ID).waitForWorkerCollection(ctx, turn, reg, paused); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("finished pending incorrectly auto-resumed: %v", err)
	}
}

// Completing the exact pending generation may wake one collection attempt,
// but it is not a human Continue and does not renew the correction allowance.
func TestWorkerCompletionWakesOneCollectionAttemptWithoutHumanContinuation(t *testing.T) {
	f := newOrdinaryLifecycleFixture(t, "continue")
	f.start(t)
	f.awaitPause(t)
	f.releaseWorker(0)
	ordinaryEventually(t, func() bool {
		got, _ := f.e.store.GetTurn(f.turn.ID)
		return got != nil && got.Status != store.TurnRunning
	}, "completed worker left its manager indefinitely paused")
	got, _ := f.e.store.GetTurn(f.turn.ID)
	if got.Status != store.TurnDone || got.Final != ordinaryCollectedAnswer || f.managers.Load() != 5 || ordinaryCorrectionCount(t, f.e, f.turn.ID) != 1 {
		t.Fatalf("completion wake changed identity or correction allowance: turn=%+v manager=%d corrections=%d", got, f.managers.Load(), ordinaryCorrectionCount(t, f.e, f.turn.ID))
	}
	events, _ := f.e.store.ListTurnEvents(f.turn.ID)
	ready := 0
	for _, ev := range events {
		if ev.Kind == KindMaxIterationsContinued {
			t.Fatal("worker completion fabricated a human continuation")
		}
		if ev.Kind == "workers_collection_resumed" {
			var body struct {
				Workers []struct {
					AgentID    string `json:"agent_id"`
					Generation int    `json:"generation"`
				} `json:"workers"`
			}
			if json.Unmarshal([]byte(ev.Text), &body) != nil || len(body.Workers) != 1 || body.Workers[0].AgentID != f.workerIDs()[0] || body.Workers[0].Generation <= 0 {
				t.Fatalf("completion wake lost exact generation: %s", ev.Text)
			}
			ready++
		}
	}
	if ready != 1 {
		t.Fatalf("completion wake receipts=%d", ready)
	}
}
