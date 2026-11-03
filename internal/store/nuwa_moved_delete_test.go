package store

import (
	"errors"
	"reflect"
	"testing"
)

func TestNuwaMovedDeleteCleansEveryOriginAndPreservesOtherAuthors(t *testing.T) {
	f := newNuwaFixture(t)
	ctx := t.Context()
	original := f.node("fact")
	if err := f.s.UpsertNuwaNode(ctx, &original, 0, NuwaWriteReceipt{Key: "first origin", Hash: "first content"}); err != nil {
		t.Fatal(err)
	}
	other := Thread{Title: "remaining author", ProjectID: f.project.ID}
	if err := f.s.CreateThread(&other); err != nil {
		t.Fatal(err)
	}
	otherTurn := Turn{ThreadID: other.ID}
	if err := f.s.CreateTurn(&otherTurn); err != nil {
		t.Fatal(err)
	}
	retained := f.node("fact")
	retained.ThreadID, retained.TurnID = other.ID, otherTurn.ID
	retained.DependsOn = []string{original.ID}
	retained.Evidence = []NuwaEvidence{{ThreadID: f.thread.ID, TurnID: f.turn.ID, Seq: f.event.Seq}}
	if err := f.s.UpsertNuwaNode(ctx, &retained, 0); err != nil {
		t.Fatal(err)
	}
	// This is a correction to someone else's node. Deleting its editor must
	// not delete the node or its project-owned correction history.
	correction := retained
	correction.ThreadID, correction.TurnID = f.thread.ID, f.turn.ID
	correction.Summary = "still a shared observation"
	if err := f.s.UpsertNuwaNode(ctx, &correction, 1, NuwaWriteReceipt{Key: "shared correction", Hash: "new shared content"}); err != nil {
		t.Fatal(err)
	}

	projects := []Project{
		f.project,
		{Name: "second origin", NuwaEnabled: true},
		{Name: "current project", NuwaEnabled: true},
		{Name: "unrelated project", NuwaEnabled: true},
	}
	for i := 1; i < len(projects); i++ {
		if err := f.s.CreateProject(&projects[i]); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.s.SetThreadProject(f.thread.ID, projects[1].ID); err != nil {
		t.Fatal(err)
	}
	secondTurn := Turn{ThreadID: f.thread.ID}
	if err := f.s.CreateTurn(&secondTurn); err != nil {
		t.Fatal(err)
	}
	second := f.node("fact")
	second.ProjectID, second.TurnID = projects[1].ID, secondTurn.ID
	if err := f.s.UpsertNuwaNode(ctx, &second, 0, NuwaWriteReceipt{Key: "second origin", Hash: "second content"}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.SetThreadProject(f.thread.ID, projects[2].ID); err != nil {
		t.Fatal(err)
	}

	snapshot := func(projectID string) (int64, []NuwaNode) {
		t.Helper()
		rev, nodes, err := f.s.NuwaSnapshot(ctx, projectID)
		if err != nil {
			t.Fatal(err)
		}
		return rev, nodes
	}
	revisions := func(nodeID string) []NuwaRevision {
		t.Helper()
		var rows []NuwaRevision
		if err := f.s.DB().Where("node_id = ?", nodeID).Order("id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		return rows
	}
	var before []int64
	for _, project := range projects {
		revision, _ := snapshot(project.ID)
		before = append(before, revision)
	}
	originalHistory, secondHistory := revisions(original.ID), revisions(second.ID)
	otherHistory := revisions(retained.ID)
	if len(originalHistory) != 1 || len(secondHistory) != 1 || len(otherHistory) != 2 {
		t.Fatalf("fixture has incomplete revision history: %d %d %d", len(originalHistory), len(secondHistory), len(otherHistory))
	}

	failed := failNuwaOperation(t, f.s, "create", "nuwa_revisions", 2)
	if err := f.s.DeleteThread(f.thread.ID); err == nil || !*failed {
		t.Fatalf("delete accepted a failed cross-project invalidation: fired=%v err=%v", *failed, err)
	}
	if thread, err := f.s.GetThread(f.thread.ID); err != nil || thread.ProjectID != projects[2].ID {
		t.Fatalf("failed delete lost its owner: thread=%+v err=%v", thread, err)
	}
	for i, project := range projects {
		if revision, _ := snapshot(project.ID); revision != before[i] {
			t.Fatalf("failed delete changed %s revision: got %d, want %d", project.Name, revision, before[i])
		}
	}
	if !reflect.DeepEqual(revisions(original.ID), originalHistory) || !reflect.DeepEqual(revisions(second.ID), secondHistory) ||
		!reflect.DeepEqual(revisions(retained.ID), otherHistory) {
		t.Fatal("failed delete changed private or shared history")
	}

	if err := f.s.DeleteThread(f.thread.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.GetThread(f.thread.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted conversation still has an owner: err=%v", err)
	}
	for i, project := range projects {
		revision, nodes := snapshot(project.ID)
		want := before[i]
		if i < 3 {
			want++
		}
		if revision != want {
			t.Fatalf("delete revision for %s: got %d, want %d", project.Name, revision, want)
		}
		if i != 0 {
			if len(nodes) != 0 {
				t.Fatalf("unexpected nodes remaining in %s: %+v", project.Name, nodes)
			}
			continue
		}
		if len(nodes) != 1 || nodes[0].ID != retained.ID || nodes[0].Version != 2 ||
			nodes[0].UpdatedThreadID != f.thread.ID || nodes[0].Summary != correction.Summary ||
			!reflect.DeepEqual(nodes[0].MissingDependencies, []string{original.ID}) ||
			!reflect.DeepEqual(nodes[0].UnavailableEvidence, retained.Evidence) {
			t.Fatalf("delete lost the surviving node or its unavailable source: %+v", nodes)
		}
	}
	if len(revisions(original.ID)) != 0 || len(revisions(second.ID)) != 0 ||
		!reflect.DeepEqual(revisions(retained.ID), otherHistory) {
		t.Fatal("delete retained origin-owned payloads or changed the other author's history")
	}
}
