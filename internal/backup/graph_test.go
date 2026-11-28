package backup

import (
	"context"
	"encoding/json"
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

// A skipped conversation can edit a subject another conversation created.
// Those attributes share one row, so the archive leaves the row out instead
// of keeping the skipped edit.
func TestSkipActiveDropsASharedRowTheSkippedConversationChanged(t *testing.T) {
	src := seed(t)
	writer := &store.Thread{Title: "writer", ProjectID: src.project.ID}
	if err := src.st.CreateThread(writer); err != nil {
		t.Fatal(err)
	}
	write := func(th *store.Thread, batch graph.Batch) graph.Result {
		t.Helper()
		turn := &store.Turn{ThreadID: th.ID, ProjectID: src.project.ID, GraphTools: true}
		if err := src.st.CreateTurn(turn); err != nil {
			t.Fatal(err)
		}
		res, _, err := src.st.ApplyGraph(context.Background(),
			store.GraphCaller{ThreadID: th.ID, TurnID: turn.ID, AgentID: "manager", ProjectID: src.project.ID}, batch)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	created := write(src.thread, graph.Batch{
		Subjects: []graph.SubjectInput{
			{Ref: "repo", Kind: "repo", Attrs: map[string]any{"path": "acme/shared"}},
			{Ref: "addr", Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.50"}},
		},
		Links: []graph.LinkInput{{From: "repo", To: "addr", Rel: "served_from"}},
	})
	var repoID string
	var repoVersion int64
	for _, subject := range created.Subjects {
		if subject.Kind == "repo" {
			repoID, repoVersion = subject.ID, subject.Version
		}
	}
	write(writer, graph.Batch{Subjects: []graph.SubjectInput{{
		ID: repoID, Version: repoVersion, Kind: "repo",
		Attrs: map[string]any{"path": "acme/shared", "credential": "writer-credential"},
	}}})
	db, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "shared.zwai-backup")
	if _, err := Create(t.Context(), Options{DataDir: src.dir, DB: db, Kind: KindFull, Output: out,
		OmitThreads: []OmittedThread{{ID: writer.ID, Reason: ReasonRunning}}}); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if _, err := Restore(t.Context(), RestoreOptions{DataDir: target, Path: out, Confirm: true, Replace: true}); err != nil {
		t.Fatal(err)
	}
	restored, err := store.Open(filepath.Join(target, filepath.Base(src.cfg.DBPath())))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if graphRows(t, restored, "graph_subjects", "kind", "repo") != 0 {
		t.Fatal("the shared subject the skipped conversation edited was archived")
	}
	if graphRows(t, restored, "graph_subjects", "kind", "ip") != 1 {
		t.Fatal("a subject the skipped conversation did not touch was dropped")
	}
}

const eventGraphSecret = "fake-event-credential-for-backup"

// Tool events and transcript rows are a second copy of a credential. A full
// backup that keeps subject credentials still redacts those copies.
func TestBackupRedactsGraphToolPayloads(t *testing.T) {
	src := seed(t)
	seedGraph(t, src)
	turn := &store.Turn{ThreadID: src.thread.ID, ProjectID: src.project.ID, Status: store.TurnDone}
	if err := src.st.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	payload := `{"subjects":[{"kind":"repo","attrs":{"url":"https://fixture:` + eventGraphSecret + `@example.test/acme/app"}},{"id":"sj_existing","attrs":{"credential":"` + eventGraphSecret + `"}}]}`
	if err := src.st.AppendEvent(&store.Event{
		ThreadID: src.thread.ID, TurnID: turn.ID, Kind: "tool_result", AgentID: "manager",
		WorkRecordPayload: true, Text: payload,
	}); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := src.st.AppendEvent(&store.Event{
		ThreadID: src.thread.ID, TurnID: turn.ID, Kind: "engine_restart_checkpoint", AgentID: "manager",
		Text: `{"manager_history":[{"role":"tool","tool_name":"read_graph","content":` + string(encoded) + `}]}`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := src.st.AppendMessages(src.thread.ID, turn.ID, []store.Message{{
		Role: "assistant", WorkRecordPayload: true,
		ToolCalls: `[{"function":{"name":"update_graph","arguments":` + string(encoded) + `}}]`,
	}}); err != nil {
		t.Fatal(err)
	}
	db, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "payload.zwai-backup")
	if _, err := Create(t.Context(), Options{DataDir: src.dir, DB: db, Kind: KindFull, Output: out}); err != nil {
		t.Fatal(err)
	}
	if archiveHas(t, out, eventGraphSecret) {
		t.Fatal("a graph tool payload kept its credential")
	}
	if !archiveHas(t, out, graphSecret) {
		t.Fatal("subject credentials were stripped from a full backup")
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

func TestSkipActiveDropsAnEarlierEditAfterAnotherConversationWrites(t *testing.T) {
	src := seed(t)
	seedGraph(t, src)
	th := &store.Thread{Title: "second writer", ProjectID: src.project.ID}
	if err := src.st.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, ProjectID: src.project.ID, GraphTools: true}
	if err := src.st.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	const marker = "synthetic-skipped-thread-data"
	_, _, err := src.st.ApplyGraph(t.Context(), store.GraphCaller{ThreadID: th.ID, TurnID: turn.ID, ProjectID: src.project.ID, AgentID: "manager"}, graph.Batch{Subjects: []graph.SubjectInput{{Kind: "repo", Attrs: map[string]any{"path": "member-graph/repo", "note": marker}}}})
	if err != nil {
		t.Fatal(err)
	}
	keep := &store.Turn{ThreadID: src.thread.ID, ProjectID: src.project.ID, GraphTools: true}
	if err := src.st.CreateTurn(keep); err != nil {
		t.Fatal(err)
	}
	_, _, err = src.st.ApplyGraph(t.Context(), store.GraphCaller{ThreadID: src.thread.ID, TurnID: keep.ID, ProjectID: src.project.ID, AgentID: "manager"}, graph.Batch{Subjects: []graph.SubjectInput{{Kind: "repo", Attrs: map[string]any{"path": "member-graph/repo", "vcs": "git"}}}})
	if err != nil {
		t.Fatal(err)
	}
	db, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "graph.zwai-backup")
	if _, err := Create(t.Context(), Options{DataDir: src.dir, DB: db, Kind: KindFull, Output: out, OmitThreads: []OmittedThread{{ID: th.ID, Reason: ReasonRunning}}}); err != nil {
		t.Fatal(err)
	}
	if archiveHas(t, out, marker) {
		t.Fatal("archive retained graph edit from skipped thread")
	}
}
