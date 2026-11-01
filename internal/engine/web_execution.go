package engine

import (
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// SetThreadWebExecution changes this conversation's Web tool location from its
// next turn. Empty inherits the PC setting and never changes that setting.
func (e *Engine) SetThreadWebExecution(id, preference string) error {
	mode, err := config.ParseWebExecution(preference)
	if err != nil {
		return err
	}
	return e.store.UpdateThread(id, map[string]any{"web_execution": mode})
}

func (e *Engine) turnWebExecution(th *store.Thread, turn *store.Turn) string {
	if turn != nil && turn.WebExecution != "" {
		return turn.WebExecution
	}
	if th.WebExecution != "" {
		return th.WebExecution
	}
	return e.cfg.Snapshot().Tools.WebExecution
}
