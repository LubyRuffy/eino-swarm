package remote

import (
	"context"
	"encoding/json"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// GraphReply is the phone's read-only graph. Revision is the whole-graph
// change counter; a phone that sends the revision it already holds gets
// Unchanged and no rows, so polling costs one small frame.
type GraphReply struct {
	Revision  int64                `json:"revision"`
	Unchanged bool                 `json:"unchanged,omitempty"`
	Subjects  []store.GraphSubject `json:"subjects,omitempty"`
	Links     []store.GraphLink    `json:"links,omitempty"`
	Todos     []store.GraphTodo    `json:"todos,omitempty"`
	Total     int                  `json:"total,omitempty"`
	More      bool                 `json:"more,omitempty"`
}

// handleGraph answers OpGraph for a conversation or a project. Since carries
// the revision the phone already has. A graph that does not fit one frame is
// refused with graph_too_large rather than cut, so the phone never draws half
// of one.
func handleGraph(eng *engine.Engine, req Request, path, sessionID string) Response {
	if (req.ThreadID == "") == (req.ProjectID == "") || req.Since < 0 {
		return fail(req.ID, path, sessionID, "bad_request", "graph needs exactly one of thread_id and project_id")
	}
	ctx := context.Background()
	var rev int64
	var err error
	if req.ThreadID != "" {
		rev, err = eng.ThreadGraphRevision(ctx, req.ThreadID)
	} else {
		if _, err = eng.GetProject(req.ProjectID); err == nil {
			rev, err = eng.Store().GraphRevisionOf(ctx, req.ProjectID)
		}
	}
	if err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	resp := okBase(req.ID, path, sessionID)
	if req.Since > 0 && req.Since == rev {
		resp.Graph = &GraphReply{Revision: rev, Unchanged: true}
		return resp
	}
	q := store.GraphQuery{Limit: -1, All: true}
	var view store.GraphView
	if req.ThreadID != "" {
		view, err = eng.ReadThreadGraph(ctx, req.ThreadID, q)
	} else {
		view, err = eng.ReadProjectGraph(ctx, req.ProjectID, q)
	}
	if err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	resp.Graph = &GraphReply{Revision: view.Revision, Subjects: view.Subjects, Links: view.Links,
		Todos: view.Todos, Total: view.Total, More: view.More}
	if raw, err := json.Marshal(resp); err != nil || len(raw) > plaintextBudget() {
		return fail(req.ID, path, sessionID, "graph_too_large", "this graph does not fit one phone frame; open it on the desktop")
	}
	return resp
}
