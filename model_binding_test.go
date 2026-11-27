package swarm

import (
	"context"
	"errors"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func finishedBindingBuilder(string, string) model.BaseChatModel {
	return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
		func(int, []*schema.Message) *schema.Message { return schema.AssistantMessage("complete", nil) },
	}}
}

func boundTestRegistry(t *testing.T) *Registry {
	t.Helper()
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	reg.ModelBuilder = finishedBindingBuilder
	reg.RestoreWorkerModel = func(context.Context, ModelBinding) (ModelBuilder, error) { return finishedBindingBuilder, nil }
	return reg
}

func TestBoundWorkerRestoreRefusesBeforeClaimAndClonesItsBinding(t *testing.T) {
	binding := &ModelBinding{Ref: "chosen", ProviderID: "provider", Model: "model"}
	for _, mode := range []string{"missing-restorer", "malformed", "failure", "missing-builder"} {
		t.Run(mode, func(t *testing.T) {
			reg := boundTestRegistry(t)
			worker := RestoredWorker{ID: "analysis-7", Role: "analysis", Task: "recover work", ModelBinding: cloneModelBinding(binding)}
			switch mode {
			case "missing-restorer":
				reg.RestoreWorkerModel = nil
			case "malformed":
				worker.ModelBinding.Model = ""
			case "failure":
				reg.RestoreWorkerModel = func(context.Context, ModelBinding) (ModelBuilder, error) { return nil, errors.New("unavailable") }
			case "missing-builder":
				reg.RestoreWorkerModel = func(context.Context, ModelBinding) (ModelBuilder, error) { return nil, nil }
			}
			if _, err := reg.Restore(context.Background(), worker, nil); err == nil {
				t.Fatal("invalid restoration accepted")
			}
			if reg.seq != 0 || len(reg.SnapshotWorkers()) != 0 {
				t.Fatal("failed restoration claimed worker")
			}
		})
	}
	reg := boundTestRegistry(t)
	if err := reg.PlantFinished(FinishedWorker{ID: "analysis-7", Role: "analysis", Result: "previous result", ModelBinding: binding}); err != nil {
		t.Fatal(err)
	}
	binding.Model = "caller changed"
	before := reg.SnapshotWorkers()[0]
	if before.ModelBinding.Model != "model" {
		t.Fatal("caller mutated planted binding")
	}
	before.ModelBinding.Model = "snapshot changed"
	if reg.SnapshotWorkers()[0].ModelBinding.Model != "model" {
		t.Fatal("snapshot mutated live binding")
	}
	h, err := reg.Resume(context.Background(), "analysis-7", "continue work", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	<-h.Done()
	if cp := reg.SnapshotWorkers()[0]; cp.ModelBinding.Model != "model" || cp.Generation != 2 {
		t.Fatalf("planted resume %+v", cp)
	}
	if reg.workerModelBinding("not-present") != nil {
		t.Fatal("unknown worker has model binding")
	}
}
