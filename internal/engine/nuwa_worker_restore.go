package engine

import (
	"context"
	"fmt"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
)

// A host tag references a durable turn, not executable configuration from the
// model. Rebuilding uses that turn's mode and verifies its original ownership.
func (e *Engine) workerConfigurationForTag(ctx context.Context, threadID, tag string) (swarm.WorkerConfig, error) {
	turn, err := e.store.GetTurnContext(ctx, tag)
	if err != nil {
		return swarm.WorkerConfig{}, err
	}
	if turn.ThreadID != threadID {
		return swarm.WorkerConfig{}, fmt.Errorf("worker configuration belongs to another conversation")
	}
	thread, err := e.store.GetThreadContext(ctx, threadID)
	if err != nil {
		return swarm.WorkerConfig{}, err
	}
	if turn.ProjectID != thread.ProjectID {
		return swarm.WorkerConfig{}, fmt.Errorf("worker configuration's project owner changed")
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
