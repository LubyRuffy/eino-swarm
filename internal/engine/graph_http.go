package engine

import (
	"context"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// ReadThreadGraph is the panel's view of a conversation's graph. Credentials
// are named and withheld here: the desktop masks them, the phone never gets
// them, and only the model's by-id read sees a value.
func (e *Engine) ReadThreadGraph(ctx context.Context, threadID string, q store.GraphQuery) (store.GraphView, error) {
	owner, err := e.store.ThreadGraphOwner(ctx, threadID)
	if err != nil {
		return store.GraphView{}, err
	}
	return e.readGraphOwner(ctx, owner, q)
}

// ReadProjectGraph is the same for a project's shared graph.
func (e *Engine) ReadProjectGraph(ctx context.Context, projectID string, q store.GraphQuery) (store.GraphView, error) {
	if _, err := e.store.GetProject(projectID); err != nil {
		return store.GraphView{}, err
	}
	return e.readGraphOwner(ctx, projectID, q)
}

func (e *Engine) readGraphOwner(ctx context.Context, owner string, q store.GraphQuery) (store.GraphView, error) {
	view, err := e.store.ReadGraph(ctx, owner, q)
	if err != nil {
		return store.GraphView{}, err
	}
	catalog := graph.MustDefault()
	for i := range view.Subjects {
		view.Subjects[i].Attrs, view.Subjects[i].Credentials = catalog.Redact(view.Subjects[i].Kind, view.Subjects[i].Attrs)
	}
	return view, nil
}

// ThreadGraphRevision is the cheap "did anything change" probe the stream and
// the phone poll with.
func (e *Engine) ThreadGraphRevision(ctx context.Context, threadID string) (int64, error) {
	owner, err := e.store.ThreadGraphOwner(ctx, threadID)
	if err != nil {
		return 0, err
	}
	return e.store.GraphRevisionOf(ctx, owner)
}
