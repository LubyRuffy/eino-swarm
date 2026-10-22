package engine

import swarm "github.com/LubyRuffy/eino-swarm"

// bindSwarmLimits copies the live config onto a registry. A parked /goal
// swarm outlives the turn that minted it, so reuse and Settings both
// have to re-apply or the old cap stays in force.
func (e *Engine) bindSwarmLimits(reg *swarm.Registry) {
	if reg == nil {
		return
	}
	limits := e.cfg.SwarmSnapshot()
	reg.SetMaxConcurrent(limits.MaxConcurrent)
	reg.SetAgentTimeout(limits.AgentTimeout())
	reg.SetMaxTurns(limits.MaxTurns)
}

// ApplyLiveSwarmLimits pushes the current swarm caps onto every live or
// parked registry. PUT /settings used to only stick on the next
// NewRegistry; queued workers kept waiting on the semaphore from the
// first spawn.
func (e *Engine) ApplyLiveSwarmLimits() {
	e.snapshotScheduleCaps()
	e.mu.Lock()
	rts := make([]*runtime, 0, len(e.runtimes))
	for _, rt := range e.runtimes {
		rts = append(rts, rt)
	}
	e.mu.Unlock()
	for _, rt := range rts {
		rt.mu.Lock()
		live, parked := rt.reg, rt.parked
		rt.mu.Unlock()
		e.bindSwarmLimits(live)
		e.bindSwarmLimits(parked)
	}
}
