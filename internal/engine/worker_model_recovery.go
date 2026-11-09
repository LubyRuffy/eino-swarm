package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// ErrInvalidWorkerRecovery reports a rejected human recovery selection.
var ErrInvalidWorkerRecovery = errors.New("invalid worker model recovery")

func recoveredWorkerBinding(turn *store.Turn, binding swarm.ModelBinding) swarm.ModelBinding {
	if replacement, ok := turn.WorkerModelRecovery[binding.ProviderID]; ok {
		return replacement
	}
	return binding
}

// RecoverWorkerModels records a human's explicit choice; a normal composer
// switch still applies only to new turns. This operation starts no execution.
func (e *Engine) RecoverWorkerModels(ctx context.Context, threadID, providerID, model string) (int, error) {
	if strings.TrimSpace(providerID) == "" {
		return 0, fmt.Errorf("%w: choose a provider first", ErrInvalidWorkerRecovery)
	}
	done, err := e.beginAdmission()
	if err != nil {
		return 0, err
	}
	defer done()
	rt := e.runtimeFor(threadID)
	unlock, err := rt.lockFinish(ctx)
	if err != nil {
		return 0, err
	}
	defer unlock()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.running || rt.parked != nil {
		return 0, ErrBusy
	}
	thread, err := e.store.GetThreadContext(ctx, threadID)
	if err != nil {
		return 0, err
	}
	target, _, err := e.pool.WorkerModel(ctx, providerID, model, thread.ReasoningEffort, nil)
	if err != nil {
		return 0, err
	}
	target.Ref = provider.ChoiceID(target.ProviderID, target.Model)
	snapshot, err := e.readRestoreSnapshot(ctx, threadID)
	if err != nil {
		return 0, err
	}
	changes := map[string]map[string]swarm.ModelBinding{}
	inspect := func(tag string, binding *swarm.ModelBinding) error {
		if tag == "" {
			return nil
		} // legacy untagged workers use the fresh Registry
		source, _, err := e.workerConfigurationOwner(ctx, threadID, tag)
		if err != nil {
			return err
		}
		selection := swarm.ModelBinding{ProviderID: source.ProviderID, Model: source.Model, ReasoningEffort: source.ReasoningEffort}
		candidates := []swarm.ModelBinding{selection}
		if binding != nil {
			candidates = append(candidates, *binding)
		}
		for _, original := range candidates {
			effective := recoveredWorkerBinding(source, original)
			if _, err := e.pool.Resolve(effective.ProviderID); !errors.Is(err, provider.ErrUnknownProvider) {
				continue
			}
			recovery := changes[tag]
			if recovery == nil {
				recovery = maps.Clone(source.WorkerModelRecovery)
				if recovery == nil {
					recovery = map[string]swarm.ModelBinding{}
				}
				changes[tag] = recovery
			}
			// Flatten prior migrations so old events and newer checkpoints agree.
			for id, previous := range recovery {
				if previous.ProviderID == effective.ProviderID {
					recovery[id] = target
				}
			}
			recovery[original.ProviderID] = target
			recovery[effective.ProviderID] = target
		}
		return nil
	}
	for _, worker := range snapshot.running {
		if err := inspect(worker.ConfigTag, worker.ModelBinding); err != nil {
			return 0, err
		}
	}
	for _, worker := range snapshot.finished {
		if worker.Collected || worker.CloseRequested || worker.Generation <= 0 {
			continue
		}
		if err := inspect(worker.ConfigTag, worker.ModelBinding); err != nil {
			return 0, err
		}
	}
	if len(changes) > 0 && snapshot.checkpointEvent != nil {
		if err := inspect(snapshot.checkpointEvent.TurnID, nil); err != nil {
			return 0, err
		}
	}
	if len(changes) == 0 {
		return 0, fmt.Errorf("%w: no retained workers have a missing provider", ErrInvalidWorkerRecovery)
	}
	if err := e.store.SaveWorkerModelRecovery(ctx, threadID, thread.ProjectID, changes); err != nil {
		return 0, err
	}
	return len(changes), nil
}
