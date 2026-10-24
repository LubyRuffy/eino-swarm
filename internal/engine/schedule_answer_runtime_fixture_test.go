package engine

import (
	"context"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/adk"
)

// Direct adapter tests still need the real runtime claim that production's
// StartTurn installs. This owns a Registry and releases it through the actual
// lifecycle rather than weakening publication's running/turn identity check.
func ownManagerTestFixture(ctx context.Context, rt *runtime, turnID string, reg *swarm.Registry) (context.Context, func()) {
	rt.mu.Lock()
	running := rt.running
	rt.mu.Unlock()
	if running {
		return ctx, func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	idle := make(chan struct{})
	rt.occupy(reg, cancel, turnID, idle)
	return ctx, func() { rt.release(cancel, idle, turnID) }
}

func (rt *runtime) runOwnedTestManager(ctx context.Context, turn *store.Turn, reg *swarm.Registry, acc *accumulator, set *tools.Set, pc *projectContext, messages []adk.Message) (swarm.RunResult, error) {
	ctx, release := ownManagerTestFixture(ctx, rt, turn.ID, reg)
	defer release()
	return rt.runManager(ctx, turn, reg, acc, set, pc, messages)
}

func (e *Engine) reportOwnedTestScheduleJSON(threadID, turnID, args string) (string, error) {
	reg := swarm.NewRegistry()
	defer reg.Close()
	ctx, release := ownManagerTestFixture(context.Background(), e.runtimeFor(threadID), turnID, reg)
	defer release()
	return e.reportScheduleContextJSON(ctx, threadID, turnID, args)
}
