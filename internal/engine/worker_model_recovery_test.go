package engine

import (
	"context"
	"errors"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func recoveryFixture(t *testing.T) (*Engine, *store.Thread, *store.Turn) {
	t.Helper()
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	th, err := e.CreateThread("Recovery test", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, ProviderID: "removed", Model: "old"}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.store.FinishTurn(turn.ID, store.TurnDone, "", ""); err != nil {
		t.Fatal(err)
	}
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, AgentID: "worker-1", Role: "worker", Kind: swarm.NotifySpawned.String(), ConfigTag: turn.ID, WorkerGeneration: 1, Text: "Inspect progress", ModelBinding: &swarm.ModelBinding{ProviderID: "removed", Model: "old"}})
	return e, th, turn
}

func TestRecoveryOnlyChangesUnavailableModelsAndFlattensLaterRepairs(t *testing.T) {
	e, th, source := recoveryFixture(t)
	// Healthy per-worker selection survives even when its original manager is gone.
	healthy := &swarm.ModelBinding{ProviderID: th.ProviderID, Model: "healthy", ReasoningEffort: "high"}
	e.record(store.Event{ThreadID: th.ID, TurnID: source.ID, AgentID: "healthy-2", Role: "healthy", Kind: swarm.NotifySpawned.String(), ConfigTag: source.ID, WorkerGeneration: 1, Text: "Inspect progress", ModelBinding: healthy})
	if n, err := e.RecoverWorkerModels(t.Context(), th.ID, th.ProviderID, "chosen"); err != nil || n != 1 {
		t.Fatalf("repair=%d %v", n, err)
	}
	running, finished, err := e.readWorkersFromThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.restorableWorkerArchive(t.Context(), th.ID, running, finished); err != nil {
		t.Fatal(err)
	}
	for _, w := range running {
		if w.ID == "healthy-2" && *w.ModelBinding != *healthy {
			t.Fatalf("healthy worker changed: %+v", w)
		}
		if w.ID == "worker-1" && w.ModelBinding.Model != "old" {
			t.Fatal("historical worker model changed before execution")
		}
	}
	if _, err := e.RecoverWorkerModels(t.Context(), th.ID, th.ProviderID, "other"); err == nil {
		t.Fatal("healthy model silently retargeted")
	}
	// A later removed target must update both original events and newer checkpoints.
	saved, err := e.store.GetTurn(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	saved.WorkerModelRecovery["removed"] = swarm.ModelBinding{ProviderID: "removed-again", Model: "middle"}
	if err := e.store.SaveWorkerModelRecovery(t.Context(), th.ID, "", map[string]map[string]swarm.ModelBinding{source.ID: saved.WorkerModelRecovery}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.RecoverWorkerModels(t.Context(), th.ID, th.ProviderID, "latest"); err != nil {
		t.Fatal(err)
	}
	saved, err = e.store.GetTurn(source.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"removed", "removed-again"} {
		if saved.WorkerModelRecovery[id].Model != "latest" {
			t.Fatal("recovery chain was not flattened")
		}
	}

}

func TestRecoveryRefusesBusyForeignMissingAndFailedState(t *testing.T) {
	for _, mode := range []string{"blank", "bad-target", "busy", "parked", "cancelled", "missing-thread", "foreign", "moved", "read-failure", "write-failure", "restart", "legacy", "settled"} {
		t.Run(mode, func(t *testing.T) {
			e, th, source := recoveryFixture(t)
			ctx := t.Context()
			id, chosen := th.ID, th.ProviderID
			rt := e.runtimeFor(th.ID)
			switch mode {
			case "blank":
				chosen = ""
			case "bad-target":
				chosen = "unknown-target"
			case "busy":
				rt.running = true
				defer func() { rt.running = false }()
			case "parked":
				rt.parked = swarm.NewRegistry()
				defer func() { rt.parked.Close(); rt.parked = nil }()
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "missing-thread":
				id = "missing"
			case "foreign":
				defer func() { _ = e.store.DB().Model(source).Update("thread_id", th.ID).Error }()
				if err := e.store.DB().Model(source).Update("thread_id", "foreign").Error; err != nil {
					t.Fatal(err)
				}
			case "moved":
				if err := e.store.DB().Model(source).Update("project_id", "other").Error; err != nil {
					t.Fatal(err)
				}
			case "read-failure":
				if err := e.store.DB().Exec("DROP TABLE events").Error; err != nil {
					t.Fatal(err)
				}
			case "write-failure":
				if err := e.store.DB().Exec("CREATE TRIGGER refuse_recovery BEFORE UPDATE OF worker_model_recovery ON turns BEGIN SELECT RAISE(ABORT,'fixture'); END").Error; err != nil {
					t.Fatal(err)
				}
			case "restart":
				e.restart.freezing = true
				defer func() { e.restart.freezing = false }()
			case "legacy":
				if err := e.store.DB().Model(&store.Event{}).Where("thread_id = ?", th.ID).Update("config_tag", "").Error; err != nil {
					t.Fatal(err)
				}
			case "settled":
				appendParkedCheckpoint(t, e, th.ID, source.ID, restartCheckpoint{Version: 1, Workers: []swarm.WorkerCheckpoint{{ID: "worker-1", Role: "worker", ConfigTag: source.ID, Generation: 1, Finished: true, Collected: true}}})
			}
			_, err := e.RecoverWorkerModels(ctx, id, chosen, "chosen")
			if err == nil {
				t.Fatal("unsafe recovery accepted")
			}
			if mode == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
		})
	}
}
