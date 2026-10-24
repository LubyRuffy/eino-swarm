package engine

import (
	"errors"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

// Process replacement may not turn a repeated premature completion into an
// unlimited model loop. Only the human's continuation grants another attempt.
func TestWorkerCollectionCorrectionRequiresItsExactDurableBudget(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("collection allowance", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	other := mustStoredTurn(t, e, th.ID, store.Turn{})
	for _, ev := range []store.Event{
		{TurnID: other.ID, Kind: KindModelRetry, AgentID: "manager", Text: `{"reason":"workers_uncollected"}`},
		{TurnID: turn.ID, Kind: KindModelRetry, AgentID: "worker", Text: `{"reason":"workers_uncollected"}`},
		{TurnID: turn.ID, Kind: KindModelRetry, AgentID: "manager", Text: `{"reason":"schedule_claim_unverified"}`},
		{TurnID: turn.ID, Kind: KindModelRetry, AgentID: "manager", Text: `{"reason":`},
	} {
		ev.ThreadID = th.ID
		if err := e.store.AppendEvent(&ev); err != nil {
			t.Fatal(err)
		}
	}
	if used, err := e.workerCollectionContinued(turn); err != nil || used {
		t.Fatalf("unrelated receipt consumed allowance: %v %v", used, err)
	}
	rt := e.runtimeFor(th.ID)
	if err := rt.recordWorkerCollectionRetry(turn, []string{"worker-2"}); err != nil {
		t.Fatal(err)
	}
	if used, err := e.workerCollectionContinued(turn); err != nil || !used {
		t.Fatalf("durable correction missing: %v %v", used, err)
	}
	if err := e.store.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindMaxIterationsContinued, AgentID: "worker"}); err != nil {
		t.Fatal(err)
	}
	if used, _ := e.workerCollectionContinued(turn); !used {
		t.Fatal("a worker granted manager continuation")
	}
	if err := e.store.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindMaxIterationsContinued, AgentID: "manager"}); err != nil {
		t.Fatal(err)
	}
	if used, err := e.workerCollectionContinued(turn); err != nil || used {
		t.Fatalf("human continuation did not renew allowance: %v %v", used, err)
	}
	e.recordMu.Lock()
	e.droppedTurns[other.ID] = struct{}{}
	e.recordMu.Unlock()
	if err := rt.recordWorkerCollectionRetry(other, []string{"worker-2"}); err == nil {
		t.Fatal("deleted turn accepted a receipt")
	}
}

func TestWorkerCollectionBudgetStorageFailureCannotGrantARequest(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("collection persistence", "", "")
	turn := mustStoredTurn(t, e, th.ID, store.Turn{})
	cause := errors.New("collection allowance is unreadable")
	name := "test:unreadable-collection-allowance"
	if err := e.store.DB().Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "events" {
			tx.AddError(cause)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if used, err := e.workerCollectionContinued(turn); used || !errors.Is(err, cause) {
		t.Fatalf("lost read cause or granted allowance: %v %v", used, err)
	}
	if err := e.store.DB().Callback().Query().Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := e.store.DB().Exec("CREATE TRIGGER refuse_worker_correction BEFORE INSERT ON events WHEN NEW.kind='model_retry' BEGIN SELECT RAISE(ABORT, 'collection receipt unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := e.runtimeFor(th.ID).recordWorkerCollectionRetry(turn, []string{"worker-2"}); err == nil {
		t.Fatal("failed write granted a correction")
	}
	if used, err := e.workerCollectionContinued(turn); err != nil || used {
		t.Fatalf("failed receipt consumed budget: %v %v", used, err)
	}
}

func TestAnExecutableOwnerMayStillParkItsUncollectedWorker(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("owned collection", "", "")
	reg, _ := ownedBlockingRegistry(t)
	rt := e.runtimeFor(th.ID)
	if ids := rt.uncollectedWorkerIDs(reg); len(ids) != 1 {
		t.Fatalf("ordinary worker was not pending: %v", ids)
	}
	wake := mustCreateWake(t, e, th.ID)
	if ids := rt.uncollectedWorkerIDs(reg); len(ids) != 0 {
		t.Fatalf("existing active wake lost parking: %v", ids)
	}
	if _, err := e.store.CancelSchedule(wake.ID); err != nil {
		t.Fatal(err)
	}
	if ids := rt.uncollectedWorkerIDs(reg); len(ids) != 1 {
		t.Fatalf("last owner revocation bypassed collection: %v", ids)
	}
}
