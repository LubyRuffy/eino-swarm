package engine

import (
	"context"
	"errors"
	"slices"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

func TestLeftoverWorkerOwnershipUsesActualGenerationSourcesAndIgnoresSettledArchives(t *testing.T) {
	for _, tc := range []struct {
		name     string
		snapshot restoreSnapshot
		owners   []string
		invalid  bool
	}{
		{name: "empty"},
		{name: "actual spawn", snapshot: restoreSnapshot{events: []store.Event{{Kind: "spawned", AgentID: "worker", TurnID: "original"}}, running: []swarm.RestoredWorker{{ID: "worker"}}}, owners: []string{"original"}},
		{name: "finished unread", snapshot: restoreSnapshot{events: []store.Event{{Kind: "spawned", AgentID: "worker", TurnID: "original"}}, finished: []swarm.FinishedWorker{{ID: "worker"}}}, owners: []string{"original"}},
		{name: "collected archive", snapshot: restoreSnapshot{finished: []swarm.FinishedWorker{{ID: "old", Collected: true}}}},
		{name: "discarded archive", snapshot: restoreSnapshot{finished: []swarm.FinishedWorker{{ID: "old", CloseRequested: true}}}},
		{name: "unknown source", snapshot: restoreSnapshot{running: []swarm.RestoredWorker{{ID: "worker"}}}, invalid: true},
		{name: "manager is not a worker owner", snapshot: restoreSnapshot{events: []store.Event{{Kind: "spawned", AgentID: "manager", TurnID: "wrong"}}, running: []swarm.RestoredWorker{{ID: "manager"}}}, invalid: true},
		{name: "checkpoint owner", snapshot: restoreSnapshot{checkpoint: &restartCheckpoint{Workers: []swarm.WorkerCheckpoint{{ID: "worker"}}}, checkpointEvent: &store.Event{Seq: 8, TurnID: "captured"}, events: []store.Event{{Seq: 2, Kind: "spawned", AgentID: "worker", TurnID: "older"}, {Seq: 8, Kind: KindRestartCheckpoint}}, running: []swarm.RestoredWorker{{ID: "worker"}}}, owners: []string{"captured"}},
		{name: "new generation after checkpoint", snapshot: restoreSnapshot{checkpoint: &restartCheckpoint{Workers: []swarm.WorkerCheckpoint{{ID: "worker"}}}, checkpointEvent: &store.Event{Seq: 8, TurnID: "captured"}, events: []store.Event{{Seq: 8, Kind: KindRestartCheckpoint}, {Seq: 9, Kind: "spawned", AgentID: "worker", TurnID: "new-generation"}}, running: []swarm.RestoredWorker{{ID: "worker"}}}, owners: []string{"new-generation"}},
		{name: "checkpoint without identity", snapshot: restoreSnapshot{checkpoint: &restartCheckpoint{}}, invalid: true},
		{name: "multiple owners need separate stored chain proof", snapshot: restoreSnapshot{events: []store.Event{{Kind: "spawned", AgentID: "one", TurnID: "first"}, {Kind: "spawned", AgentID: "two", TurnID: "second"}}, running: []swarm.RestoredWorker{{ID: "one"}}, finished: []swarm.FinishedWorker{{ID: "two"}}}, owners: []string{"first", "second"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owners, err := leftoverWorkerOwners(&tc.snapshot)
			if errors.Is(err, store.ErrHistoryRegistryInheritanceInvalid) != tc.invalid || !tc.invalid && (err != nil || !slices.Equal(owners, tc.owners)) {
				t.Fatalf("owners=%q err=%v, want=%q invalid=%t", owners, err, tc.owners, tc.invalid)
			}
		})
	}
}

func TestPreparingRecoveryStatePersistsItsOwnerBeforeInstallingAnyWorkers(t *testing.T) {
	e := newTestEngine(t)
	thread, err := e.CreateThread("Private recovery owner", "", "")
	if err != nil {
		t.Fatal(err)
	}
	first := mustStoredTurn(t, e, thread.ID, store.Turn{})
	plantFinishedWorker(t, e, thread.ID, first.ID, "reader", "reader", "Original unread result.")
	historyReadEvent(e, first, "reader")
	second := mustStoredTurn(t, e, thread.ID, store.Turn{})
	rt := e.runtimeFor(thread.ID)
	plan, err := rt.prepareLeftoverWorkers(t.Context(), second)
	if err != nil || len(plan.finished) != 1 || len(plan.running) != 0 {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	if rt.running || rt.reg != nil || rt.parked != nil || rt.restore != nil || rt.planted != nil {
		t.Fatal("preparation changed runtime ownership or installed worker state")
	}
	if mode, err := e.store.TurnHasHistoryQuery(thread.ID, second.ID); err != nil || !mode {
		t.Fatalf("source edge not durable before runtime claim: %t %v", mode, err)
	}
	for range 2 {
		if _, err := rt.prepareLeftoverWorkers(t.Context(), second); err != nil {
			t.Fatalf("identical recovery preparation was not idempotent: %v", err)
		}
	}
	messages := rt.applyLeftoverWorkers(plan, false, true, 0, []adk.Message{schema.UserMessage("Continue the existing work.")})
	_, installed := rt.takeWorkerRestore()
	if len(installed) != 1 || installed[0].ID != "reader" || !slices.Contains(knownWorkerIDsInMessages(messages), "reader") {
		t.Fatalf("admitted recovery state was not installed: %+v %v", installed, messages)
	}
}

func TestRecoveryPreparationFailsClosedWithoutMutatingRuntime(t *testing.T) {
	e := newTestEngine(t)
	thread, err := e.CreateThread("Refused recovery", "", "")
	if err != nil {
		t.Fatal(err)
	}
	first := mustStoredTurn(t, e, thread.ID, store.Turn{})
	second := mustStoredTurn(t, e, thread.ID, store.Turn{})
	rt := e.runtimeFor(thread.ID)
	snapshot := &restoreSnapshot{threadID: thread.ID, events: []store.Event{{Kind: "spawned", AgentID: "reader", TurnID: first.ID}}, running: []swarm.RestoredWorker{{ID: "reader"}}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := rt.prepareLeftoverWorkers(ctx, second, snapshot); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled preparation=%v", err)
	}
	var absent *runtime
	if _, err := absent.prepareLeftoverWorkers(t.Context(), second, snapshot); !errors.Is(err, store.ErrHistoryRegistryInheritanceInvalid) {
		t.Fatalf("missing runtime=%v", err)
	}
	if _, err := rt.prepareLeftoverWorkers(t.Context(), nil, snapshot); !errors.Is(err, store.ErrHistoryRegistryInheritanceInvalid) {
		t.Fatalf("missing turn=%v", err)
	}
	if _, err := rt.prepareLeftoverWorkers(t.Context(), second, &restoreSnapshot{threadID: thread.ID, running: []swarm.RestoredWorker{{ID: "unknown"}}}); !errors.Is(err, store.ErrHistoryRegistryInheritanceInvalid) {
		t.Fatalf("unknown owner=%v", err)
	}
	if _, err := rt.prepareLeftoverWorkers(t.Context(), first, &restoreSnapshot{threadID: thread.ID, events: snapshot.events, running: snapshot.running}); err != nil {
		t.Fatalf("same-turn cold recovery needs no self edge: %v", err)
	}
	if err := e.store.DB().Callback().Create().Before("gorm:create").Register("refuse_history_recovery_edge", func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*store.Event); ok && event.Kind == "history_registry_inherited" {
			_ = tx.AddError(errors.New("private recovery edge storage unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.prepareLeftoverWorkers(t.Context(), second, snapshot); err == nil {
		t.Fatal("recovery continued after its durable source edge failed")
	}
	if rt.running || rt.reg != nil || rt.parked != nil || rt.restore != nil || rt.planted != nil {
		t.Fatal("failed preparation installed state")
	}
	if mode, err := e.store.TurnHasHistoryQuery(thread.ID, second.ID); err != nil || mode {
		t.Fatalf("failed preparation manufactured mode: %t %v", mode, err)
	}
	_ = e.store.Close()
	if _, err := rt.prepareLeftoverWorkers(t.Context(), second); err == nil {
		t.Fatal("unreadable recovery state was ignored")
	}
}
