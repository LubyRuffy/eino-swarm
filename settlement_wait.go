package swarm

import (
	"context"
	"errors"
)

// ErrPendingGenerationChanged refuses a completion wait whose original pending
// execution was replaced, collected or closed. It never settles any result.
var ErrPendingGenerationChanged = errors.New("pending worker generation changed")

// WaitPendingCompletion observes all specified executions reaching terminal
// state. Unlike wait_agents, it returns no result and consumes no obligation.
// The caller must still collect their actual terminal results with wait_agents.
func (r *Registry) WaitPendingCompletion(ctx context.Context, expected []PendingAgent) error {
	if len(expected) == 0 {
		return ErrPendingGenerationChanged
	}
	r.mu.Lock()
	done := make([]<-chan struct{}, 0, len(expected))
	seen := make(map[string]int, len(expected))
	for _, p := range expected {
		h := r.pending[p.AgentID]
		if r.closed || h == nil || p.Generation <= 0 || !p.Running || seen[p.AgentID] != 0 {
			r.mu.Unlock()
			return ErrPendingGenerationChanged
		}
		h.mu.Lock()
		generation, ch := h.gen, h.done
		h.mu.Unlock()
		if generation != p.Generation {
			r.mu.Unlock()
			return ErrPendingGenerationChanged
		}
		seen[p.AgentID] = p.Generation
		done = append(done, ch)
	}
	r.mu.Unlock()
	for _, ch := range done {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	matched := 0
	for _, p := range r.PendingAgents() {
		if seen[p.AgentID] == p.Generation && p.Generation > 0 && !p.Running {
			matched++
		}
	}
	if matched != len(expected) {
		return ErrPendingGenerationChanged
	}
	return nil
}
