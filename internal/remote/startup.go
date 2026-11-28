package remote

import (
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
)

func requireStartupReady(eng *engine.Engine, op string) error {
	switch strings.TrimSpace(op) {
	case OpTaskPanelClose, OpSetProductMode, OpTranslateSelection, OpProjectCreate, OpProjectUpdate, OpStart, OpSend, OpEditInput, OpDeliver, OpSteer, OpPreempt, OpAnswer, OpContinue, OpRunNow, OpCancelWait, OpResumeGoal, OpFollowupSteer, OpTune, OpSetTimeouts, OpHostUpdateStart:
		if err := eng.RequireReady(); err != nil {
			return err
		}
		return eng.RejectIfBackup()
	default:
		return nil
	}
}
