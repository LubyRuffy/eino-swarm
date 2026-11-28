package store

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
	"gorm.io/gorm"
)

var errGraphStorage = errors.New("storage refused")

// failingTable makes every statement on one table fail until it is cleared.
// The callbacks stay registered until the Store closes, so they only act while
// the flag names a table; no background actor writes these tables.
type failingTable struct{ name atomic.Value }

func newFailingTable(s *Store) *failingTable {
	f := &failingTable{}
	f.name.Store("")
	fail := func(db *gorm.DB) {
		if name, _ := f.name.Load().(string); name != "" && db.Statement.Table == name {
			_ = db.AddError(errGraphStorage)
		}
	}
	_ = s.db.Callback().Query().Before("gorm:query").Register("graph-test:query", fail)
	_ = s.db.Callback().Row().Before("gorm:row").Register("graph-test:row", fail)
	_ = s.db.Callback().Create().Before("gorm:create").Register("graph-test:create", fail)
	_ = s.db.Callback().Update().Before("gorm:update").Register("graph-test:update", fail)
	_ = s.db.Callback().Delete().Before("gorm:delete").Register("graph-test:delete", fail)
	return f
}

func (f *failingTable) on(name string) func() { f.name.Store(name); return func() { f.name.Store("") } }

// A storage error must come back to the model as an error, never as an empty
// graph: an empty read would make it believe nothing was found and start over.
func TestGraphReadsReportStorageFailureInsteadOfAnEmptyGraph(t *testing.T) {
	f := newGraphFixture(t, false)
	ev := f.event(f.thread, f.turn, "observed")
	res := f.apply(graph.Batch{
		Subjects: []graph.SubjectInput{{Ref: "ip", Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.7"}}, {Ref: "dom", Kind: "domain", Attrs: map[string]any{"domain": "a.example.com"}}},
		Links:    []graph.LinkInput{{From: "ip", To: "dom", Rel: "serves"}},
		Todos:    []graph.TodoInput{{Title: "check"}},
		Evidence: []graph.Evidence{ev},
	})
	subject := res.Subjects[0].ID
	ctx := context.Background()
	failing := newFailingTable(f.s)
	reads := map[string][]func() error{
		"graph_revisions": {
			func() error { _, err := f.s.ReadGraph(ctx, f.owner, GraphQuery{All: true}); return err },
			func() error { _, err := f.s.GraphRevisionOf(ctx, f.owner); return err },
			func() error { _, err := f.s.ReadGraphDigest(ctx, f.owner, f.thread.ID, f.turn.ID); return err },
		},
		"graph_subjects": {
			func() error { _, err := f.s.ReadGraph(ctx, f.owner, GraphQuery{}); return err },
			func() error { _, err := f.s.ReadGraph(ctx, f.owner, GraphQuery{All: true}); return err },
			func() error { _, err := f.s.ReadGraph(ctx, f.owner, GraphQuery{ID: subject}); return err },
			func() error { _, err := f.s.ReadGraphDigest(ctx, f.owner, f.thread.ID, f.turn.ID); return err },
		},
		"graph_links": {
			func() error { _, err := f.s.ReadGraph(ctx, f.owner, GraphQuery{All: true}); return err },
			func() error { _, err := f.s.ReadGraph(ctx, f.owner, GraphQuery{ID: subject}); return err },
		},
		"graph_todos": {
			func() error { _, err := f.s.ReadGraph(ctx, f.owner, GraphQuery{Todos: true}); return err },
			func() error { _, err := f.s.ReadGraph(ctx, f.owner, GraphQuery{All: true}); return err },
			func() error { _, err := f.s.ReadGraph(ctx, f.owner, GraphQuery{ID: subject}); return err },
			func() error { _, err := f.s.ReadGraphDigest(ctx, f.owner, f.thread.ID, f.turn.ID); return err },
		},
		"events": {
			func() error { _, err := f.s.ReadGraph(ctx, f.owner, GraphQuery{All: true}); return err },
			func() error { _, err := f.s.ReadGraphDigest(ctx, f.owner, f.thread.ID, f.turn.ID); return err },
		},
		"threads": {
			func() error { _, err := f.s.ThreadGraphOwner(ctx, f.thread.ID); return err },
		},
	}
	for table, calls := range reads {
		for i, call := range calls {
			off := failing.on(table)
			err := call()
			off()
			if !errors.Is(err, errGraphStorage) {
				t.Errorf("%s read %d: got %v, want the storage error", table, i, err)
			}
		}
	}
}

func TestGraphWriteIsAtomicWhenStorageFailsMidBatch(t *testing.T) {
	f := newGraphFixture(t, false)
	ev := f.event(f.thread, f.turn, "observed")
	seed := f.apply(graph.Batch{Todos: []graph.TodoInput{{Title: "existing"}}})
	failing := newFailingTable(f.s)
	before := f.read(GraphQuery{All: true})
	todoID := seed.Todos[0].ID
	batch := graph.Batch{
		Subjects: []graph.SubjectInput{{Ref: "ip", Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.7"}}, {Ref: "dom", Kind: "domain", Attrs: map[string]any{"domain": "a.example.com"}}},
		Links:    []graph.LinkInput{{From: "ip", To: "dom", Rel: "serves"}},
		Todos:    []graph.TodoInput{{Title: "new"}, {ID: todoID, Version: 1, Status: graph.TodoDone, Result: "done"}},
		Evidence: []graph.Evidence{ev},
	}
	for _, table := range []string{"graph_subjects", "graph_subject_keys", "graph_links", "graph_todos", "graph_revisions"} {
		off := failing.on(table)
		err := f.applyErr(batch)
		off()
		if !errors.Is(err, errGraphStorage) {
			t.Errorf("%s: got %v, want the storage error", table, err)
		}
		after := f.read(GraphQuery{All: true})
		if after.Revision != before.Revision || len(after.Subjects) != 0 || len(after.Links) != 0 || len(after.Todos) != 1 || after.Todos[0].Status != graph.TodoOpen {
			t.Fatalf("%s: a refused write left rows behind: %+v", table, after)
		}
	}
	f.apply(batch)
}

func TestDeletingAnOwnerReportsAFailedGraphTableInsteadOfLeavingFindings(t *testing.T) {
	f := newGraphFixture(t, false)
	failing := newFailingTable(f.s)
	for _, table := range []string{"graph_subjects", "graph_subject_keys", "graph_links", "graph_todos", "graph_revisions"} {
		off := failing.on(table)
		err := f.s.db.Transaction(func(tx *gorm.DB) error { return deleteGraphOwners(tx, []string{f.owner}) })
		off()
		if !errors.Is(err, errGraphStorage) {
			t.Errorf("%s: got %v", table, err)
		}
	}
	if err := f.s.db.Transaction(func(tx *gorm.DB) error { return deleteGraphOwners(tx, nil) }); err != nil {
		t.Fatalf("deleting no owners is a no-op: %v", err)
	}
}

func TestUnknownConversationHasNoGraphOwner(t *testing.T) {
	f := newGraphFixture(t, false)
	if _, err := f.s.ThreadGraphOwner(context.Background(), "th_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

// A to-do can be created already settled, reopened later, repointed at another
// subject, and refused with a precise cause when its target or order is unknown.
func TestTodoCreatedSettledReopenedAndRepointed(t *testing.T) {
	f := newGraphFixture(t, false)
	ev := f.event(f.thread, f.turn, "observed")
	res := f.apply(graph.Batch{
		Subjects: []graph.SubjectInput{{Ref: "a", Kind: "domain", Attrs: map[string]any{"domain": "a.example.com"}}, {Ref: "b", Kind: "domain", Attrs: map[string]any{"domain": "b.example.com"}}},
		Links:    []graph.LinkInput{{From: "a", To: "b", Rel: "related"}},
		Todos: []graph.TodoInput{
			{Ref: "done", Title: "already known", Status: graph.TodoDone, About: "a"},
			{Ref: "failed", Title: "tried", Status: graph.TodoFailed, Result: "no access"},
		},
		Evidence: []graph.Evidence{ev},
	})
	todos := f.read(GraphQuery{Todos: true}).Todos
	if len(todos) != 2 || todos[0].SettledAt == nil || len(todos[0].Evidence) != 1 || todos[1].SettledAt == nil || todos[1].Result != "no access" {
		t.Fatalf("settled-at-creation to-dos: %+v", todos)
	}
	doneID, version := res.Todos[0].ID, res.Todos[0].Version
	subjectB := res.Subjects[1].ID

	if err := f.applyErr(graph.Batch{Todos: []graph.TodoInput{{ID: "td_missing", Version: 1, Status: graph.TodoDoing}}}); !errors.Is(err, graph.ErrInvalid) {
		t.Errorf("unknown to-do: %v", err)
	}
	if err := f.applyErr(graph.Batch{Todos: []graph.TodoInput{{ID: doneID, Version: version, About: "nope"}}}); !errors.Is(err, graph.ErrInvalid) {
		t.Errorf("unknown about: %v", err)
	}
	if err := f.applyErr(graph.Batch{Todos: []graph.TodoInput{{ID: doneID, Before: "nope"}}}); !errors.Is(err, graph.ErrInvalid) {
		t.Errorf("unknown before: %v", err)
	}
	reopened := f.apply(graph.Batch{Todos: []graph.TodoInput{{ID: doneID, Version: version, Status: graph.TodoOpen, About: subjectB, Title: "recheck"}}})
	if reopened.Todos[0].Status != graph.TodoOpen || reopened.Todos[0].Version != version+1 {
		t.Fatalf("reopen result: %+v", reopened.Todos[0])
	}
	got := f.read(GraphQuery{ID: subjectB}).Todos
	if len(got) != 1 || got[0].Title != "recheck" || got[0].SettledAt != nil {
		t.Fatalf("a reopened to-do must read as unsettled and follow its new subject: %+v", got)
	}
	// Settling again keeps the earlier evidence and adds the new.
	ev2 := f.event(f.thread, f.turn, "second")
	f.apply(graph.Batch{Todos: []graph.TodoInput{{ID: doneID, Version: version + 1, Status: graph.TodoDone}}, Evidence: []graph.Evidence{ev2}})
	if got := f.read(GraphQuery{ID: subjectB}).Todos; len(got) != 1 || len(got[0].Evidence) != 2 {
		t.Fatalf("evidence after settling twice: %+v", got)
	}
}
