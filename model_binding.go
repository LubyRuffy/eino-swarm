package swarm

import (
	"context"
	"fmt"
	"strings"
)

// ModelBinding records the concrete model selected for a worker. It contains
// no credentials. Ref is provenance; restoration uses the concrete fields.
type ModelBinding struct {
	Ref             string `json:"ref,omitempty"`
	ProviderID      string `json:"provider_id"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

func cloneModelBinding(binding *ModelBinding) *ModelBinding {
	if binding == nil {
		return nil
	}
	copy := *binding
	return &copy
}

func validateModelBinding(binding ModelBinding) error {
	if strings.TrimSpace(binding.ProviderID) == "" || strings.TrimSpace(binding.Model) == "" {
		return fmt.Errorf("worker model binding requires provider_id and model")
	}
	return nil
}

func (r *Registry) restoreWorkerModel(ctx context.Context, binding *ModelBinding, fallback ModelBuilder) (ModelBuilder, error) {
	config := r.workerConfigurationForContext(ctx)
	if binding == nil {
		if fallback == nil {
			fallback = config.ModelBuilder
		}
		return fallback, nil
	}
	if err := validateModelBinding(*binding); err != nil {
		return nil, err
	}
	if config.RestoreWorkerModel == nil {
		return nil, fmt.Errorf("worker model binding cannot be restored: no model restorer is configured")
	}
	builder, err := config.RestoreWorkerModel(ctx, *binding)
	if err != nil {
		return nil, fmt.Errorf("restore worker model: %w", err)
	}
	if builder == nil {
		return nil, fmt.Errorf("restored worker model has no builder")
	}
	return builder, nil
}

func (r *Registry) workerModelBinding(id string) *ModelBinding {
	r.mu.Lock()
	h := r.agents[id]
	r.mu.Unlock()
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return cloneModelBinding(h.modelBinding)
}
