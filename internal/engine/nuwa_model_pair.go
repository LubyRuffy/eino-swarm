package engine

import (
	"errors"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// ErrNuwaModelUnavailable means a Nuwa planner or executor is not in the
// ready catalog. The project editor is the place to choose again; the turn
// must not start on a different model by itself.
var ErrNuwaModelUnavailable = errors.New("engine: choose a ready planner model and executor model in the project")

// ErrNuwaModelInvalid means a save named a model that is not ready, or
// supplied only half of the pair.
var ErrNuwaModelInvalid = errors.New("engine: the Nuwa planner and executor must both be ready models")

// NuwaModelPair is the project planner and the model new worker roles use.
type NuwaModelPair struct {
	PlannerProviderID  string
	PlannerModel       string
	ExecutorProviderID string
	ExecutorModel      string
}

func (p NuwaModelPair) complete() bool {
	return strings.TrimSpace(p.PlannerProviderID) != "" &&
		strings.TrimSpace(p.PlannerModel) != "" &&
		strings.TrimSpace(p.ExecutorProviderID) != "" &&
		strings.TrimSpace(p.ExecutorModel) != ""
}

func nuwaPairSaved(p *store.Project) bool {
	return p != nil && strings.TrimSpace(p.NuwaPlannerProviderID) != "" &&
		strings.TrimSpace(p.NuwaPlannerModel) != "" &&
		strings.TrimSpace(p.NuwaExecutorProviderID) != "" &&
		strings.TrimSpace(p.NuwaExecutorModel) != ""
}

func nuwaPairFromProject(p *store.Project) NuwaModelPair {
	if p == nil {
		return NuwaModelPair{}
	}
	return NuwaModelPair{
		PlannerProviderID:  p.NuwaPlannerProviderID,
		PlannerModel:       p.NuwaPlannerModel,
		ExecutorProviderID: p.NuwaExecutorProviderID,
		ExecutorModel:      p.NuwaExecutorModel,
	}
}

// DefaultNuwaModelPair picks a planner and executor from the ready catalog.
// The planner is the first ready row marked default, otherwise the first
// ready row. The executor is the first other ready row, or the planner when
// the catalog has only one. Order is the catalog order. Nothing here ranks
// models or names a product model.
func (e *Engine) DefaultNuwaModelPair() (NuwaModelPair, error) {
	ready := e.readyNuwaModels()
	if len(ready) == 0 {
		return NuwaModelPair{}, ErrNuwaModelUnavailable
	}
	planner := ready[0]
	for _, info := range ready {
		if info.Default {
			planner = info
			break
		}
	}
	executor := planner
	for _, info := range ready {
		if info.ProviderID != planner.ProviderID || info.Model != planner.Model {
			executor = info
			break
		}
	}
	return NuwaModelPair{
		PlannerProviderID: planner.ProviderID, PlannerModel: planner.Model,
		ExecutorProviderID: executor.ProviderID, ExecutorModel: executor.Model,
	}, nil
}

// ResolveNuwaModelPair decides what to store. A supplied pair is checked
// against the ready catalog. Enabling Nuwa with no pair and no saved pair
// fills the catalog default. Disabling, or enabling again with a saved pair,
// leaves the stored columns unchanged.
func (e *Engine) ResolveNuwaModelPair(enabled bool, existing *store.Project, supplied *NuwaModelPair) (*NuwaModelPair, error) {
	if supplied != nil {
		pair := *supplied
		if err := e.ValidateNuwaModelPair(pair); err != nil {
			return nil, err
		}
		return &pair, nil
	}
	if !enabled || nuwaPairSaved(existing) {
		return nil, nil
	}
	pair, err := e.DefaultNuwaModelPair()
	if err != nil {
		return nil, err
	}
	return &pair, nil
}

// ValidateNuwaModelPair rejects a half pair or a name that is not ready.
func (e *Engine) ValidateNuwaModelPair(pair NuwaModelPair) error {
	if !pair.complete() {
		return ErrNuwaModelInvalid
	}
	if _, err := e.readyNuwaProvider(pair.PlannerProviderID, pair.PlannerModel); err != nil {
		return ErrNuwaModelInvalid
	}
	if _, err := e.readyNuwaProvider(pair.ExecutorProviderID, pair.ExecutorModel); err != nil {
		return ErrNuwaModelInvalid
	}
	return nil
}

// SaveNuwaModelPair writes a validated pair without changing Nuwa mode.
// Closing the mode does not call this, so the previous choice stays.
func (e *Engine) SaveNuwaModelPair(id string, pair NuwaModelPair) (*store.Project, error) {
	if err := e.ValidateNuwaModelPair(pair); err != nil {
		return nil, err
	}
	if err := e.store.UpdateProject(id, map[string]any{
		"nuwa_planner_provider_id":  strings.TrimSpace(pair.PlannerProviderID),
		"nuwa_planner_model":        strings.TrimSpace(pair.PlannerModel),
		"nuwa_executor_provider_id": strings.TrimSpace(pair.ExecutorProviderID),
		"nuwa_executor_model":       strings.TrimSpace(pair.ExecutorModel),
	}); err != nil {
		return nil, err
	}
	return e.store.GetProject(id)
}

func (e *Engine) readyNuwaModels() []configInfo {
	var ready []configInfo
	for _, info := range e.pool.List() {
		if info.Ready && strings.TrimSpace(info.ProviderID) != "" && strings.TrimSpace(info.Model) != "" {
			ready = append(ready, configInfo{ProviderID: info.ProviderID, Model: info.Model, Default: info.Default})
		}
	}
	return ready
}

// configInfo is the ready-catalog slice this file needs. It is not the
// provider info type, so a UI field added there does not leak into selection.
type configInfo struct {
	ProviderID string
	Model      string
	Default    bool
}

func (e *Engine) readyNuwaProvider(providerID, model string) (config.Provider, error) {
	providerID = strings.TrimSpace(providerID)
	model = strings.TrimSpace(model)
	for _, info := range e.readyNuwaModels() {
		if info.ProviderID != providerID || info.Model != model {
			continue
		}
		prov, err := e.pool.ResolveModel(providerID, model)
		if err != nil {
			return config.Provider{}, ErrNuwaModelUnavailable
		}
		prov.Model = model
		prov.ID = providerID
		return prov, nil
	}
	return config.Provider{}, ErrNuwaModelUnavailable
}
