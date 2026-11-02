package provider

import (
	"context"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/model"
)

func TestWorkerModelFreezesSelectionAndRestorationIgnoresCurrentDefault(t *testing.T) {
	cfg := configFor(t, true)
	pool := New(cfg)
	var constructed []string
	pool.build = func(_ context.Context, p config.Provider) (model.BaseChatModel, error) {
		constructed = append(constructed, p.Model)
		return &fakeModel{}, nil
	}
	binding, builder, err := pool.WorkerModel(context.Background(), "", "selected-model", "high", nil)
	if err != nil {
		t.Fatal(err)
	}
	if binding.ProviderID != cfg.Models.Default || binding.Model != "selected-model" || binding.ReasoningEffort != "high" || builder == nil {
		t.Fatalf("selection=%+v builder=%v", binding, builder != nil)
	}
	cfg.Models.Providers[0].Model = "changed-default"
	pool.Invalidate()
	if _, err := pool.RestoreWorkerModel(context.Background(), binding, nil); err != nil {
		t.Fatal(err)
	}
	if len(constructed) != 2 || constructed[0] != "selected-model" || constructed[1] != "selected-model" {
		t.Fatalf("bound worker followed latest default: %v", constructed)
	}
}

func TestWorkerModelRejectsUnavailableAndMalformedSelection(t *testing.T) {
	pool := New(configFor(t, false))
	if _, _, err := pool.WorkerModel(context.Background(), "missing", "model", "", nil); err == nil {
		t.Fatal("unknown provider selected")
	}
	if _, _, err := pool.WorkerModel(context.Background(), "", "model", "", nil); err == nil {
		t.Fatal("unconfigured endpoint selected")
	}
	for _, binding := range []swarm.ModelBinding{
		{},
		{ProviderID: "provider"},
		{Model: "model"},
		{ProviderID: "provider", Model: "model", ReasoningEffort: "not-a-supported-effort"},
		{ProviderID: "missing", Model: "model"},
	} {
		if _, err := pool.RestoreWorkerModel(context.Background(), binding, nil); err == nil {
			t.Fatalf("restored malformed/unavailable binding %+v", binding)
		}
	}
}
