package engine

import "github.com/LubyRuffy/eino-swarm/internal/config"

// UpdateConfig orders desktop and phone patches through persistence and live
// application. No pool/runtime lock is taken while Config holds its write lock.
func (e *Engine) UpdateConfig(edit func(*config.Config) error) error {
	e.configUpdateMu.Lock()
	defer e.configUpdateMu.Unlock()
	if err := e.cfg.Update(edit); err != nil {
		return err
	}
	e.pool.Invalidate()
	e.ApplyLiveSwarmLimits()
	return nil
}
