package engine

import (
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/workerstate"
	"github.com/cloudwego/eino/schema"
)

// workerRosterMessages preserves resumable identities as runtime state. A synthetic
// spawn/tool pair makes finished workers look newly created on every continuation.
func workerRosterMessages(running []swarm.RestoredWorker, finished []swarm.FinishedWorker) []*schema.Message {
	workers := make([]workerstate.Worker, 0, len(running)+len(finished))
	seen := map[string]bool{}
	add := func(id, role, status string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		role = strings.TrimSpace(role)
		if role == "" {
			role = "worker"
		}
		workers = append(workers, workerstate.Worker{AgentID: id, Role: role, Status: status})
	}
	for _, worker := range running {
		add(worker.ID, worker.Role, workerstate.Running)
	}
	for _, worker := range finished {
		status := workerstate.Done
		if worker.Err != nil {
			status = workerstate.Failed
		}
		add(worker.ID, worker.Role, status)
	}
	if message := workerstate.New(workers); message != nil {
		return []*schema.Message{message}
	}
	return nil
}

func (m *autoCompact) rosterPin(fallback []*schema.Message) []*schema.Message {
	if m == nil || m.engine == nil || m.threadID == "" {
		return nil
	}
	running, finished, err := m.engine.readWorkersFromThread(m.threadID)
	if err != nil {
		for i := len(fallback) - 1; i >= 0; i-- {
			if _, ok := workerstate.Parse(fallback[i]); ok {
				return []*schema.Message{fallback[i]}
			}
		}
		return nil
	}
	return workerRosterMessages(running, finished)
}
