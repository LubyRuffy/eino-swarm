package swarm

import "context"

type workerTaskKey struct{}

// WorkerTask returns the task supplied when this worker generation started.
// It is model-authored task data, not authorization or the manager's context.
// Later inbox messages do not rewrite it; a resumed generation gets its new task.
// An unknown legacy assignment returns an empty task with a false presence flag.
func WorkerTask(ctx context.Context) (string, bool) {
	task, ok := ctx.Value(workerTaskKey{}).(string)
	return task, ok && task != ""
}

func withWorkerTask(ctx context.Context, task string) context.Context {
	return context.WithValue(ctx, workerTaskKey{}, task)
}
