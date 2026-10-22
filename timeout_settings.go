package swarm

import "time"

// SetAgentTimeout affects workers whose lifetime has not been created yet.
// Existing contexts and the watchdog recovery budget are deliberately untouched.
func (r *Registry) SetAgentTimeout(timeout time.Duration) {
	r.mu.Lock()
	r.AgentTimeout = timeout
	r.mu.Unlock()
}

func (r *Registry) agentTimeout() time.Duration {
	r.mu.Lock()
	timeout := r.AgentTimeout
	r.mu.Unlock()
	if timeout <= 0 {
		return DefaultAgentTimeout
	}
	return timeout
}

// SetMaxTurns shares the same lock as the worker's initial turn-cap snapshot.
func (r *Registry) SetMaxTurns(turns int) { r.mu.Lock(); r.MaxTurns = turns; r.mu.Unlock() }
