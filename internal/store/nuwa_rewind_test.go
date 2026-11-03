package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestNuwaRewindInvalidatesEvidenceWithoutRemovingItsReference(t *testing.T) {
	for _, mode := range []struct {
		name    string
		enabled bool
	}{
		{name: "enabled", enabled: true},
		{name: "disabled with history"},
	} {
		t.Run(mode.name, func(t *testing.T) {
			f := newNuwaFixture(t)
			ctx := context.Background()
			node := f.node("fact")
			node.Evidence = []NuwaEvidence{{ThreadID: f.thread.ID, TurnID: f.turn.ID, Seq: f.event.Seq}}
			if err := f.s.UpsertNuwaNode(ctx, &node, 0); err != nil {
				t.Fatal(err)
			}
			if !mode.enabled {
				if err := f.s.UpdateProject(f.project.ID, map[string]any{"nuwa_enabled": false}); err != nil {
					t.Fatal(err)
				}
			}
			before, _, err := f.s.NuwaSnapshot(ctx, f.project.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.s.TruncateFromEventSeq(f.thread.ID, f.event.Seq); err != nil {
				t.Fatal(err)
			}
			after, nodes, err := f.s.NuwaSnapshot(ctx, f.project.ID)
			if err != nil || after != before+1 || len(nodes) != 1 || nodes[0].ID != node.ID ||
				nodes[0].Version != node.Version || !reflect.DeepEqual(nodes[0].Evidence, node.Evidence) ||
				!reflect.DeepEqual(nodes[0].UnavailableEvidence, node.Evidence) {
				t.Fatalf("rewind revision %d -> %d, nodes %+v, error %v", before, after, nodes, err)
			}
			var receipt NuwaRevision
			if err := f.s.DB().First(&receipt, "project_id = ? AND revision = ?", f.project.ID, after).Error; err != nil ||
				receipt.NodeID != "" || receipt.Payload != "{}" {
				t.Fatalf("rewind invalidation receipt %+v, error %v", receipt, err)
			}
			if err := f.s.TruncateFromEventSeq(f.thread.ID, f.event.Seq); !errors.Is(err, ErrNotFound) {
				t.Fatalf("repeated cut must report absent source: %v", err)
			}
			if rev, _, err := f.s.NuwaSnapshot(ctx, f.project.ID); err != nil || rev != after {
				t.Fatalf("repeated cut changed revision %d -> %d, error %v", after, rev, err)
			}
		})
	}
}

func TestNuwaRewindUsesTheConversationCurrentProject(t *testing.T) {
	f := newNuwaFixture(t)
	destination := Project{Name: "destination", NuwaEnabled: true}
	if err := f.s.CreateProject(&destination); err != nil {
		t.Fatal(err)
	}
	if err := f.s.SetThreadProject(f.thread.ID, destination.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.TruncateFromEventSeq(f.thread.ID, f.event.Seq); err != nil {
		t.Fatal(err)
	}
	for projectID, want := range map[string]int64{f.project.ID: 1, destination.ID: 2} {
		if rev, _, err := f.s.NuwaSnapshot(context.Background(), projectID); err != nil || rev != want {
			t.Fatalf("project %s revision %d, want %d, error %v", projectID, rev, want, err)
		}
	}
}

func TestNuwaRewindDoesNotCreateAnOrdinaryCollaborationView(t *testing.T) {
	s := open(t)
	project := Project{Name: "ordinary"}
	if err := s.CreateProject(&project); err != nil {
		t.Fatal(err)
	}
	thread := Thread{Title: "source", ProjectID: project.ID}
	if err := s.CreateThread(&thread); err != nil {
		t.Fatal(err)
	}
	turn := seedTurn(t, s, thread.ID, "user_message", "original")
	cut, err := firstUserEvent(s, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TruncateFromEventSeq(thread.ID, cut.Seq); err != nil {
		t.Fatal(err)
	}
	if revision, _, err := s.NuwaSnapshot(context.Background(), project.ID); err != nil || revision != 0 {
		t.Fatalf("ordinary rewind created a collaboration revision %d, error %v", revision, err)
	}
}

func TestNuwaRewindRollsBackWhenCollaborationInvalidationFails(t *testing.T) {
	for _, fault := range []struct {
		operation string
		table     string
	}{
		{operation: "query", table: "projects"},
		{operation: "update", table: "projects"},
		{operation: "create", table: "nuwa_revisions"},
	} {
		t.Run(fault.operation+"/"+fault.table, func(t *testing.T) {
			f := newNuwaFixture(t)
			if _, err := f.s.EnqueueFollowup(f.thread.ID, "retain on rollback"); err != nil {
				t.Fatal(err)
			}
			fired := failNuwaOperation(t, f.s, fault.operation, fault.table, 1)
			if err := f.s.TruncateFromEventSeq(f.thread.ID, f.event.Seq); err == nil || !*fired {
				t.Fatalf("invalidation fault did not abort rewind: fired %v, error %v", *fired, err)
			}
			if _, err := f.s.GetEvent(f.thread.ID, f.event.Seq); err != nil {
				t.Fatalf("rewind event was lost on rollback: %v", err)
			}
			if _, err := f.s.GetTurn(f.turn.ID); err != nil {
				t.Fatalf("rewind turn was lost on rollback: %v", err)
			}
			if rows, err := f.s.ListFollowups(f.thread.ID); err != nil || len(rows) != 1 {
				t.Fatalf("followups after rollback %+v, error %v", rows, err)
			}
			if revision, _, err := f.s.NuwaSnapshot(context.Background(), f.project.ID); err != nil || revision != 0 {
				t.Fatalf("revision after rollback %d, error %v", revision, err)
			}
		})
	}
}
