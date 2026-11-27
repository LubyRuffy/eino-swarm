package backup

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const graphSecret = "fake-credential-for-backup-test"

// seedGraph writes one project-owned graph from the member conversation and one
// standalone graph, each with a credential attribute and a to-do.
func seedGraph(t *testing.T, src seeded) (project, standalone string) {
	t.Helper()
	write := func(th *store.Thread, projectID, name string) {
		turn := &store.Turn{ThreadID: th.ID, ProjectID: projectID, GraphTools: true}
		if err := src.st.CreateTurn(turn); err != nil {
			t.Fatal(err)
		}
		_, _, err := src.st.ApplyGraph(context.Background(),
			store.GraphCaller{ThreadID: th.ID, TurnID: turn.ID, AgentID: "manager", ProjectID: projectID},
			graph.Batch{
				Todos: []graph.TodoInput{{Title: name + " todo"}},
				Subjects: []graph.SubjectInput{{Kind: "repo", Attrs: map[string]any{
					"path": name + "/repo", "credential": graphSecret}}},
			})
		if err != nil {
			t.Fatal(err)
		}
	}
	write(src.thread, src.project.ID, "member-graph")
	write(src.standalone, "", "standalone-graph")
	return "member-graph", "standalone-graph"
}

func graphRows(t *testing.T, st *store.Store, table, column, like string) int64 {
	t.Helper()
	var n int64
	if err := st.DB().Table(table).Where(column+" LIKE ?", like).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

// A graph is user work: a backup carries it, a credential-free one carries it
// without the credential, and omitting a conversation takes out what that
// conversation wrote rather than leaving it in a table nobody thinks to filter.
func TestGraphFollowsItsOwnerThroughBackup(t *testing.T) {
	src := seed(t)
	member, standalone := seedGraph(t, src)
	srcDB, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name                           string
		kind                           Kind
		omitSecrets, omit              bool
		retainMember, retainStandalone bool
		secret                         bool
	}{
		{name: "full", kind: KindFull, retainMember: true, retainStandalone: true, secret: true},
		{name: "full without credentials", kind: KindFull, omitSecrets: true, retainMember: true, retainStandalone: true},
		{name: "project never carries credentials", kind: KindProject, retainMember: true},
		{name: "full omits member", kind: KindFull, omit: true, retainStandalone: true, secret: true},
		{name: "project omits member", kind: KindProject, omit: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "graph.zwai-backup")
			options := Options{DataDir: src.dir, DB: srcDB, Kind: tc.kind, Output: out, OmitSecrets: tc.omitSecrets}
			if tc.kind == KindProject {
				options.ProjectID = src.project.ID
			}
			if tc.omit {
				options.OmitThreads = []OmittedThread{{ID: src.thread.ID, Title: src.thread.Title, Reason: ReasonRunning}}
			}
			if _, err := Create(t.Context(), options); err != nil {
				t.Fatal(err)
			}
			target := t.TempDir()
			if tc.kind == KindProject {
				target = seed(t).dir
			}
			if _, err := Restore(t.Context(), RestoreOptions{DataDir: target, Path: out, Confirm: true, Replace: tc.kind == KindFull}); err != nil {
				t.Fatal(err)
			}
			restored, err := store.Open(filepath.Join(target, filepath.Base(src.cfg.DBPath())))
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			if got := graphRows(t, restored, "graph_subjects", "name", member+"%") > 0; got != tc.retainMember {
				t.Fatalf("member subject present=%t, want %t", got, tc.retainMember)
			}
			if got := graphRows(t, restored, "graph_todos", "title", member+"%") > 0; got != tc.retainMember {
				t.Fatalf("member to-do present=%t, want %t", got, tc.retainMember)
			}
			if got := graphRows(t, restored, "graph_subjects", "name", standalone+"%") > 0; got != tc.retainStandalone {
				t.Fatalf("standalone subject present=%t, want %t", got, tc.retainStandalone)
			}
			if tc.retainMember {
				var rows []store.GraphSubject
				if err := restored.DB().Find(&rows).Error; err != nil {
					t.Fatal(err)
				}
				kept := false
				for _, row := range rows {
					if _, ok := row.Attrs["credential"]; ok {
						kept = true
					}
				}
				// The standalone graph is in the full archive only.
				if tc.kind == KindFull && kept != tc.secret {
					t.Fatalf("credential present=%t, want %t", kept, tc.secret)
				}
				if tc.kind == KindProject && kept {
					t.Fatal("a project archive carried a credential")
				}
			}
			if tc.omit || tc.omitSecrets {
				if archiveHas(t, out, graphSecret) && (tc.omitSecrets || tc.kind == KindProject) {
					t.Fatal("the archive still holds the credential")
				}
			}
		})
	}
}

// An archive from before graphs existed has none of the tables and must still
// restore.
func TestRestoreAcceptsAnArchiveWithoutGraphTables(t *testing.T) {
	src := seed(t)
	for _, table := range []string{"graph_revisions", "graph_todos", "graph_links", "graph_subject_keys", "graph_subjects"} {
		if err := src.st.DB().Migrator().DropTable(table); err != nil {
			t.Fatal(err)
		}
	}
	db, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "old.zwai-backup")
	if _, err := Create(t.Context(), Options{DataDir: src.dir, DB: db, Kind: KindFull, Output: out,
		OmitThreads: []OmittedThread{{ID: src.thread.ID, Reason: ReasonRunning}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(t.Context(), RestoreOptions{DataDir: t.TempDir(), Path: out, Confirm: true, Replace: true}); err != nil {
		t.Fatal(err)
	}
}
