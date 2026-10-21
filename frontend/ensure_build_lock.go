package frontend

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/lease"
)

// The engine's data-dir lease does not cover another shell using the same
// checkout. Keep the build lock outside npm's and Vite's replaceable outputs.
// The local slot also excludes same-process calls on platforms with reentrant
// process-owned file locks, and remains cancellable while another build runs.
var buildSlot = make(chan struct{}, 1)

func lockBuild(ctx context.Context, root string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case buildSlot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	releaseSlot := func() { <-buildSlot }
	poll := time.NewTicker(50 * time.Millisecond)
	defer poll.Stop()
	for {
		hold, err := lease.Acquire(filepath.Join(root, ".build-lock"))
		if err == nil {
			return func() { hold.Release(); releaseSlot() }, nil
		}
		if !errors.Is(err, lease.ErrHeld) {
			releaseSlot()
			return nil, fmt.Errorf("frontend build lock: %w", err)
		}
		select {
		case <-ctx.Done():
			releaseSlot()
			return nil, ctx.Err()
		case <-poll.C:
		}
	}
}
