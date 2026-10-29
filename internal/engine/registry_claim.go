package engine

import (
	"context"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// claimRegistry admits a successor before detaching its parked Registry. The
// same lock keeps Stop, idle accounting and a concurrent start from observing
// an unowned Registry or changing the source turn during the durable write.
// A fresh Registry's fallible restore preparation also precedes occupation.
func (rt *runtime) claimRegistry(ctx context.Context, turn *store.Turn, cancel context.CancelFunc, idle chan struct{}, fresh func() (*swarm.Registry, error), reconfigure func(*swarm.Registry)) (*swarm.Registry, bool, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.running {
		return nil, false, ErrBusy
	}
	if err := context.Cause(ctx); err != nil {
		return nil, false, err
	}
	reg := rt.parked
	reused := reg != nil
	if reused {
		if err := rt.engine.recordHistoryRegistryInheritance(ctx, rt.threadID, turn.ID, rt.turnID); err != nil {
			return nil, false, err
		}
		// Occupation installs dynamic worker accounting around this builder.
		// Refresh it first so newly spawned workers retain that wrapper too.
		if reconfigure != nil {
			reconfigure(reg)
		}
	} else {
		var err error
		reg, err = fresh()
		if err != nil {
			return nil, false, err
		}
	}
	rt.parked = nil
	rt.occupyLocked(reg, cancel, turn.ID, idle)
	return reg, reused, nil
}
