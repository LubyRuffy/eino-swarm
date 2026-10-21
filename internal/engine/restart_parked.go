package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/components/tool"
)

// ActiveWork keeps a process alive while a manager, an independent parked
// worker, or a restart handoff owns work. A future wake alone owns no execution.
func (e *Engine) ActiveWork() int {
	n := 0
	for _, rt := range e.snapshotRuntimes() {
		if st := rt.status(); st.Running || st.Workers > 0 {
			n++
		}
	}
	if e.GetRestartStatus().Stage != "" {
		n++
	}
	return n
}

// ResumeParkedWorkers restarts workers whose manager had already yielded to
// a future wake. It retains that completed turn and its trace, without firing
// the wake or granting another manager turn across a human stop/pause.
func (e *Engine) ResumeParkedWorkers() (int, error) {
	done, err := e.beginAdmission()
	if err != nil {
		return 0, err
	}
	defer done()
	threads, err := e.store.ListThreads(true, "")
	if err != nil {
		return 0, err
	}
	n := 0
	var failures []error
	for _, th := range threads {
		count, err := e.resumeParkedThread(&th)
		n += count
		if err != nil {
			failures = append(failures, fmt.Errorf("restore parked workers for %s: %w", th.ID, err))
		}
	}
	return n, errors.Join(failures...)
}

func (e *Engine) resumeParkedThread(th *store.Thread) (int, error) {
	turns, err := e.store.ListTurns(th.ID)
	if err != nil || len(turns) == 0 {
		return 0, err
	}
	turn := &turns[len(turns)-1]
	retained := e.retainedWorkerIDs(turn)
	if turn.Status != store.TurnDone && len(retained) == 0 {
		return 0, nil
	}
	checkpoint := e.checkpointForTurn(turn.ID)
	if checkpoint == nil || checkpoint.HumanStopped || checkpoint.Pause != "" || e.turnHasKind(turn.ID, KindUserStopRequested) {
		return 0, nil
	}
	rt := e.runtimeFor(th.ID)
	if _, owner := rt.workerOwner(); owner == "" {
		return 0, nil
	}
	running, finished, err := e.readWorkersFromThread(th.ID)
	if err != nil || len(running) == 0 {
		return 0, err
	}
	if turn.Status == store.TurnError {
		owned := running[:0]
		for _, worker := range running {
			if retained[worker.ID] {
				owned = append(owned, worker)
			}
		}
		running = owned
		if len(running) == 0 {
			return 0, nil
		}
	}
	reg, err := e.parkedRegistry(th, turn)
	if err != nil {
		return 0, err
	}
	rt.mu.Lock()
	if rt.running || rt.parked != nil || rt.userStopped {
		rt.mu.Unlock()
		reg.Close()
		return 0, nil
	}
	rt.parked, rt.turnID, rt.abandoned = reg, turn.ID, false
	rt.mu.Unlock()
	reg.SetHistory(checkpoint.ManagerHistory)
	for _, msg := range checkpoint.ManagerInbox {
		reg.SteerManagerMessage(msg)
	}
	attachHostNotify(reg, newAccumulator(e, th.ID, turn.ID, e.cfg.Swarm.DeltaCoalesce()))
	workerTools := append(append([]tool.BaseTool(nil), reg.SubAgentTools...), reg.SendTool())
	for _, w := range finished {
		if err := reg.PlantFinished(w); err != nil {
			rt.reapParked()
			return 0, err
		}
	}
	for _, w := range running {
		if _, err := reg.Restore(context.Background(), w, reg.ModelBuilder, workerTools...); err != nil {
			rt.reapParked()
			return 0, err
		}
	}
	rt.reapUnownedParked()
	return len(running), nil
}

func (e *Engine) parkedRegistry(th *store.Thread, turn *store.Turn) (*swarm.Registry, error) {
	pc, err := e.projectContextFor(th)
	if err != nil {
		return nil, err
	}
	set, err := tools.Build(context.Background(), e.cfg, e.WorkspaceDir(th.ID))
	if err != nil {
		return nil, err
	}
	if th.PlanMode {
		set = tools.ExploreOnly(set)
	}
	providerID := strings.TrimSpace(turn.ProviderID)
	if providerID == "" {
		providerID = th.ProviderID
	}
	prov, err := e.pool.ResolveModel(providerID, turn.Model)
	if err != nil {
		return nil, err
	}
	effort := turn.ReasoningEffort
	if effort == "" {
		effort = th.ReasoningEffort
	}
	builder, err := e.pool.ModelBuilder(context.Background(), prov.ID, prov.Model, effort, e.callRecorder(th.ID, turn.ID))
	if err != nil {
		return nil, err
	}
	return e.newTurnRegistry(builder, set, pc), nil
}
