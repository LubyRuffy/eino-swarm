package store

import (
	"errors"
	"strings"
	"testing"
)

func TestNuwaSharedReadRequiresItsRecordedTurnProject(t *testing.T) {
	f := newNuwaFixture(t)
	node := f.node("fact")
	if err := f.s.UpsertNuwaNode(t.Context(), &node, 0); err != nil {
		t.Fatal(err)
	}
	// The thread is in this project, but this durable turn was standalone.
	if _, nodes, err := f.s.NuwaSnapshotForTurn(t.Context(), f.project.ID, f.thread.ID, f.turn.ID); len(nodes) != 0 ||
		!errors.Is(err, ErrInvalidNuwaNode) || !strings.Contains(err.Error(), "source turn belongs to another project") {
		t.Fatalf("mismatched durable turn read: nodes=%+v err=%v", nodes, err)
	}
	if err := f.s.DB().Model(&f.turn).Update("project_id", f.project.ID).Error; err != nil {
		t.Fatal(err)
	}
	revision, nodes, err := f.s.NuwaSnapshotForTurn(t.Context(), f.project.ID, f.thread.ID, f.turn.ID)
	if err != nil || revision != 1 || len(nodes) != 1 || nodes[0].ID != node.ID {
		t.Fatalf("matching durable turn read: revision=%d nodes=%+v err=%v", revision, nodes, err)
	}
}
