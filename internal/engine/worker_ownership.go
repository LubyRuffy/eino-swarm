package engine

import (
	"encoding/json"
	"errors"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// KindWorkersRetained is a durable receipt, not a successful scheduled check.
// Consumers must not infer a worker failure from its manager's terminal error.
const KindWorkersRetained = "workers_retained"

type workerRetention struct {
	Reason    string   `json:"reason"`
	OwnerKind string   `json:"owner_kind"`
	OwnerID   string   `json:"owner_id"`
	WorkerIDs []string `json:"worker_ids"`
}

func (rt *runtime) workerOwner() (kind, id string) {
	th, err := rt.engine.store.GetThread(rt.threadID)
	if err != nil || th.PlanMode {
		return "", ""
	}
	if pursuingGoal(th) && !th.GoalCapped && !th.GoalIdle &&
		th.GoalAutoTurns < rt.engine.cfg.SwarmSnapshot().GoalAutoTurns() {
		return "goal", th.ID
	}
	wake, err := rt.engine.store.ActiveThreadWake(rt.threadID)
	if err == nil && wake != nil {
		return "thread_wake", wake.ID
	}
	return "", ""
}

func isolatedScheduledFailure(turn *store.Turn, status string, runErr error) bool {
	var exhausted *swarm.WorkerTimeoutLimitError
	return turn.ScheduleContinue && status == store.TurnError && !isContextOverflow(runErr) && !errors.As(runErr, &exhausted)
}

func (rt *runtime) parkAfterTurn(turn *store.Turn, status string, runErr error, reg *swarm.Registry) bool {
	if status == store.TurnDone {
		if rt.shouldPark(status) {
			rt.parkRegistry(reg)
			return true
		}
		return false
	}
	if !isolatedScheduledFailure(turn, status, runErr) {
		return false
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.userStopped || rt.abandoned {
		return false
	}
	kind, owner := rt.workerOwner()
	if owner == "" {
		return false
	}
	receipt := workerRetention{Reason: "scheduled_check_failed", OwnerKind: kind, OwnerID: owner, WorkerIDs: []string{}}
	for _, worker := range reg.Progress() {
		if worker.Running {
			receipt.WorkerIDs = append(receipt.WorkerIDs, worker.AgentID)
		}
	}
	body, _ := json.Marshal(receipt)
	e := rt.engine
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	ev := store.Event{ThreadID: rt.threadID, TurnID: turn.ID, AgentID: swarm.DefaultManagerID, Kind: KindWorkersRetained, Text: string(body)}
	if err := e.store.AppendEvent(&ev); err != nil {
		e.log.Warn("could not save worker ownership after a failed check", "turn", turn.ID, "err", err)
		return false
	}
	rt.parked = reg
	e.broadcast(ev)
	return true
}

// Wake revocation may have observed a running manager just before it parked.
// Recheck after release so either finalization or the revoker reaps the registry.
func (rt *runtime) reapUnownedParked() {
	rt.mu.Lock()
	if rt.running || rt.parked == nil || rt.abandoned {
		rt.mu.Unlock()
		return
	}
	_, owner := rt.workerOwner()
	if !rt.userStopped && owner != "" {
		rt.mu.Unlock()
		return
	}
	reg, turnID := rt.parked, rt.turnID
	rt.recordParkedStop(reg, turnID)
	rt.parked = nil
	rt.mu.Unlock()
	reg.Cleanup()
	reg.Close()
}

// Called under rt.mu: neither a new registry nor late finished events may
// overtake this conversation-wide stop receipt.
func (rt *runtime) recordParkedStop(reg *swarm.Registry, turnID string) {
	if liveWorkers(reg.Progress()) > 0 {
		rt.engine.record(store.Event{ThreadID: rt.threadID, TurnID: turnID, Kind: KindCleanup, AgentID: swarm.DefaultManagerID,
			Text: "stopped background workers after their task owner ended or the user stopped them"})
	}
}

func (e *Engine) retainedWorkerIDs(turn *store.Turn) map[string]bool {
	if turn.Status != store.TurnError || !turn.ScheduleContinue {
		return nil
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		return nil
	}
	var ids map[string]bool
	terminal := false
	for _, ev := range events {
		if ev.Kind == KindCleanup || ev.Kind == KindUserStopRequested {
			ids = nil
		}
		if isManagerAgent(ev.AgentID) && (ev.Kind == swarm.NotifyError.String() || ev.Kind == swarm.NotifyDone.String()) {
			terminal = true
		}
		if ev.Kind != KindWorkersRetained || !isManagerAgent(ev.AgentID) || terminal {
			continue
		}
		var receipt workerRetention
		if json.Unmarshal([]byte(ev.Text), &receipt) != nil || receipt.Reason != "scheduled_check_failed" || strings.TrimSpace(receipt.OwnerID) == "" ||
			(receipt.OwnerKind != "goal" && receipt.OwnerKind != "thread_wake") {
			continue
		}
		valid := true
		for _, id := range receipt.WorkerIDs {
			if strings.TrimSpace(id) != id || isManagerAgent(id) {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		ids = make(map[string]bool, len(receipt.WorkerIDs))
		for _, id := range receipt.WorkerIDs {
			ids[id] = true
		}
	}
	return ids
}
