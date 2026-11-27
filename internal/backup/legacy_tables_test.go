package backup

import (
	"path/filepath"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// An upgraded database still holds the removed work-record tables. A backup has
// to carry them into a destination that never had them, and omitting a
// conversation has to reach them: leaving them out of the omission would leak
// that conversation's content through a table nobody reads any more.
func TestLegacyWorkTablesFollowTheirConversationThroughBackup(t *testing.T) {
	src := seed(t)
	member := seedLegacyWork(t, src.st, src.project.ID, src.thread, "thread", "member-private-record-017")
	standalone := seedLegacyWork(t, src.st, "", src.standalone, "thread", "unrelated-private-record-067")
	srcDB, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name                           string
		kind                           Kind
		omit                           bool
		retainMember, retainStandalone bool
	}{
		{name: "full", kind: KindFull, retainMember: true, retainStandalone: true},
		{name: "project", kind: KindProject, retainMember: true},
		{name: "full omits member", kind: KindFull, omit: true, retainStandalone: true},
		{name: "project omits member", kind: KindProject, omit: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "legacy.zwai-backup")
			options := Options{DataDir: src.dir, DB: srcDB, Kind: tc.kind, Output: out}
			if tc.kind == KindProject {
				options.ProjectID = src.project.ID
			}
			if tc.omit {
				options.OmitThreads = []OmittedThread{{ID: src.thread.ID, Title: src.thread.Title, Reason: ReasonRunning}}
			}
			if _, err := Create(t.Context(), options); err != nil {
				t.Fatal(err)
			}
			// A destination opened by this build has no legacy tables at all.
			target := t.TempDir()
			if tc.kind == KindProject {
				dest := seed(t)
				target = dest.dir
			}
			if _, err := Restore(t.Context(), RestoreOptions{DataDir: target, Path: out, Confirm: true, Replace: tc.kind == KindFull}); err != nil {
				t.Fatal(err)
			}
			if got := archiveHas(t, out, member.Title); got != tc.retainMember {
				t.Fatalf("archive contains %q = %t, want %t", member.Title, got, tc.retainMember)
			}
			if got := archiveHas(t, out, standalone.Title); got != tc.retainStandalone {
				t.Fatalf("archive contains %q = %t, want %t", standalone.Title, got, tc.retainStandalone)
			}
			restored, err := store.Open(filepath.Join(target, filepath.Base(src.cfg.DBPath())))
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			for _, check := range []struct {
				table, column string
				work          legacyWork
				retained      bool
			}{
				{"nuwa_nodes", "id", member, tc.retainMember},
				{"nuwa_revisions", "node_id", member, tc.retainMember},
				{"nuwa_verify_slots", "target_id", member, tc.retainMember},
				{"nuwa_nodes", "id", standalone, tc.retainStandalone},
			} {
				var present bool
				if restored.DB().Migrator().HasTable(check.table) {
					present = legacyRows(t, restored, check.table, check.column, check.work.ID) == 1
				}
				if present != check.retained {
					t.Fatalf("%s row for %s present=%t, want %t", check.table, check.work.ID, present, check.retained)
				}
			}
		})
	}
}

// A database that never had the tables must not make a backup fail.
func TestBackupWorksWithoutAnyLegacyTables(t *testing.T) {
	src := seed(t)
	for _, table := range []string{"nuwa_nodes", "nuwa_revisions", "nuwa_verify_slots"} {
		if err := src.st.DB().Migrator().DropTable(table); err != nil {
			t.Fatal(err)
		}
	}
	db, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "plain.zwai-backup")
	if _, err := Create(t.Context(), Options{DataDir: src.dir, DB: db, Kind: KindFull, Output: out,
		OmitThreads: []OmittedThread{{ID: src.thread.ID, Reason: ReasonRunning}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(t.Context(), RestoreOptions{DataDir: t.TempDir(), Path: out, Confirm: true, Replace: true}); err != nil {
		t.Fatal(err)
	}
}
