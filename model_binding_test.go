package swarm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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
	reg.SelectWorkerModel = func(_ context.Context, ref string) (ModelBinding, ModelBuilder, error) {
		return ModelBinding{Ref: ref, ProviderID: "provider", Model: ref}, finishedBindingBuilder, nil
	}
	reg.RestoreWorkerModel = func(context.Context, ModelBinding) (ModelBuilder, error) { return finishedBindingBuilder, nil }
	return reg
}

func TestWorkerModelSelectionErrorsHaveNoWorkerSideEffects(t *testing.T) {
	for _, mode := range []string{"unknown", "malformed", "missing-builder"} {
		t.Run(mode, func(t *testing.T) {
			reg := boundTestRegistry(t)
			reg.SelectWorkerModel = func(context.Context, string) (ModelBinding, ModelBuilder, error) {
				switch mode {
				case "unknown":
					return ModelBinding{}, nil, errors.New("not in catalog")
				case "malformed":
					return ModelBinding{ProviderID: "provider"}, finishedBindingBuilder, nil
				default:
					return ModelBinding{ProviderID: "provider", Model: "model"}, nil, nil
				}
			}
			rec := &recorder{}
			reg.setSink(rec.cb())
			out, err := reg.spawn(context.Background(), `{"role":"analysis","task":"inspect input","model_ref":"selected"}`)
			if err != nil || !strings.Contains(out, `"error"`) {
				t.Fatalf("refusal out=%s err=%v", out, err)
			}
			if len(reg.SnapshotWorkers()) != 0 || reg.seq != 0 || len(rec.all()) != 0 {
				t.Fatal("failed model choice occupied a worker or emitted lifecycle events")
			}
		})
	}
}

func TestExistingWorkerModelConflictDoesNotSteerOrResume(t *testing.T) {
	reg := boundTestRegistry(t)
	var restoreError error
	reg.RestoreWorkerModel = func(context.Context, ModelBinding) (ModelBuilder, error) { return finishedBindingBuilder, restoreError }
	probe := &workerContextProbeModel{seen: make(chan context.Context, 1), release: make(chan struct{})}
	reg.SelectWorkerModel = func(_ context.Context, ref string) (ModelBinding, ModelBuilder, error) {
		return ModelBinding{Ref: ref, ProviderID: "provider", Model: ref}, func(string, string) model.BaseChatModel { return probe }, nil
	}
	out, err := reg.spawn(context.Background(), `{"role":"analysis","task":"inspect input","model_ref":"one"}`)
	if err != nil || strings.Contains(out, `"error"`) {
		t.Fatalf("spawn %s %v", out, err)
	}
	select {
	case <-probe.seen:
	case <-time.After(3 * time.Second):
		t.Fatal("worker never started")
	}
	h, ok := reg.get("analysis-1")
	if !ok {
		t.Fatal("missing worker")
	}
	refuse := func() {
		t.Helper()
		out, err := reg.spawn(context.Background(), `{"role":"analysis","task":"must not arrive","model_ref":"two"}`)
		if err != nil || !strings.Contains(out, "conflicts") {
			t.Fatalf("conflicting spawn out=%s err=%v", out, err)
		}
		cp := reg.SnapshotWorkers()[0]
		if cp.Generation != 1 || len(cp.Inbox) != 0 || cp.ModelBinding.Model != "one" {
			t.Fatalf("conflict mutated worker %+v", cp)
		}
	}
	refuse()
	close(probe.release)
	<-h.Done()
	refuse()
	reg.Stats() // Conflict must also preserve the archived generation.
	refuse()
	restoreError = errors.New("provider removed")
	out, err = reg.resume(context.Background(), `{"agent_id":"analysis-1","task":"must not create a generation"}`)
	if err != nil || !strings.Contains(out, "provider removed") {
		t.Fatalf("resume out=%s err=%v", out, err)
	}
	if cp := reg.SnapshotWorkers()[0]; cp.Generation != 1 || !cp.Finished {
		t.Fatalf("failed resume mutated state %+v", cp)
	}
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

func TestModelReferenceSchemaOnlyAppearsWhenHostEnablesSelection(t *testing.T) {
	reg := NewRegistry()
	defer reg.Close()
	info, _ := reg.Tools()[0].Info(context.Background())
	if strings.Contains(info.Desc, "model_ref") {
		t.Fatal("default tool advertises unavailable selection")
	}
	params, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := params.Properties.Get("model_ref"); ok {
		t.Fatal("default schema offers model_ref")
	}
	reg.SelectWorkerModel = func(context.Context, string) (ModelBinding, ModelBuilder, error) { return ModelBinding{}, nil, nil }
	info, _ = reg.Tools()[0].Info(context.Background())
	params, err = info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := params.Properties.Get("model_ref"); !ok {
		t.Fatal("enabled schema hides model_ref")
	}
}
