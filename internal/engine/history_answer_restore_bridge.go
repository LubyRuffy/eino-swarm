package engine

import (
	"context"
	"fmt"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// leftoverWorkerPlan contains only already-read recovery state. Preparing it
// never starts a worker or changes runtime ownership; claim can therefore fail
// before detaching a parked registry or occupying a new one.
type leftoverWorkerPlan struct {
	running  []swarm.RestoredWorker
	finished []swarm.FinishedWorker
}

func (rt *runtime) prepareLeftoverWorkers(ctx context.Context, turn *store.Turn, snapshots ...*restoreSnapshot) (*leftoverWorkerPlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if rt == nil || rt.engine == nil || turn == nil || turn.ThreadID != rt.threadID {
		return nil, store.ErrHistoryRegistryInheritanceInvalid
	}
	snapshot := firstRestore(snapshots)
	if snapshot == nil || snapshot.threadID != turn.ThreadID {
		var err error
		snapshot, err = rt.engine.readRestoreSnapshot(ctx, turn.ThreadID)
		if err != nil {
			return nil, err
		}
	}
	owners, err := leftoverWorkerOwners(snapshot)
	if err != nil {
		return nil, err
	}
	owner, err := rt.engine.store.ProvenHistoryRegistryOwner(ctx, turn.ThreadID, owners)
	if err != nil {
		return nil, err
	}
	if owner != "" && owner != turn.ID {
		if err := rt.engine.recordHistoryRegistryInheritance(ctx, turn.ThreadID, turn.ID, owner); err != nil {
			return nil, err
		}
	}
	return &leftoverWorkerPlan{running: snapshot.running, finished: snapshot.finished}, nil
}

// A checkpoint owns exactly the worker state it captured. A later actual spawn
// identifies the owner of that worker's new generation. Fully collected or
// explicitly discarded archive entries do not attach executable/unread work to
// an unrelated fresh registry.
func leftoverWorkerOwners(snapshot *restoreSnapshot) ([]string, error) {
	owners := map[string]string{}
	start := 0
	if snapshot.checkpoint != nil {
		if snapshot.checkpointEvent == nil {
			return nil, store.ErrHistoryRegistryInheritanceInvalid
		}
		for _, worker := range snapshot.checkpoint.Workers {
			owners[worker.ID] = snapshot.checkpointEvent.TurnID
		}
		for i, event := range snapshot.events {
			if event.Seq == snapshot.checkpointEvent.Seq {
				start = i + 1
				break
			}
		}
	}
	for _, event := range snapshot.events[start:] {
		if event.Kind == swarm.NotifySpawned.String() && !isManagerAgent(event.AgentID) {
			owners[event.AgentID] = event.TurnID
		}
	}
	var sources []string
	seen := map[string]bool{}
	accept := func(id string) error {
		source := owners[id]
		if source == "" {
			return fmt.Errorf("recover worker %s without a recorded registry owner: %w", id, store.ErrHistoryRegistryInheritanceInvalid)
		}
		if !seen[source] {
			sources = append(sources, source)
			seen[source] = true
		}
		return nil
	}
	for _, worker := range snapshot.running {
		if err := accept(worker.ID); err != nil {
			return nil, err
		}
	}
	for _, worker := range snapshot.finished {
		if !worker.Collected && !worker.CloseRequested {
			if err := accept(worker.ID); err != nil {
				return nil, err
			}
		}
	}
	return sources, nil
}
