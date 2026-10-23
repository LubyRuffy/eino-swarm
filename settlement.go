package swarm

import (
	"context"
	"sort"
	"time"
)

// PendingAgent is a current worker generation whose result has not been
// returned by wait_agents or explicitly discarded by close_agent.
type PendingAgent struct {
	AgentID    string `json:"agent_id"`
	Role       string `json:"role"`
	Generation int    `json:"generation"`
	Running    bool   `json:"running"`
	Err        string `json:"error,omitempty"`
}

// PendingAgents is a non-consuming snapshot, including completed results
// pruned by Stats. A running entry remains pending after a timed-out wait.
func (r *Registry) PendingAgents() []PendingAgent {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []PendingAgent
	for _, h := range r.pending {
		h.mu.Lock()
		p := PendingAgent{AgentID: h.ID, Role: h.Role, Generation: h.gen, Running: true}
		select {
		case <-h.done:
			p.Running = false
		default:
		}
		if h.err != nil {
			p.Err = h.err.Error()
		}
		h.mu.Unlock()
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AgentID < out[j].AgentID })
	return out
}

// Caller holds r.mu. Pending handles are separate from the disposable
// Progress roster so a Stats poll cannot destroy an uncollected result.
func (r *Registry) notePendingLocked(h *Handle) {
	if r.pending == nil {
		r.pending = map[string]*Handle{}
	}
	if !h.collected && !h.closeRequested {
		r.pending[h.ID] = h
	}
}

type workerCollection struct {
	handle     *Handle
	generation int
}

func (h *Handle) generation() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.gen
}

func (r *Registry) collectWorker(token workerCollection, closeRequested bool) bool {
	var cancel context.CancelFunc
	defer func() {
		if cancel != nil {
			cancel()
		}
	}()
	r.mu.Lock()
	defer r.mu.Unlock()
	h := token.handle
	if r.pending[h.ID] != h {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.gen != token.generation {
		return false // an old result must never consume a resumed generation
	}
	h.collected = !closeRequested
	h.closeRequested = closeRequested
	if closeRequested {
		cancel = h.cancel // cancellation belongs to this generation, not a later resume
	}
	delete(r.pending, h.ID)
	if past := r.past[h.ID]; past != nil && past.checkpoint.Generation == h.gen {
		past.checkpoint.Collected = h.collected
		past.checkpoint.CloseRequested = h.closeRequested
	}
	return true
}

// One lock binds result, status and generation to the exact returned entry.
func (h *Handle) collectionSnapshot() (waitEntry, workerCollection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e := waitEntry{AgentID: h.ID, Role: h.Role, Generation: h.gen, Status: "running", Activity: h.activity}
	select {
	case <-h.done:
		e.Status, e.Activity, e.Result = "done", "", h.result
		e.Elapsed = float64(h.finished.Sub(h.spawned)) / float64(time.Millisecond)
		e.Undelivered = append([]string(nil), h.undelivered...)
		if h.err != nil {
			e.Status, e.Err = "failed", h.err.Error()
		}
	default:
	}
	return e, workerCollection{handle: h, generation: h.gen}
}

func (r *Registry) waitSnapshotCollections(ids []string, timedOut bool) (waitReport, []workerCollection) {
	report := waitReport{Agents: make([]waitEntry, 0, len(ids)), TimedOut: timedOut}
	var collected []workerCollection
	for _, id := range ids {
		h, ok := r.get(id)
		if !ok {
			report.Agents = append(report.Agents, waitEntry{AgentID: id, Status: "unknown", Err: "unknown agent"})
			continue
		}
		e, token := h.collectionSnapshot()
		report.Agents = append(report.Agents, e)
		if e.Status != "running" {
			collected = append(collected, token)
		}
	}
	return report, collected
}

func (r *Registry) restoreCollection(h *Handle, generation int, collected, closed bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	if generation > 0 {
		h.gen = generation
	}
	h.collected, h.closeRequested = collected, closed
	delete(r.pending, h.ID)
	r.notePendingLocked(h)
}
