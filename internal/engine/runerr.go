package engine

import (
	"errors"
	"fmt"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

// publicTurnError is what the transcript and the turn row show. eino wraps
// a silent stream in NodeRunError and a graph path; that dump is for a
// trace, not a person.
func publicTurnError(err error) string {
	if err == nil {
		return ""
	}
	var idle provider.IdleTimeoutError
	if errors.As(err, &idle) {
		return fmt.Sprintf("the model sent no data for %s; raise the provider request timeout in Settings, or lower thinking", idle.Idle)
	}
	return err.Error()
}
