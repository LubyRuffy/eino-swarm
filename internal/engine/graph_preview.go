package engine

import (
	"strconv"
	"sync"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// graphPreview is the in-progress frame of a write that has not committed.
// Readers merge it into the snapshot they return. It is not a revision, and a
// refused batch clears it without leaving rows.
type graphPreview struct {
	mu     sync.Mutex
	owner  string
	token  uint64
	active bool
	view   store.GraphView
}

func (e *Engine) showGraphPreview(owner string, view store.GraphView) {
	e.graphPreview.mu.Lock()
	e.graphPreview.token++
	e.graphPreview.owner = owner
	e.graphPreview.active = true
	e.graphPreview.view = view
	e.graphPreview.mu.Unlock()
}

func (e *Engine) clearGraphPreview(owner string) {
	e.graphPreview.mu.Lock()
	if e.graphPreview.owner == owner {
		e.graphPreview.active = false
		e.graphPreview.view = store.GraphView{}
	}
	e.graphPreview.mu.Unlock()
}

// graphPreviewToken is non-zero while owner is showing an uncommitted frame.
func (e *Engine) graphPreviewToken(owner string) uint64 {
	e.graphPreview.mu.Lock()
	defer e.graphPreview.mu.Unlock()
	if !e.graphPreview.active || e.graphPreview.owner != owner {
		return 0
	}
	return e.graphPreview.token
}

func (e *Engine) overlayGraphPreview(owner string, view *store.GraphView, q store.GraphQuery) {
	if !q.Preview || q.ID != "" || q.Text != "" || q.Status != "" || q.Offset != 0 || q.Limit >= 0 {
		return
	}
	e.graphPreview.mu.Lock()
	defer e.graphPreview.mu.Unlock()
	if !e.graphPreview.active || e.graphPreview.owner != owner {
		return
	}
	frame := e.graphPreview.view
	if frame.Revision != view.Revision {
		return
	}
	if q.All || !q.Todos {
		view.Subjects = append([]store.GraphSubject(nil), frame.Subjects...)
		view.Links = append([]store.GraphLink(nil), frame.Links...)
		view.Total = frame.Total
	}
	if q.All || q.Todos {
		if q.Todos {
			view.Total += len(frame.Todos) - len(view.Todos)
		}
		view.Todos = append([]store.GraphTodo(nil), frame.Todos...)
	}
	view.More = view.More || frame.More
}

// previewGraph is the stored picture plus the batch that is about to commit.
// The first new done to-do is drawn in progress. New subjects are drawn
// without credential values. Rows already stored stay as they are.
func previewGraph(base store.GraphView, batch graph.Batch) store.GraphView {
	catalog := graph.MustDefault()
	view := base
	view.Subjects = append([]store.GraphSubject(nil), base.Subjects...)
	view.Links = append([]store.GraphLink(nil), base.Links...)
	view.Todos = append([]store.GraphTodo(nil), base.Todos...)
	held := false
	for i, todo := range batch.Todos {
		if todo.ID != "" {
			continue
		}
		if len(view.Todos) == store.GraphMaxRows {
			view.More = true
			break
		}
		status := todo.Status
		if status == "" {
			status = graph.TodoOpen
		}
		if !held && status == graph.TodoDone {
			status = graph.TodoDoing
			held = true
		}
		view.Todos = append(view.Todos, store.GraphTodo{
			ID: previewID("", todo.Ref, "todo", i), Title: todo.Title, Status: status, Result: todo.Result, Position: len(view.Todos),
		})
	}
	for i, subject := range batch.Subjects {
		if subject.HostRetract && subject.ID != "" {
			for j := range view.Subjects {
				if view.Subjects[j].ID == subject.ID && view.Subjects[j].Status == graph.StatusLead {
					view.Subjects[j].Status = graph.StatusRetracted
				}
			}
		}
		if subject.ID != "" {
			continue
		}
		if len(view.Subjects) == store.GraphMaxRows {
			view.More = true
			break
		}
		kind := catalog.Kind(subject.Kind)
		if kind == nil {
			continue
		}
		cleaned, err := kind.Clean(subject.Attrs)
		if err != nil {
			continue
		}
		attrs, credentials := catalog.Redact(subject.Kind, cleaned)
		status := subject.Status
		if status == "" {
			status = graph.StatusLead
		}
		view.Subjects = append(view.Subjects, store.GraphSubject{
			ID: previewID("", subject.Ref, "subject", i), OwnerID: base.OwnerID, Kind: subject.Kind,
			Name: kind.DisplayName(attrs), Status: status, Attrs: attrs, Credentials: credentials,
		})
		view.Total++
	}
	for _, link := range batch.Links {
		if link.From == "" && link.To == "" {
			continue
		}
		view.Links = append(view.Links, store.GraphLink{FromID: link.From, ToID: link.To, Rel: link.Rel})
	}
	return view
}

func previewID(id, ref, kind string, i int) string {
	if id != "" {
		return id
	}
	if ref != "" {
		return ref
	}
	return "preview-" + kind + "-" + strconv.Itoa(i)
}
