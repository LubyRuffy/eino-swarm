package engine

import (
	"errors"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// errNuwaModeRemoved ends a turn that was started in the removed Nuwa mode.
// turns.nuwa_enabled stays in the schema only so such a turn is recognised
// here; its planner/executor split no longer exists, so continuing or retrying
// it as an ordinary turn would silently change what it was doing.
var errNuwaModeRemoved = errors.New("the Nuwa mode was removed; this turn cannot be continued or retried")

func rejectRemovedNuwa(turn *store.Turn) error {
	if turn != nil && turn.NuwaEnabled {
		return errNuwaModeRemoved
	}
	return nil
}
