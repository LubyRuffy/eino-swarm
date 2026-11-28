package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
)

func TestClearGraphRemovesThePictureAndAdvancesTheRevision(t *testing.T) {
	f := newGraphFixture(t, true)
	caller := f.caller(f.thread, f.turn, "manager")
	f.apply(graph.Batch{Subjects: []graph.SubjectInput{{Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.9"}}}})
	before, err := f.s.GraphRevisionOf(context.Background(), f.owner)
	if err != nil || before < 1 {
		t.Fatal(before, err)
	}
	if err := f.s.ClearGraph(context.Background(), caller); err != nil {
		t.Fatal(err)
	}
	view, err := f.s.ReadGraph(context.Background(), f.owner, GraphQuery{All: true, Limit: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Subjects) != 0 || len(view.Links) != 0 || len(view.Todos) != 0 || view.Revision != before+1 {
		t.Fatalf("after clear: rev=%d subjects=%d links=%d todos=%d", view.Revision, len(view.Subjects), len(view.Links), len(view.Todos))
	}
	again, err := f.s.GraphRevisionOf(context.Background(), f.owner)
	if err != nil || again != view.Revision {
		t.Fatal(again, err)
	}
	empty := newGraphFixture(t, false)
	if err := empty.s.ClearGraph(context.Background(), empty.caller(empty.thread, empty.turn, "manager")); err != nil {
		t.Fatal(err)
	}
	if rev, err := empty.s.GraphRevisionOf(context.Background(), empty.owner); err != nil || rev != 0 {
		t.Fatal(rev, err)
	}
	if err := f.s.ClearGraph(context.Background(), GraphCaller{}); err == nil {
		t.Fatal("a clear without an owner was accepted")
	}
	if err := f.s.ClearGraph(context.Background(), caller); err != nil {
		t.Fatal(err)
	}
	// A second clear still moves the revision, so a client holding the empty
	// picture at the previous number cannot keep a stale frame.
	if rev, err := f.s.GraphRevisionOf(context.Background(), f.owner); err != nil || rev != again+1 {
		t.Fatal(rev, err)
	}
}

func TestGraphClearKeepsRowsAndReplayReceiptWhenRevisionWriteFails(t *testing.T) {
	f := newGraphFixture(t, true)
	ctx := t.Context()
	batch := graph.Batch{
		Subjects: []graph.SubjectInput{
			{Ref: "name", Kind: "domain", Attrs: map[string]any{"domain": "project.test"}},
			{Ref: "address", Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.7"}},
		},
		Links: []graph.LinkInput{{From: "name", To: "address", Rel: "resolves"}},
		Todos: []graph.TodoInput{{Title: "Verify observation", About: "name"}},
	}
	caller := f.caller(f.thread, f.turn, "manager")
	hash, err := HashGraphBatch(batch)
	if err != nil {
		t.Fatal(err)
	}
	caller.ReceiptKey, caller.ReceiptHash = "clear-rollback-receipt", hash
	committed, replayed, err := f.s.ApplyGraph(ctx, caller, batch)
	if err != nil || replayed {
		t.Fatalf("initial write replayed=%v: %v", replayed, err)
	}
	before := f.read(GraphQuery{All: true})
	// Refuse the final write after ClearGraph has issued every deletion.
	if err := f.s.DB().Exec(`CREATE TRIGGER reject_clear_revision BEFORE INSERT ON graph_revisions
		WHEN NEW.payload = '{}' BEGIN SELECT RAISE(ABORT, 'clear revision refused'); END`).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.s.ClearGraph(ctx, caller); err == nil || !strings.Contains(err.Error(), "clear revision refused") {
		t.Fatalf("clear returned %v, want the injected revision failure", err)
	}
	if after := f.read(GraphQuery{All: true}); !reflect.DeepEqual(after, before) {
		t.Fatalf("failed clear mutated the graph: before=%+v after=%+v", before, after)
	}
	var keys int64
	if err := f.s.DB().Model(&GraphSubjectKey{}).Where("owner_id = ?", f.owner).Count(&keys).Error; err != nil || keys != 2 {
		t.Fatalf("failed clear left %d identity keys: %v", keys, err)
	}
	frozen, replayed, err := f.s.ApplyGraph(ctx, caller, batch)
	if err != nil || !replayed || !reflect.DeepEqual(committed, frozen) {
		t.Fatalf("failed clear lost the old receipt: result=%+v replayed=%v err=%v", frozen, replayed, err)
	}
}

func TestGraphClearRefusesAStaleOwnerPlanningOrCancelledTurn(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*graphFixture) error
	}{
		{"moved conversation", func(f *graphFixture) error {
			return f.s.UpdateThread(f.thread.ID, map[string]any{"project_id": ""})
		}},
		{"planning conversation", func(f *graphFixture) error {
			return f.s.UpdateThread(f.thread.ID, map[string]any{"plan_mode": true})
		}},
		{"cancelled turn", func(f *graphFixture) error {
			return f.s.FinishTurn(f.turn.ID, TurnCancelled, "", "")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newGraphFixture(t, true)
			f.apply(graph.Batch{Todos: []graph.TodoInput{{Title: "Keep the existing review"}}})
			if err := tc.prepare(f); err != nil {
				t.Fatal(err)
			}
			caller := f.caller(f.thread, f.turn, "manager")
			if err := f.s.ClearGraph(t.Context(), caller); !errors.Is(err, graph.ErrInvalid) {
				t.Fatalf("clear refused as %v", err)
			}
			view := f.read(GraphQuery{All: true})
			if view.Revision != 1 || len(view.Todos) != 1 || view.Todos[0].Title != "Keep the existing review" {
				t.Fatalf("refused clear changed the graph: %+v", view)
			}
		})
	}
}
