package app

import (
	"context"
	"errors"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"time"
)

// Restore is owned by the serving engine. It finishes recovery before starting
// scheduling; cancellation leaves durable checkpoints available to the next owner.
func (a *App) Restore(ctx context.Context) error {
	a.restoreMu.Lock()
	defer a.restoreMu.Unlock()
	if a.restored {
		return nil
	}
	restoreCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(a.startupCtx, cancel)
	defer stop()
	if a.startupCtx.Err() != nil {
		cancel()
	}
	started := time.Now()
	if err := a.Engine.Restore(restoreCtx, a.Engine.SetRestoreProgress); err != nil {
		var partial *engine.RestoreFailures
		if !errors.As(err, &partial) {
			return err
		}
		a.Engine.SetRestoreFailures(partial.Failed)
		a.Logger.Warn("some conversations could not be restored", "failed", partial.Failed, "err", err)
	}
	if err := restoreCtx.Err(); err != nil {
		return err
	}
	a.Engine.FinishRestore()
	a.restored = true
	a.Logger.Info("engine startup", "phase", "unfinished_runs_restored", "duration_ms", time.Since(started).Milliseconds())
	return nil
}
