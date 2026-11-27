package engine

import (
	"context"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// wireWorkerModelRecovery lets a restored worker rebuild the exact model it
// last ran on. The binding survives catalog or default changes and process
// replacement; every rebuilt factory still passes through worker accounting.
func (e *Engine) wireWorkerModelRecovery(reg *swarm.Registry, turn *store.Turn) {
	if turn == nil {
		return
	}
	recorder := e.callRecorder(turn.ThreadID, turn.ID)
	reg.RestoreWorkerModel = func(ctx context.Context, binding swarm.ModelBinding) (swarm.ModelBuilder, error) {
		return e.pool.RestoreWorkerModel(ctx, binding, recorder)
	}
}
