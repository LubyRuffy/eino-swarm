package swarm

import (
	"context"
	"fmt"
	"sort"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func (r *Registry) delegationParent(ctx context.Context) (string, error) {
	caller, exists := ToolCaller(ctx)
	if !exists || caller.AgentID == DefaultManagerID {
		return "", nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !r.workerConfigurationForContext(ctx).EnableDelegation {
		return "", fmt.Errorf("worker delegation is not enabled")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.validateParentLocked(caller); err != nil {
		return "", err
	}
	return caller.AgentID, nil
}

// Caller holds r.mu, the same lock that closes a subtree and admits new work.
func (r *Registry) validateParentLocked(caller AgentIdentity) error {
	h := r.agents[caller.AgentID]
	if h == nil {
		return fmt.Errorf("delegating worker is no longer active")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.gen != caller.Generation || h.closeRequested || !h.finished.IsZero() {
		return fmt.Errorf("delegating worker generation is no longer active")
	}
	return nil
}

func (r *Registry) workerTools(config WorkerConfig, existing []tool.BaseTool) []tool.BaseTool {
	if !config.EnableDelegation {
		return existing
	}
	for _, candidate := range r.toolsWithWorkerConfig(config) {
		if toolName(candidate) != "send_message" {
			existing = append(existing, candidate)
		}
	}
	return existing
}

// Caller holds r.mu. Parent identity is immutable throughout a worker's life.
func (r *Registry) parentOfLocked(id string) string {
	if h := r.agents[id]; h != nil {
		return h.ParentID
	}
	if h := r.pending[id]; h != nil {
		return h.ParentID
	}
	if past := r.past[id]; past != nil {
		return past.ParentID
	}
	return ""
}

func (r *Registry) descendantLocked(id, ancestor string) bool {
	seen := map[string]bool{}
	for id != "" && !seen[id] {
		seen[id] = true
		id = r.parentOfLocked(id)
		if id == ancestor {
			return true
		}
	}
	return false
}

func (r *Registry) validateRestoredParent(id, parent string) error {
	if parent == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.validateParentRelationLocked(id, parent)
}

func (r *Registry) validateParentRelationLocked(id, parent string) error {
	if parent == id || r.descendantLocked(parent, id) {
		return fmt.Errorf("restore: worker parent relation contains a cycle")
	}
	return nil
}

func (r *Registry) requireWorkerTargets(ctx context.Context, ids []string) error {
	parent, err := r.delegationParent(ctx)
	if err != nil || parent == "" {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range ids {
		if !r.descendantLocked(id, parent) {
			return fmt.Errorf("worker may only control its own descendants; %q is outside its subtree", id)
		}
	}
	return nil
}

func (r *Registry) spawnContext(parentID string) []*schema.Message {
	if parentID == "" {
		return r.historySnapshot()
	}
	r.mu.Lock()
	h := r.agents[parentID]
	r.mu.Unlock()
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return cloneMessages(h.history)
}

type closedWorker struct {
	AgentID         string `json:"agent_id"`
	Generation      int    `json:"generation"`
	Cancelled       bool   `json:"cancelled"`
	AlreadyFinished bool   `json:"already_finished"`
}

func (r *Registry) closeSubtree(id string) (string, error) {
	r.mu.Lock()
	handles := map[string]*Handle{}
	for key, h := range r.agents {
		handles[key] = h
	}
	for key, h := range r.pending {
		handles[key] = h
	}
	if handles[id] == nil && r.past[id] == nil {
		r.mu.Unlock()
		return ctlRefuse(fmt.Sprintf("close_agent: unknown agent %q", id))
	}
	ids := []string{id}
	for key := range handles {
		if key != id && r.descendantLocked(key, id) {
			ids = append(ids, key)
		}
	}
	sort.Strings(ids[1:])
	var cancellations []context.CancelFunc
	closed := make([]closedWorker, 0, len(ids))
	for _, key := range ids {
		entry := closedWorker{AgentID: key, AlreadyFinished: true}
		if h := handles[key]; h != nil {
			h.mu.Lock()
			entry.Generation = h.gen
			select {
			case <-h.done:
			default:
				entry.Cancelled = true
				entry.AlreadyFinished = false
			}
			h.collected, h.closeRequested = entry.AlreadyFinished, entry.Cancelled
			if entry.Cancelled && h.cancel != nil {
				cancellations = append(cancellations, h.cancel)
			}
			delete(r.pending, key)
			if past := r.past[key]; past != nil && past.checkpoint.Generation == h.gen {
				past.checkpoint.Collected = h.collected
				past.checkpoint.CloseRequested = h.closeRequested
			}
			h.mu.Unlock()
		} else {
			past := r.past[key]
			entry.Generation = past.checkpoint.Generation
			past.checkpoint.Collected, past.checkpoint.CloseRequested = true, false
		}
		closed = append(closed, entry)
	}
	r.mu.Unlock()
	for _, cancel := range cancellations {
		cancel()
	}
	return marshal(map[string]any{"agent_id": id, "generation": closed[0].Generation, "cancelled": closed[0].Cancelled, "already_finished": closed[0].AlreadyFinished, "closed_agents": closed}), nil
}

// Keep ancestry needed by live or retained descendants when bounding history.
// A finished leaf may be forgotten, but its still-needed ancestors may not.
func (r *Registry) trimArchiveLocked() {
	for len(r.pastIDs) > r.archiveCap() {
		removed := false
		for index, id := range r.pastIDs {
			needed := false
			for child := range r.agents {
				needed = needed || r.descendantLocked(child, id)
			}
			for child := range r.pending {
				needed = needed || r.descendantLocked(child, id)
			}
			for child := range r.past {
				needed = needed || r.descendantLocked(child, id)
			}
			if needed {
				continue
			}
			r.pastIDs = append(r.pastIDs[:index], r.pastIDs[index+1:]...)
			delete(r.past, id)
			removed = true
			break
		}
		if !removed {
			return
		}
	}
}
