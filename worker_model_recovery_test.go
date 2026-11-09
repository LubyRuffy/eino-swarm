package swarm

import (
	"context"
	"testing"
)

func TestExplicitRecoveryKeepsCompletedResultHistoryUntilActualResume(t *testing.T) {
	reg := boundTestRegistry(t)
	original := ModelBinding{ProviderID: "removed", Model: "original"}
	target := ModelBinding{ProviderID: "available", Model: "replacement"}
	cfg := reg.WorkerConfiguration()
	cfg.RecoveredModels = map[string]ModelBinding{"removed": target}
	reg.ConfigureWorkers(cfg)
	// The configured snapshot must own its map; caller edits cannot retarget it.
	cfg.RecoveredModels["removed"] = original
	if err := reg.PlantFinished(FinishedWorker{ID: "worker-1", Role: "worker", Generation: 1, Result: "Original evidence", ModelBinding: &original}); err != nil {
		t.Fatal(err)
	}
	before := reg.SnapshotWorkers()
	if len(before) != 1 || *before[0].ModelBinding != original || before[0].Result != "Original evidence" {
		t.Fatalf("history rewritten: %+v", before)
	}
	h, err := reg.Resume(context.Background(), "worker-1", "Continue the task", nil)
	if err != nil {
		t.Fatal(err)
	}
	<-h.Done()
	after := reg.SnapshotWorkers()
	if len(after) != 1 || *after[0].ModelBinding != target || after[0].ID != "worker-1" || after[0].Generation != 2 {
		t.Fatalf("actual resumed binding: %+v", after)
	}
	if original.ProviderID != "removed" {
		t.Fatal("input binding mutated")
	}
	if cfg.recoveredBinding(nil) != nil {
		t.Fatal("legacy nil binding fabricated")
	}
	if got := (WorkerConfig{}).recoveredBinding(&original); *got != original || got == &original {
		t.Fatal("ordinary pinned binding changed or shared")
	}
}
