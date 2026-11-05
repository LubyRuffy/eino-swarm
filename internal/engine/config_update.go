package engine

import (
	"reflect"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// UpdateConfig orders desktop and phone patches through persistence and live
// application. No pool/runtime lock is taken while Config holds its write lock.
func (e *Engine) UpdateConfig(edit func(*config.Config) error) error {
	e.configUpdateMu.Lock()
	defer e.configUpdateMu.Unlock()
	previousMCP := e.cfg.Snapshot().MCP
	if err := e.cfg.Update(edit); err != nil {
		return err
	}
	currentMCP := e.cfg.Snapshot().MCP
	if e.mcp != nil && !reflect.DeepEqual(previousMCP, currentMCP) {
		e.mcp.Reload(currentMCP.Servers)
	}
	e.pool.Invalidate()
	e.ApplyLiveSwarmLimits()
	return nil
}
