package engine

import (
	"encoding/json"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A replacement may restart the model request, but it cannot renew the
// manager's correction allowance or silently settle the worker generation.
func TestOrdinaryCollectionBudgetAndWorkerIdentitySurviveActualRestart(t *testing.T) {
	f := newOrdinaryLifecycleFixture(t, "restart")
	f.start(t)
	ordinaryAwait(t, f.corrected, "manager never entered its one automatic correction")
	before := f.pending()
	if len(before) != 1 || !before[0].Running || before[0].Generation <= 0 {
		t.Fatalf("initial worker did not own one unsettled generation: %+v", before)
	}
	if got := ordinaryCorrectionCount(t, f.e, f.turn.ID); got != 1 {
		t.Fatalf("the correcting HTTP request preceded its durable receipt: %d", got)
	}
	next := coldRestartReplayEngine(t, f.e)
	f.e = next
	if restored, err := next.ResumeOrphanedTurns(); err != nil || restored != 1 {
		t.Fatalf("replacement did not resume the original ordinary turn: %d %v", restored, err)
	}
	ordinaryAwait(t, f.resumed, "replacement did not reach the worker's actual HTTP adapter")
	ordinaryEventually(t, func() bool {
		if !next.Status(f.thread.ID).AwaitingContinue {
			return false
		}
		events, _ := next.store.ListTurnEvents(f.turn.ID)
		for _, event := range events {
			if event.Kind == KindMaxIterations {
				return true
			}
		}
		return false
	}, "replacement granted another automatic correction instead of pausing")
	turn, _ := next.store.GetTurn(f.turn.ID)
	pending := f.pending()
	if turn == nil || turn.Status != store.TurnRunning || len(pending) != 1 || pending[0].AgentID != before[0].AgentID || pending[0].Generation != before[0].Generation || !pending[0].Running {
		t.Fatalf("replacement changed the turn or settled its pending generation: turn=%+v pending=%+v before=%+v", turn, pending, before)
	}
	if f.managers.Load() != 4 || f.workers.Load() != 2 || ordinaryCorrectionCount(t, next, f.turn.ID) != 1 {
		t.Fatalf("restart reset the budget or spawned a replacement identity: manager=%d workerHTTP=%d corrections=%d", f.managers.Load(), f.workers.Load(), ordinaryCorrectionCount(t, next, f.turn.ID))
	}
	events, _ := next.store.ListTurnEvents(f.turn.ID)
	spawned, spawnCalls := 0, 0
	for _, event := range events {
		if event.Kind == swarm.NotifyToolCall.String() && isManagerAgent(event.AgentID) {
			name, _ := splitToolCallText(event.Text)
			if name == "spawn_agent" {
				spawnCalls++
			}
		}
		if event.Kind == swarm.NotifySpawned.String() {
			spawned++
			if event.AgentID != before[0].AgentID {
				t.Fatalf("restart fabricated a different worker: %q", event.AgentID)
			}
		}
	}
	// Restore's existing lifecycle hook reports the real restarted request;
	// it must not invent another manager spawn_agent tool call or identity.
	if spawned != 2 || spawnCalls != 1 {
		t.Fatalf("recovery changed the actual worker-creation facts: lifecycle=%d spawnCalls=%d", spawned, spawnCalls)
	}
	f.assertWithheld(t)
	select {
	case <-f.resumedCancelled:
		t.Fatal("manager cap cancelled the worker's restored HTTP request")
	default:
	}
	f.assertStopped(t, 0)
	ordinaryAwait(t, f.resumedCancelled, "Stop did not cancel the replacement's real worker HTTP request")
}

func ordinaryCorrectionCount(t *testing.T, e *Engine, turnID string) int {
	t.Helper()
	events, err := e.store.ListTurnEvents(turnID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range events {
		var body struct{ Reason string }
		if event.Kind == KindModelRetry && isManagerAgent(event.AgentID) && json.Unmarshal([]byte(event.Text), &body) == nil && body.Reason == "workers_uncollected" {
			count++
		}
	}
	return count
}

// Restore emits an actual same-ID lifecycle event. That event must preserve
// the generation identity returned by the subsequent real wait_agents call.
func TestOrdinaryRestoredWorkerCollectionSurvivesAnotherColdReplay(t *testing.T) {
	f := newOrdinaryLifecycleFixture(t, "restart-collect")
	f.start(t)
	ordinaryAwait(t, f.corrected, "manager never entered the pre-restart correction")
	pending := f.pending()
	if len(pending) != 1 {
		t.Fatalf("missing initial pending worker: %+v", pending)
	}
	original := pending[0]
	next := coldRestartReplayEngine(t, f.e)
	f.e = next
	if n, err := next.ResumeOrphanedTurns(); err != nil || n != 1 {
		t.Fatalf("could not resume the actual turn: %d %v", n, err)
	}
	ordinaryAwait(t, f.resumed, "restored worker did not reach its actual HTTP request")
	f.releaseWorker(0)
	finished := waitForTurn(t, next, f.turn.ID)
	if finished.Status != store.TurnDone || finished.Final != ordinaryCollectedAnswer || f.managers.Load() != 5 || f.workers.Load() != 2 {
		t.Fatalf("actual restored worker was not collected: turn=%+v manager=%d workerHTTP=%d", finished, f.managers.Load(), f.workers.Load())
	}
	assertCollected := func(e *Engine) {
		t.Helper()
		running, done := e.workersFromThread(f.thread.ID)
		if len(running) != 0 || len(done) != 1 {
			t.Fatalf("actual Restore+wait changed the roster: running=%d done=%d", len(running), len(done))
		}
		if got := done[0]; got.ID != original.AgentID || got.Generation != original.Generation || !got.Collected || got.Result != f.result(0) {
			t.Fatalf("actual Restore+wait failed to settle its exact generation: id=%q generation=%d collected=%v want_id=%q want_generation=%d", got.ID, got.Generation, got.Collected, original.AgentID, original.Generation)
		}
	}
	assertCollected(next)
	last := coldRestartReplayEngine(t, next)
	f.e = last
	assertCollected(last)
	if n, err := last.ResumeOrphanedTurns(); err != nil || n != 0 || f.managers.Load() != 5 || ordinaryCorrectionCount(t, last, f.turn.ID) != 1 {
		t.Fatalf("completed collection revived after cold replay: restored=%d err=%v manager=%d", n, err, f.managers.Load())
	}
}
