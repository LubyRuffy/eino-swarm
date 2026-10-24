package remote

import (
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"strings"
)

func requireStartupReady(eng *engine.Engine, op string) error {
	switch strings.TrimSpace(op) {
	case OpStart, OpSend, OpSteer, OpPreempt, OpAnswer, OpRunNow, OpCancelWait, OpResumeGoal, OpFollowupSteer, OpTune, OpSetTimeouts:
		return eng.RequireReady()
	default:
		return nil
	}
}
