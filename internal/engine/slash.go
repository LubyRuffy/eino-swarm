package engine

import (
	"fmt"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/slash"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// applySlashGoal intercepts a `/goal` user send the way Codex dispatches
// SlashCommand::Goal: it is never a chat line, and a live turn is steered
// instead of 409. Composer parse can miss (fullwidth slash, IME `、`, glued
// CJK); this is the layer that still has to catch it.
func (e *Engine) applySlashGoal(threadID string, in *UserInput) (turn *store.Turn, done bool, err error) {
	if in == nil || in.ContinueGoal || in.ContinueSchedule {
		return nil, false, nil
	}
	arg, ok := slash.Lookup(in.Text, "goal")
	if !ok {
		return nil, false, nil
	}
	if strings.TrimSpace(arg) == "" {
		return nil, false, fmt.Errorf("engine: /goal needs an objective")
	}
	if err := e.SetThreadGoal(threadID, arg); err != nil {
		return nil, false, err
	}
	if e.Status(threadID).Running {
		id := e.lastTurnID(threadID)
		if id == "" {
			return nil, true, nil
		}
		turn, err := e.store.GetTurn(id)
		return turn, true, err
	}
	in.Text = arg
	return nil, false, nil
}
