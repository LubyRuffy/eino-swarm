package provider

import (
	"context"
	"fmt"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// WorkerModel resolves a selection once and builds its per-agent factory. The
// returned binding can be persisted without exposing any endpoint credentials.
func (p *Pool) WorkerModel(ctx context.Context, id, modelName, effort string, rec Recorder) (swarm.ModelBinding, swarm.ModelBuilder, error) {
	prov, err := p.ResolveModel(id, modelName)
	if err != nil {
		return swarm.ModelBinding{}, nil, err
	}
	binding := swarm.ModelBinding{ProviderID: prov.ID, Model: strings.TrimSpace(prov.Model), ReasoningEffort: config.NormalizeReasoning(effort)}
	builder, err := p.ModelBuilder(ctx, binding.ProviderID, binding.Model, binding.ReasoningEffort, rec)
	if err != nil {
		return swarm.ModelBinding{}, nil, err
	}
	return binding, builder, nil
}

// RestoreWorkerModel rebuilds the concrete pinned model, never the current
// default or a possibly changed catalog reference. Missing providers fail closed.
func (p *Pool) RestoreWorkerModel(ctx context.Context, binding swarm.ModelBinding, rec Recorder) (swarm.ModelBuilder, error) {
	if strings.TrimSpace(binding.ProviderID) == "" || strings.TrimSpace(binding.Model) == "" {
		return nil, fmt.Errorf("provider: worker model binding requires provider_id and model")
	}
	if config.NormalizeReasoning(binding.ReasoningEffort) != binding.ReasoningEffort {
		return nil, fmt.Errorf("provider: invalid bound worker reasoning effort %q", binding.ReasoningEffort)
	}
	return p.ModelBuilder(ctx, binding.ProviderID, binding.Model, binding.ReasoningEffort, rec)
}
