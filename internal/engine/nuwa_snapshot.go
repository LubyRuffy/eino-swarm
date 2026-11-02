package engine

import (
	"context"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// NuwaWorker is a projection of actual worker lifecycle receipts, not a model's
// claim that a task has started or completed.
type NuwaWorker struct {
	AgentID      string              `json:"agent_id"`
	Role         string              `json:"role"`
	Status       string              `json:"status"`
	ThreadID     string              `json:"thread_id"`
	TurnID       string              `json:"turn_id"`
	Generation   int                 `json:"generation"`
	ParentID     string              `json:"parent_id,omitempty"`
	ModelBinding *swarm.ModelBinding `json:"model_binding,omitempty"`
}

// NuwaProjectView combines transactional shared records and durable worker state.
type NuwaProjectView struct {
	ProjectID string           `json:"project_id"`
	Revision  int64            `json:"revision"`
	Nodes     []store.NuwaNode `json:"nodes"`
	Workers   []NuwaWorker     `json:"workers"`
}

// ProjectNuwaSnapshot reads only the selected project's collaboration state.
func (e *Engine) ProjectNuwaSnapshot(ctx context.Context, projectID string) (NuwaProjectView, error) {
	revision, nodes, err := e.store.NuwaSnapshot(ctx, projectID)
	if err != nil {
		return NuwaProjectView{}, err
	}
	view := NuwaProjectView{ProjectID: projectID, Revision: revision, Nodes: nodes, Workers: []NuwaWorker{}}
	var events []store.Event
	err = e.store.DB().WithContext(ctx).Table("events").Select("events.*").Joins("JOIN threads ON threads.id=events.thread_id").
		Where("threads.project_id = ? AND events.kind IN ?", projectID, []string{swarm.NotifySpawned.String(), swarm.NotifyFinished.String(), KindCleanup, KindRestartCheckpoint, swarm.NotifyToolCall.String(), swarm.NotifyToolResult.String()}).
		Order("events.thread_id, events.seq").Find(&events).Error
	if err != nil {
		return NuwaProjectView{}, err
	}
	byThread := map[string][]store.Event{}
	threadOrder := []string{}
	for _, event := range events {
		if _, ok := byThread[event.ThreadID]; !ok {
			threadOrder = append(threadOrder, event.ThreadID)
		}
		byThread[event.ThreadID] = append(byThread[event.ThreadID], event)
	}
	for _, threadID := range threadOrder {
		rows := byThread[threadID]
		origins := map[string]string{}
		stoppedByCheckpoint := map[string]int{}
		if checkpoint, _ := latestRestartCheckpoint(rows); checkpoint != nil && checkpoint.HumanStopped {
			for _, worker := range checkpoint.Workers {
				if !worker.Finished {
					stoppedByCheckpoint[worker.ID] = worker.Generation
				}
			}
		}
		for _, event := range rows {
			if event.Kind == swarm.NotifySpawned.String() {
				origins[event.AgentID] = event.TurnID
			}
		}
		running, finished := orphanedWorkers(rows)
		for _, worker := range running {
			view.Workers = append(view.Workers, NuwaWorker{AgentID: worker.ID, Role: worker.Role, Status: "running", ThreadID: threadID, TurnID: origins[worker.ID], Generation: worker.Generation, ModelBinding: worker.ModelBinding, ParentID: worker.ParentID})
		}
		for _, worker := range finished {
			status := "done"
			if worker.Err != nil {
				status = "failed"
				prefix := "swarm: agent " + worker.ID + " cancelled: "
				checkpointGeneration, checkpointStopped := stoppedByCheckpoint[worker.ID]
				if worker.Err.Error() == cleanedUpWorkerErr ||
					(worker.CloseRequested && worker.Err.Error() == "context canceled") ||
					(checkpointStopped && checkpointGeneration == worker.Generation && worker.Err.Error() == "interrupted") ||
					(strings.HasPrefix(worker.Err.Error(), prefix) && strings.TrimSpace(strings.TrimPrefix(worker.Err.Error(), prefix)) != "") {
					status = "cancelled"
				}
			}
			view.Workers = append(view.Workers, NuwaWorker{AgentID: worker.ID, Role: worker.Role, Status: status, ThreadID: threadID, TurnID: origins[worker.ID], Generation: worker.Generation, ModelBinding: worker.ModelBinding, ParentID: worker.ParentID})
		}
	}
	return view, nil
}

func (e *Engine) invalidateNuwaWorkers(event store.Event) {
	if event.Kind != swarm.NotifySpawned.String() && event.Kind != swarm.NotifyFinished.String() && event.Kind != KindCleanup && event.Kind != KindRestartCheckpoint {
		return
	}
	thread, err := e.store.GetThread(event.ThreadID)
	if err != nil || thread.ProjectID == "" {
		return
	}
	project, err := e.store.GetProject(thread.ProjectID)
	if err != nil || (!project.NuwaEnabled && project.NuwaRevision == 0) {
		return
	}
	if err := e.store.BumpNuwaRevision(context.Background(), project.ID); err != nil {
		e.log.Warn("could not invalidate collaboration worker state", "project", project.ID, "err", err)
	}
}
