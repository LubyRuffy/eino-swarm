package remote

import (
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"strings"
)

func requireStartupReady(eng *engine.Engine, op string) error {
	switch strings.TrimSpace(op) {
	case OpProjectCreate, OpProjectUpdate, OpStart, OpSend, OpSteer, OpPreempt, OpAnswer, OpContinue, OpRunNow, OpCancelWait, OpResumeGoal, OpFollowupSteer, OpTune, OpSetTimeouts:
		return eng.RequireReady()
	default:
		return nil
	}
}
