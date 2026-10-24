package swarm

import "sort"

// HandoffWorker identifies a worker still waiting to settle its handoff.
// It intentionally carries no task text, model history, or tool arguments.
type HandoffWorker struct {
	ID   string `json:"id"`
	Role string `json:"role"`
}

// HandoffProgress separates settled workers from durable checkpoint writes.
// A cancelled handoff is settled even when an external operation ignores stop;
// that operation remains unknown in its checkpoint rather than completed.
type HandoffProgress struct {
	Total   int             `json:"total"`
	Settled int             `json:"settled"`
	Pending []HandoffWorker `json:"pending,omitempty"`
}

// HandoffProgress reads only lifecycle flags and identities, without cloning
// worker histories or consuming inboxes. Archived workers count once.
func (r *Registry) HandoffProgress() HandoffProgress {
	r.mu.Lock()
	p := HandoffProgress{Total: len(r.past), Settled: len(r.past)}
	handles := make([]*Handle, 0, len(r.agents))
	for id, h := range r.agents {
		if _, archived := r.past[id]; archived {
			p.Settled--
		} else {
			p.Total++
		}
		handles = append(handles, h)
	}
	r.mu.Unlock()
	for _, h := range handles {
		h.mu.Lock()
		if !h.finished.IsZero() || h.handoff.cancelled {
			p.Settled++
		} else {
			p.Pending = append(p.Pending, HandoffWorker{ID: h.ID, Role: h.Role})
		}
		h.mu.Unlock()
	}
	sort.Slice(p.Pending, func(i, j int) bool { return p.Pending[i].ID < p.Pending[j].ID })
	return p
}
