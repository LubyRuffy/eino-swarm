package engine

import (
	"encoding/json"
	"fmt"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const workerCollectionCorrectionCue = "The manager's completion attempt was withheld because worker results have not been collected. Continue this same turn with the existing worker IDs below. Use wait_agents to receive their actual current results, or close_agent only for work no longer needed. A wait timeout, collecting a different worker or a previous generation, and saying that work will return later do not settle these workers. Do not spawn replacements or invent a goal or schedule to bypass collection."

func workerCollectionCue(ids []string) string {
	body, _ := json.Marshal(ids)
	return workerCollectionCorrectionCue + "\nUncollected worker IDs: " + string(body)
}

func (rt *runtime) uncollectedWorkerIDs(reg *swarm.Registry) []string {
	pending := reg.PendingAgents()
	if len(pending) == 0 {
		return nil
	}
	if _, owner := rt.workerOwner(); owner != "" {
		return nil
	}
	ids := make([]string, 0, len(pending))
	for _, worker := range pending {
		ids = append(ids, worker.AgentID)
	}
	return ids
}

// This allowance belongs to the durable turn, not one RunWith invocation or
// process. A human extension is the only way to renew a consumed allowance.
func (e *Engine) workerCollectionContinued(turn *store.Turn, snapshots ...*restoreSnapshot) (bool, error) {
	events, err := e.restoreTurnEvents(turn.ID, snapshots...)
	if err != nil {
		return false, fmt.Errorf("read worker collection correction allowance: %w", err)
	}
	used := false
	for _, ev := range events {
		if !isManagerAgent(ev.AgentID) {
			continue
		}
		if ev.Kind == KindMaxIterationsContinued {
			used = false
		}
		var body struct{ Reason string }
		if ev.Kind == KindModelRetry && json.Unmarshal([]byte(ev.Text), &body) == nil && body.Reason == "workers_uncollected" {
			used = true
		}
	}
	return used, nil
}

func (rt *runtime) recordWorkerCollectionRetry(turn *store.Turn, ids []string) error {
	body, _ := json.Marshal(map[string]any{"attempt": 1, "cap": 1, "reason": "workers_uncollected", "worker_ids": ids})
	e := rt.engine
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	if _, dropped := e.droppedTurns[turn.ID]; dropped {
		return fmt.Errorf("worker collection owner was deleted")
	}
	ev := store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: KindModelRetry, AgentID: swarm.DefaultManagerID, Text: string(body)}
	if err := e.store.AppendEvent(&ev); err != nil {
		return fmt.Errorf("persist worker collection correction allowance: %w", err)
	}
	e.broadcast(ev)
	return nil
}
