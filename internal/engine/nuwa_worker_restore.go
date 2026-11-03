package engine

import (
	"context"
	"errors"
	"fmt"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
)

var errTurnProjectOwnerChanged = errors.New("project owner changed")

func validateTurnProjectOwner(turn *store.Turn, thread *store.Thread) error {
	// Store.Open materializes legacy NULLs. An empty string here is an
	// explicit standalone owner and must still match the conversation.
	if turn.ProjectID != thread.ProjectID {
		return errTurnProjectOwnerChanged
	}
	return nil
}

func (e *Engine) workerConfigurationOwner(ctx context.Context, threadID, tag string) (*store.Turn, *store.Thread, error) {
	turn, err := e.store.GetTurnContext(ctx, tag)
	if err != nil {
		return nil, nil, err
	}
	if turn.ThreadID != threadID {
		return nil, nil, fmt.Errorf("worker configuration belongs to another conversation")
	}
	thread, err := e.store.GetThreadContext(ctx, threadID)
	if err != nil {
		return nil, nil, err
	}
	if err := validateTurnProjectOwner(turn, thread); err != nil {
		return nil, nil, fmt.Errorf("worker configuration's %w", err)
	}
	return turn, thread, nil
}

// Validate runnable tags before admitting a registry so a refusal leaves all
// saved workers available for recovery once the owner is corrected.
func (e *Engine) validateRestoredWorkerProjectOwners(ctx context.Context, threadID string, workers []swarm.RestoredWorker) error {
	checked := map[string]bool{}
	for _, worker := range workers {
		if worker.ConfigTag == "" || checked[worker.ConfigTag] {
			continue
		}
		if _, _, err := e.workerConfigurationOwner(ctx, threadID, worker.ConfigTag); err != nil {
			return fmt.Errorf("restore worker configuration: %w", err)
		}
		checked[worker.ConfigTag] = true
	}
	return nil
}

// A host tag references a durable turn, not executable configuration from the
// model. Rebuilding uses that turn's mode and verifies its original ownership.
func (e *Engine) workerConfigurationForTag(ctx context.Context, threadID, tag string) (swarm.WorkerConfig, error) {
	turn, thread, err := e.workerConfigurationOwner(ctx, threadID, tag)
	if err != nil {
		return swarm.WorkerConfig{}, err
	}
	pc, err := e.projectContextFor(thread)
	if err != nil {
		return swarm.WorkerConfig{}, err
	}
	set, err := e.conversationToolsForExecution(ctx, thread, e.turnWebExecution(thread, turn))
	if err != nil {
		return swarm.WorkerConfig{}, err
	}
	if turn.NuwaReadOnly {
		set = tools.ExploreOnly(set)
	}
	builder, err := e.pool.ModelBuilder(ctx, turn.ProviderID, turn.Model, turn.ReasoningEffort, e.callRecorder(threadID, tag))
	if err != nil {
		return swarm.WorkerConfig{}, err
	}
	reg := e.newTurnRegistry(builder, set, pc, turn)
	return reg.WorkerConfiguration(), nil
}
