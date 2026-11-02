package store

import (
	"context"
	"sync"
	"testing"
)

func TestNuwaModeRevisionAdvancesOnlyForARealModeChange(t *testing.T) {
	f := newNuwaFixture(t)
	for _, step := range []struct {
		patch map[string]any
		on    bool
		rev   int64
	}{
		{map[string]any{"name": "Ordinary rename"}, true, 0},
		{map[string]any{"nuwa_enabled": true}, true, 0},
		{map[string]any{"nuwa_enabled": false}, false, 1},
		{map[string]any{"nuwa_enabled": false, "name": "Same mode"}, false, 1},
		{map[string]any{"nuwa_enabled": true}, true, 2},
	} {
		if err := f.s.UpdateProject(f.project.ID, step.patch); err != nil {
			t.Fatal(err)
		}
		project, err := f.s.GetProject(f.project.ID)
		if err != nil || project.NuwaEnabled != step.on || project.NuwaRevision != step.rev {
			t.Fatalf("mode patch=%v project=%+v err=%v", step.patch, project, err)
		}
	}
	var history int64
	if err := f.s.DB().Model(&NuwaRevision{}).Where("project_id = ?", f.project.ID).Count(&history).Error; err != nil || history != 0 {
		t.Fatalf("mode invalidation invented node audit versions: count=%d err=%v", history, err)
	}
	// Migrated nullable counters/modes follow the same ordinary-off default.
	if err := f.s.DB().Model(&Project{}).Where("id = ?", f.project.ID).Updates(map[string]any{"nuwa_enabled": nil, "nuwa_revision": nil}).Error; err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		if err := f.s.UpdateProject(f.project.ID, map[string]any{"nuwa_enabled": enabled}); err != nil {
			t.Fatal(err)
		}
		project, err := f.s.GetProject(f.project.ID)
		want := int64(0)
		if enabled {
			want = 1
		}
		if err != nil || project.NuwaRevision != want {
			t.Fatalf("legacy mode/counter: enabled=%v project=%+v err=%v", enabled, project, err)
		}
	}
}

// Sharing work and turning off future Nuwa turns update the same SQL counter.
// Concurrent mode submissions must count the actual transition once and must
// not overwrite a node's atomically committed CAS/revision receipt.
func TestNuwaModeChangesAndSharedCASDoNotLoseProjectRevisions(t *testing.T) {
	f := newNuwaFixture(t)
	node := f.node("fact")
	if err := f.s.UpsertNuwaNode(context.Background(), &node, 0); err != nil {
		t.Fatal(err)
	}
	const modeEdits = 12
	start := make(chan struct{})
	errors := make(chan error, modeEdits+1)
	var work sync.WaitGroup
	for range modeEdits {
		work.Add(1)
		go func() {
			defer work.Done()
			<-start
			errors <- f.s.UpdateProject(f.project.ID, map[string]any{"nuwa_enabled": false})
		}()
	}
	work.Add(1)
	go func() {
		defer work.Done()
		<-start
		update := node
		update.Summary = "Corrected evidence"
		errors <- f.s.UpsertNuwaNode(context.Background(), &update, node.Version)
	}()
	close(start)
	work.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	project, err := f.s.GetProject(f.project.ID)
	if err != nil || project.NuwaEnabled || project.NuwaRevision != 3 {
		t.Fatalf("concurrent writes lost/counted duplicate revisions: %+v err=%v", project, err)
	}
	revision, nodes, err := f.s.NuwaSnapshot(context.Background(), f.project.ID)
	if err != nil || revision != 3 || len(nodes) != 1 || nodes[0].Version != 2 || nodes[0].Summary != "Corrected evidence" {
		t.Fatalf("CAS was lost during mode updates: revision=%d nodes=%+v err=%v", revision, nodes, err)
	}
	var receipts []NuwaRevision
	if err := f.s.DB().Where("project_id = ?", f.project.ID).Order("revision").Find(&receipts).Error; err != nil || len(receipts) != 2 || receipts[0].Revision != 1 || receipts[1].Version != 2 || (receipts[1].Revision != 2 && receipts[1].Revision != 3) {
		t.Fatalf("mode edits corrupted node audit history: %+v err=%v", receipts, err)
	}
}
