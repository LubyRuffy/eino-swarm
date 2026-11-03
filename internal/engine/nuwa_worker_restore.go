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

// Validate runnable work and uncollected results before installing a fresh
// Registry. A settled archive from another project stays in durable history.
func (e *Engine) restorableWorkerArchive(ctx context.Context, threadID string, running []swarm.RestoredWorker, finished []swarm.FinishedWorker) ([]swarm.FinishedWorker, error) {
	checked := map[string]error{}
	check := func(tag string) error {
		if tag == "" {
			return nil
		}
		if err, found := checked[tag]; found {
			return err
		}
		_, _, err := e.workerConfigurationOwner(ctx, threadID, tag)
		checked[tag] = err
		return err
	}
	for _, worker := range running {
		if err := check(worker.ConfigTag); err != nil {
			return nil, fmt.Errorf("restore worker configuration: %w", err)
		}
	}
	archive := make([]swarm.FinishedWorker, 0, len(finished))
	for _, worker := range finished {
		if err := check(worker.ConfigTag); err != nil {
			settled := worker.Collected || worker.CloseRequested || worker.Generation <= 0
			if settled && errors.Is(err, errTurnProjectOwnerChanged) {
				continue
			}
			return nil, fmt.Errorf("restore worker configuration: %w", err)
		}
		archive = append(archive, worker)
	}
	return archive, nil
}

// Caller holds rt.mu so an owner refusal leaves the original parked Registry
// available for Stop or a later attempt after the conversation is moved back.
func (rt *runtime) validateParkedWorkerProjectOwners(ctx context.Context, target *store.Turn) error {
	for _, tag := range rt.parked.RetainedWorkerConfigTags() {
		source, _, err := rt.engine.workerConfigurationOwner(ctx, rt.threadID, tag)
		if err != nil {
			return fmt.Errorf("adopt worker configuration: %w", err)
		}
		if target != nil && source.ProjectID != target.ProjectID {
			return fmt.Errorf("adopt worker configuration: %w", errTurnProjectOwnerChanged)
		}
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
