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

func (r *Registry) selectWorkerModel(ctx context.Context, ref string) (*ModelBinding, ModelBuilder, error) {
	config := r.workerConfigurationForContext(ctx)
	if config.SelectWorkerModel == nil {
		if ref != "" {
			return nil, nil, fmt.Errorf("spawn_agent: model_ref selection is not enabled")
		}
		return nil, config.ModelBuilder, nil
	}
	binding, builder, err := config.SelectWorkerModel(ctx, ref)
	if err != nil {
		return nil, nil, fmt.Errorf("spawn_agent: select worker model: %w", err)
	}
	if err := validateModelBinding(binding); err != nil {
		return nil, nil, err
	}
	if builder == nil {
		return nil, nil, fmt.Errorf("spawn_agent: selected worker model has no builder")
	}
	return &binding, builder, nil
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

// A same-role spawn is steering or resumption, not permission to change that
// worker's model. Resolve an explicit reference before touching its state.
func checkWorkerModel(existing, requested *ModelBinding) error {
	if existing == nil || requested == nil || *existing != *requested {
		return fmt.Errorf("spawn_agent: model_ref conflicts with the existing worker model; use a distinct role for a different model")
	}
	return nil
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
