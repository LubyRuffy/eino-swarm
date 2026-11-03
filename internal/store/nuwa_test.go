package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"
)

type nuwaFixture struct {
	s       *Store
	project Project
	thread  Thread
	turn    Turn
	event   Event
}

func newNuwaFixture(t *testing.T) nuwaFixture {
	t.Helper()
	f := nuwaFixture{s: open(t), project: Project{Name: "collaboration", NuwaEnabled: true}}
	if err := f.s.CreateProject(&f.project); err != nil {
		t.Fatal(err)
	}
	f.thread = Thread{Title: "source", ProjectID: f.project.ID}
	if err := f.s.CreateThread(&f.thread); err != nil {
		t.Fatal(err)
	}
	f.turn = Turn{ThreadID: f.thread.ID}
	if err := f.s.CreateTurn(&f.turn); err != nil {
		t.Fatal(err)
	}
	f.event = Event{ThreadID: f.thread.ID, TurnID: f.turn.ID, Kind: "tool_result", AgentID: "worker-1", Text: "inspected source"}
	if err := f.s.AppendEvent(&f.event); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f nuwaFixture) node(kind string) NuwaNode {
	n := NuwaNode{ProjectID: f.project.ID, Kind: kind, Title: "inspect assigned input", Summary: "reported observation", Detail: "full supporting observation", ThreadID: f.thread.ID, TurnID: f.turn.ID, AgentID: "worker-1", Generation: 1}
	if kind == "artifact" {
		n.ArtifactPath = "outputs/report.txt"
	}
	return n
}

func TestNuwaNodeVersionsKeepOriginalAuthorAndCompleteCorrectionHistory(t *testing.T) {
	f := newNuwaFixture(t)
	n := f.node("fact")
	n.Evidence = []NuwaEvidence{{ThreadID: f.thread.ID, TurnID: f.turn.ID, Seq: f.event.Seq, Label: "source observation"}}
	if err := f.s.UpsertNuwaNode(context.Background(), &n, 0); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(n.ID, "nw_") || n.Version != 1 || n.Status != "reported" {
		t.Fatalf("new node %+v", n)
	}
	other := Thread{ProjectID: f.project.ID, Title: "review"}
	if err := f.s.CreateThread(&other); err != nil {
		t.Fatal(err)
	}
	review := Turn{ThreadID: other.ID}
	if err := f.s.CreateTurn(&review); err != nil {
		t.Fatal(err)
	}
	update := n
	update.ThreadID = other.ID
	update.TurnID = review.ID
	update.AgentID = "reviewer-2"
	update.Generation = 3
	update.Detail = "corrected supporting observation"
	update.Summary = "corrected observation"
	if err := f.s.UpsertNuwaNode(context.Background(), &update, 1); err != nil {
		t.Fatal(err)
	}
	if update.Version != 2 || update.ThreadID != f.thread.ID || update.TurnID != f.turn.ID || update.AgentID != "worker-1" || update.Generation != 1 || update.UpdatedBy != "reviewer-2" || update.UpdatedThreadID != other.ID || update.UpdatedTurnID != review.ID || update.UpdatedGeneration != 3 {
		t.Fatalf("correction lost author identity %+v", update)
	}
	rev, nodes, err := f.s.NuwaSnapshot(context.Background(), f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rev != 2 || len(nodes) != 1 || nodes[0].Detail != update.Detail {
		t.Fatalf("snapshot rev=%d nodes=%+v", rev, nodes)
	}
	var receipts []NuwaRevision
	if err := f.s.DB().Order("revision").Find(&receipts).Error; err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 2 || receipts[0].Revision != 1 || receipts[1].Revision != 2 {
		t.Fatalf("version history %+v", receipts)
	}
	var original NuwaNode
	if err := json.Unmarshal([]byte(receipts[0].Payload), &original); err != nil {
		t.Fatal(err)
	}
	if original.Detail != n.Detail || original.Version != 1 {
		t.Fatalf("original evidence was overwritten %+v", original)
	}
	if err := f.s.UpsertNuwaNode(context.Background(), &n, 1); !errors.Is(err, ErrNuwaConflict) {
		t.Fatalf("stale update err=%v", err)
	}
	if revision, _, _ := f.s.NuwaSnapshot(context.Background(), f.project.ID); revision != 2 {
		t.Fatal("conflict advanced project revision")
	}
}

func TestNuwaRejectsInvalidOwnersEvidenceDependenciesAndCycles(t *testing.T) {
	f := newNuwaFixture(t)
	base := f.node("fact")
	if err := f.s.UpsertNuwaNode(context.Background(), &base, 0); err != nil {
		t.Fatal(err)
	}
	dependent := f.node("task")
	dependent.DependsOn = []string{base.ID}
	if err := f.s.UpsertNuwaNode(context.Background(), &dependent, 0); err != nil {
		t.Fatal(err)
	}
	foreign := Project{Name: "foreign"}
	if err := f.s.CreateProject(&foreign); err != nil {
		t.Fatal(err)
	}
	foreignThread := Thread{ProjectID: foreign.ID}
	if err := f.s.CreateThread(&foreignThread); err != nil {
		t.Fatal(err)
	}
	foreignTurn := Turn{ThreadID: foreignThread.ID}
	if err := f.s.CreateTurn(&foreignTurn); err != nil {
		t.Fatal(err)
	}
	foreignEvent := Event{ThreadID: foreignThread.ID, TurnID: foreignTurn.ID, Kind: "tool_result"}
	if err := f.s.AppendEvent(&foreignEvent); err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(*NuwaNode){
		"kind": func(n *NuwaNode) { n.Kind = "verified" }, "status": func(n *NuwaNode) { n.Status = "verified" }, "title": func(n *NuwaNode) { n.Title = " " },
		"project": func(n *NuwaNode) { n.ProjectID = "missing" }, "thread": func(n *NuwaNode) { n.ThreadID = "missing" }, "turn": func(n *NuwaNode) { n.TurnID = "missing" },
		"foreign-thread": func(n *NuwaNode) { n.ThreadID = foreignThread.ID; n.TurnID = foreignTurn.ID }, "wrong-turn": func(n *NuwaNode) { n.TurnID = foreignTurn.ID },
		"caller": func(n *NuwaNode) { n.AgentID = "" }, "generation": func(n *NuwaNode) { n.Generation = -1 },
		"missing-dependency": func(n *NuwaNode) { n.DependsOn = []string{"missing"} },
		"missing-evidence":   func(n *NuwaNode) { n.Evidence = []NuwaEvidence{{ThreadID: f.thread.ID, Seq: 999}} },
		"foreign-evidence":   func(n *NuwaNode) { n.Evidence = []NuwaEvidence{{ThreadID: foreignThread.ID, Seq: foreignEvent.Seq}} },
		"wrong-evidence-turn": func(n *NuwaNode) {
			n.Evidence = []NuwaEvidence{{ThreadID: f.thread.ID, TurnID: foreignTurn.ID, Seq: f.event.Seq}}
		},
		"invalid-evidence": func(n *NuwaNode) { n.Evidence = []NuwaEvidence{{ThreadID: f.thread.ID, Seq: 0}} },
		"artifact-path":    func(n *NuwaNode) { n.Kind = "artifact"; n.ArtifactPath = "" },
	} {
		t.Run(name, func(t *testing.T) {
			n := f.node("fact")
			edit(&n)
			if err := f.s.UpsertNuwaNode(context.Background(), &n, 0); !errors.Is(err, ErrInvalidNuwaNode) {
				t.Fatalf("invalid node err=%v", err)
			}
		})
	}
	for name, deps := range map[string][]string{"self": {base.ID}, "cycle": {dependent.ID}} {
		t.Run(name, func(t *testing.T) {
			n := base
			n.DependsOn = deps
			if err := f.s.UpsertNuwaNode(context.Background(), &n, base.Version); !errors.Is(err, ErrInvalidNuwaNode) {
				t.Fatalf("cycle err=%v", err)
			}
		})
	}
	if rev, nodes, err := f.s.NuwaSnapshot(context.Background(), f.project.ID); err != nil || rev != 2 || len(nodes) != 2 {
		t.Fatalf("invalid writes changed snapshot rev=%d nodes=%d err=%v", rev, len(nodes), err)
	}
}

func TestNuwaConcurrentCASCommitsOneVersionAndOneRevision(t *testing.T) {
	f := newNuwaFixture(t)
	n := f.node("task")
	if err := f.s.UpsertNuwaNode(context.Background(), &n, 0); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, summary := range []string{"first update", "second update"} {
		wg.Add(1)
		go func(summary string) {
			defer wg.Done()
			copy := n
			copy.Summary = summary
			<-start
			results <- f.s.UpsertNuwaNode(context.Background(), &copy, 1)
		}(summary)
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrNuwaConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	if rev, nodes, _ := f.s.NuwaSnapshot(context.Background(), f.project.ID); rev != 2 || len(nodes) != 1 || nodes[0].Version != 2 {
		t.Fatalf("non-atomic CAS rev=%d nodes=%+v", rev, nodes)
	}
}

func TestNuwaCancelledTurnStorageFailureAndDeletedOwnersCannotPublish(t *testing.T) {
	f := newNuwaFixture(t)
	n := f.node("fact")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.s.UpsertNuwaNode(ctx, &n, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write err=%v", err)
	}
	if err := f.s.FinishTurn(f.turn.ID, TurnCancelled, "", "human stopped"); err != nil {
		t.Fatal(err)
	}
	if err := f.s.UpsertNuwaNode(context.Background(), &n, 0); !errors.Is(err, ErrInvalidNuwaNode) {
		t.Fatalf("stopped turn err=%v", err)
	}
	if err := f.s.DB().Model(&Turn{}).Where("id = ?", f.turn.ID).Update("status", TurnRunning).Error; err != nil {
		t.Fatal(err)
	}
	failed := false
	if err := f.s.DB().Callback().Create().Before("gorm:create").Register("nuwa:test:fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "nuwa_revisions" && !failed {
			failed = true
			_ = tx.AddError(errors.New("receipt disk failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.UpsertNuwaNode(context.Background(), &n, 0); err == nil {
		t.Fatal("failed audit receipt committed node")
	}
	if n.ID != "" {
		t.Fatal("failed write mutated caller identity")
	}
	if rev, nodes, _ := f.s.NuwaSnapshot(context.Background(), f.project.ID); rev != 0 || len(nodes) != 0 {
		t.Fatalf("storage error partially committed rev=%d nodes=%v", rev, nodes)
	}
	if err := f.s.DeleteThread(f.thread.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.UpsertNuwaNode(context.Background(), &n, 0); !errors.Is(err, ErrInvalidNuwaNode) {
		t.Fatalf("late write recreated deleted owner: %v", err)
	}
}

func TestNuwaDeletionKeepsUnavailableReferencesExplicitAndIsProjectScoped(t *testing.T) {
	f := newNuwaFixture(t)
	base := f.node("fact")
	if err := f.s.UpsertNuwaNode(context.Background(), &base, 0); err != nil {
		t.Fatal(err)
	}
	thread := Thread{ProjectID: f.project.ID, Title: "dependent source"}
	if err := f.s.CreateThread(&thread); err != nil {
		t.Fatal(err)
	}
	turn := Turn{ThreadID: thread.ID}
	if err := f.s.CreateTurn(&turn); err != nil {
		t.Fatal(err)
	}
	dependent := f.node("task")
	dependent.ThreadID = thread.ID
	dependent.TurnID = turn.ID
	dependent.DependsOn = []string{base.ID}
	dependent.Evidence = []NuwaEvidence{{ThreadID: f.thread.ID, Seq: f.event.Seq}}
	if err := f.s.UpsertNuwaNode(context.Background(), &dependent, 0); err != nil {
		t.Fatal(err)
	}
	otherProject := Project{Name: "independent"}
	if err := f.s.CreateProject(&otherProject); err != nil {
		t.Fatal(err)
	}
	otherThread := Thread{ProjectID: otherProject.ID}
	if err := f.s.CreateThread(&otherThread); err != nil {
		t.Fatal(err)
	}
	otherTurn := Turn{ThreadID: otherThread.ID}
	if err := f.s.CreateTurn(&otherTurn); err != nil {
		t.Fatal(err)
	}
	other := f.node("artifact")
	other.ProjectID = otherProject.ID
	other.ThreadID = otherThread.ID
	other.TurnID = otherTurn.ID
	if err := f.s.UpsertNuwaNode(context.Background(), &other, 0); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteThread(f.thread.ID); err != nil {
		t.Fatal(err)
	}
	rev, nodes, err := f.s.NuwaSnapshot(context.Background(), f.project.ID)
	if err != nil || rev != 3 || len(nodes) != 1 || len(nodes[0].MissingDependencies) != 1 || nodes[0].MissingDependencies[0] != base.ID || len(nodes[0].UnavailableEvidence) != 1 || nodes[0].DependsOn[0] != base.ID {
		t.Fatalf("deleted sources became usable: rev=%d nodes=%+v err=%v", rev, nodes, err)
	}
	var oldReceipts int64
	if err := f.s.DB().Model(&NuwaRevision{}).Where("node_id = ?", base.ID).Count(&oldReceipts).Error; err != nil {
		t.Fatal(err)
	}
	if oldReceipts != 0 {
		t.Fatal("deleted private origin survived in revision payloads")
	}
	if err := f.s.DeleteProject(f.project.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.s.NuwaSnapshot(context.Background(), f.project.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted project snapshot err=%v", err)
	}
	if err := f.s.UpsertNuwaNode(context.Background(), &dependent, dependent.Version); !errors.Is(err, ErrInvalidNuwaNode) {
		t.Fatalf("late write recreated deleted project: %v", err)
	}
	if revision, nodes, err := f.s.NuwaSnapshot(context.Background(), otherProject.ID); err != nil || revision != 1 || len(nodes) != 1 || nodes[0].ID != other.ID {
		t.Fatalf("deletion escaped project: rev=%d nodes=%+v err=%v", revision, nodes, err)
	}
}

func TestNuwaInvalidationCancellationAndLegacyNullRevision(t *testing.T) {
	f := newNuwaFixture(t)
	if err := f.s.DB().Model(&Project{}).Where("id = ?", f.project.ID).UpdateColumn("nuwa_revision", nil).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.s.BumpNuwaRevision(context.Background(), f.project.ID); err != nil {
		t.Fatal(err)
	}
	if revision, nodes, err := f.s.NuwaSnapshot(context.Background(), f.project.ID); err != nil || revision != 1 || len(nodes) != 0 {
		t.Fatalf("invalidation created content: %d %+v %v", revision, nodes, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.s.BumpNuwaRevision(ctx, f.project.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled invalidation err=%v", err)
	}
	if _, _, err := f.s.NuwaSnapshot(ctx, f.project.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled snapshot err=%v", err)
	}
	if err := f.s.BumpNuwaRevision(context.Background(), "missing"); !errors.Is(err, ErrInvalidNuwaNode) {
		t.Fatalf("unknown invalidation err=%v", err)
	}
	if err := f.s.UpsertNuwaNode(context.Background(), nil, 0); !errors.Is(err, ErrInvalidNuwaNode) {
		t.Fatal(err)
	}
	for _, version := range []int64{-1, 1} {
		n := f.node("task")
		if err := f.s.UpsertNuwaNode(context.Background(), &n, version); !errors.Is(err, ErrNuwaConflict) {
			t.Fatalf("invalid initial CAS version %d: %v", version, err)
		}
	}
	n := f.node("task")
	n.ID = "nw_missing"
	if err := f.s.UpsertNuwaNode(context.Background(), &n, 1); !errors.Is(err, ErrInvalidNuwaNode) {
		t.Fatal(err)
	}
}

// Faults run through the actual GORM/SQLite transaction. Keep the one-shot
// callback registered until this private Store closes, even after it fires.
func failNuwaOperation(t *testing.T, s *Store, operation, table string, nth int) *bool {
	t.Helper()
	var register func(string, func(*gorm.DB)) error
	switch operation {
	case "query":
		register = s.DB().Callback().Query().Before("gorm:query").Register
	case "create":
		register = s.DB().Callback().Create().Before("gorm:create").Register
	case "update":
		register = s.DB().Callback().Update().Before("gorm:update").Register
	case "delete":
		register = s.DB().Callback().Delete().Before("gorm:delete").Register
	}
	seen, failed := 0, false
	if err := register("nuwa:test:fault", func(tx *gorm.DB) {
		if tx.Statement.Table == table {
			seen++
			if seen == nth && !failed {
				failed = true
				_ = tx.AddError(errors.New("injected shared-store failure"))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	return &failed
}

func TestNuwaThreadMoveCommitsBothInvalidationsOrKeepsOriginalProject(t *testing.T) {
	f := newNuwaFixture(t)
	// A disabled project with saved work still owns an open Collaboration view.
	if err := f.s.UpdateProject(f.project.ID, map[string]any{"nuwa_enabled": false}); err != nil {
		t.Fatal(err)
	}
	destination := &Project{Name: "Destination", NuwaEnabled: true}
	if err := f.s.CreateProject(destination); err != nil {
		t.Fatal(err)
	}
	original, err := f.s.GetProject(f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	failed := failNuwaOperation(t, f.s, "create", "nuwa_revisions", 2)
	if err := f.s.SetThreadProject(f.thread.ID, destination.ID); err == nil || !*failed {
		t.Fatalf("move accepted despite a failed invalidation: fired=%t err=%v", *failed, err)
	}
	verify := func(owner string, before, after, receipts int64) {
		t.Helper()
		thread, err := f.s.GetThread(f.thread.ID)
		if err != nil || thread.ProjectID != owner {
			t.Fatalf("move owner=%+v err=%v", thread, err)
		}
		for id, want := range map[string]int64{original.ID: before, destination.ID: after} {
			project, err := f.s.GetProject(id)
			if err != nil || project.NuwaRevision != want {
				t.Fatalf("move revision=%+v want=%d err=%v", project, want, err)
			}
		}
		var count int64
		if err := f.s.DB().Model(&NuwaRevision{}).Count(&count).Error; err != nil || count != receipts {
			t.Fatalf("move receipts=%d want=%d err=%v", count, receipts, err)
		}
	}
	verify(original.ID, original.NuwaRevision, 0, 0)
	if err := f.s.SetThreadProject(f.thread.ID, destination.ID); err != nil {
		t.Fatal(err)
	}
	verify(destination.ID, original.NuwaRevision+1, 1, 2)
	if err := f.s.SetThreadProject(f.thread.ID, destination.ID); err != nil {
		t.Fatal(err)
	}
	verify(destination.ID, original.NuwaRevision+1, 1, 2)
}

func TestNuwaThreadMoveRefusesIncompleteStorageWork(t *testing.T) {
	for _, tc := range []struct{ operation, table string }{
		{"query", "threads"},
		{"update", "threads"},
		{"query", "projects"},
	} {
		t.Run(tc.operation+"/"+tc.table, func(t *testing.T) {
			f := newNuwaFixture(t)
			failed := failNuwaOperation(t, f.s, tc.operation, tc.table, 1)
			if err := f.s.SetThreadProject(f.thread.ID, ""); err == nil || !*failed {
				t.Fatalf("move accepted with incomplete storage work: fired=%t err=%v", *failed, err)
			}
			thread, err := f.s.GetThread(f.thread.ID)
			if err != nil || thread.ProjectID != f.project.ID {
				t.Fatalf("failed move changed membership: thread=%+v err=%v", thread, err)
			}
		})
	}
	f := newNuwaFixture(t)
	if err := f.s.SetThreadProject("missing-thread", f.project.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing thread move returned %v", err)
	}
}

func TestNuwaWriteFailuresRollBackNodeRevisionAndAuditTogether(t *testing.T) {
	for _, test := range []struct {
		operation, table string
		existing         bool
		nth              int
	}{
		{"update", "projects", false, 1}, {"query", "projects", false, 1},
		{"query", "threads", false, 1}, {"query", "turns", false, 1}, {"query", "events", false, 1},
		{"query", "nuwa_nodes", false, 1}, {"query", "nuwa_nodes", true, 1}, {"query", "nuwa_nodes", true, 2},
		{"create", "nuwa_nodes", false, 1}, {"update", "nuwa_nodes", true, 1}, {"create", "nuwa_revisions", true, 1},
	} {
		t.Run(test.operation+"/"+test.table+fmt.Sprint(test.existing, test.nth), func(t *testing.T) {
			f := newNuwaFixture(t)
			n := f.node("fact")
			version := int64(0)
			if test.existing {
				if err := f.s.UpsertNuwaNode(context.Background(), &n, 0); err != nil {
					t.Fatal(err)
				}
				version = 1
			}
			n.Evidence = []NuwaEvidence{{ThreadID: f.thread.ID, Seq: f.event.Seq}}
			failed := failNuwaOperation(t, f.s, test.operation, test.table, test.nth)
			if err := f.s.UpsertNuwaNode(context.Background(), &n, version); err == nil || !*failed {
				t.Fatalf("fault did not refuse write: fired=%v err=%v", *failed, err)
			}
			if revision, nodes, err := f.s.NuwaSnapshot(context.Background(), f.project.ID); err != nil || revision != version || len(nodes) != int(version) {
				t.Fatalf("partial transaction rev=%d nodes=%v err=%v", revision, nodes, err)
			}
		})
	}
	f := newNuwaFixture(t)
	n := f.node("task")
	if err := f.s.UpsertNuwaNode(context.Background(), &n, 0); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DB().Exec(`CREATE TRIGGER nuwa_ignore_update BEFORE UPDATE ON nuwa_nodes BEGIN SELECT RAISE(IGNORE); END;`).Error; err != nil {
		t.Fatal(err)
	}
	n.Summary = "must not commit"
	if err := f.s.UpsertNuwaNode(context.Background(), &n, 1); !errors.Is(err, ErrNuwaConflict) {
		t.Fatalf("ignored CAS update err=%v", err)
	}
	if revision, nodes, _ := f.s.NuwaSnapshot(context.Background(), f.project.ID); revision != 1 || nodes[0].Version != 1 {
		t.Fatalf("ignored row update committed receipt rev=%d nodes=%+v", revision, nodes)
	}
}

func TestNuwaSnapshotReadFailureCannotClaimACompleteSnapshot(t *testing.T) {
	for _, table := range []string{"projects", "nuwa_nodes", "events"} {
		t.Run(table, func(t *testing.T) {
			f := newNuwaFixture(t)
			n := f.node("fact")
			n.Evidence = []NuwaEvidence{{ThreadID: f.thread.ID, Seq: f.event.Seq}}
			if err := f.s.UpsertNuwaNode(context.Background(), &n, 0); err != nil {
				t.Fatal(err)
			}
			failNuwaOperation(t, f.s, "query", table, 1)
			if _, _, err := f.s.NuwaSnapshot(context.Background(), f.project.ID); err == nil {
				t.Fatal("failed read accepted snapshot")
			}
		})
	}
}

func TestNuwaOwnerDeletionFailuresRetainOwnersAndTheirSharedRecords(t *testing.T) {
	for _, test := range []struct {
		operation, table string
		project          bool
		nth              int
	}{
		{"query", "threads", false, 2}, {"query", "nuwa_nodes", false, 1},
		{"delete", "nuwa_revisions", false, 1}, {"delete", "nuwa_nodes", false, 1},
		{"update", "projects", false, 1}, {"create", "nuwa_revisions", false, 1},
		{"delete", "nuwa_revisions", true, 1}, {"delete", "nuwa_nodes", true, 1},
	} {
		t.Run(test.operation+"/"+test.table+fmt.Sprint(test.project), func(t *testing.T) {
			f := newNuwaFixture(t)
			n := f.node("fact")
			if err := f.s.UpsertNuwaNode(context.Background(), &n, 0); err != nil {
				t.Fatal(err)
			}
			failed := failNuwaOperation(t, f.s, test.operation, test.table, test.nth)
			var err error
			if test.project {
				err = f.s.DeleteProject(f.project.ID)
			} else {
				err = f.s.DeleteThread(f.thread.ID)
			}
			if err == nil || !*failed {
				t.Fatalf("deletion fault did not refuse: fired=%v err=%v", *failed, err)
			}
			if _, err := f.s.GetThread(f.thread.ID); err != nil {
				t.Fatal("failed deletion removed owner", err)
			}
			if revision, nodes, err := f.s.NuwaSnapshot(context.Background(), f.project.ID); err != nil || revision != 1 || len(nodes) != 1 {
				t.Fatalf("deletion partially committed rev=%d nodes=%v err=%v", revision, nodes, err)
			}
		})
	}
	f := newNuwaFixture(t)
	loose := Thread{Title: "no project"}
	if err := f.s.CreateThread(&loose); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteThread(loose.ID); err != nil {
		t.Fatal(err)
	}
}

func TestNuwaWriteReceiptDeduplicatesLostAcknowledgementWithoutRewritingNewerVersions(t *testing.T) {
	f := newNuwaFixture(t)
	receipt := NuwaWriteReceipt{Key: "trusted-call", Hash: "canonical-content"}
	n := f.node("fact")
	if err := f.s.UpsertNuwaNode(context.Background(), &n, 0, receipt); err != nil {
		t.Fatal(err)
	}
	changed := n
	changed.Summary = "newer correction"
	if err := f.s.UpsertNuwaNode(context.Background(), &changed, 1, NuwaWriteReceipt{Key: "trusted-correction", Hash: "correction-content"}); err != nil {
		t.Fatal(err)
	}
	retry := f.node("fact")
	if err := f.s.UpsertNuwaNode(context.Background(), &retry, 0, receipt); err != nil {
		t.Fatal(err)
	}
	if retry.ID != n.ID || retry.Version != 1 || retry.Summary != n.Summary {
		t.Fatalf("retry did not return frozen receipt %+v", retry)
	}
	if rev, nodes, err := f.s.NuwaSnapshot(context.Background(), f.project.ID); err != nil || rev != 2 || len(nodes) != 1 || nodes[0].Summary != changed.Summary {
		t.Fatalf("ack retry overwrote latest version rev=%d nodes=%+v err=%v", rev, nodes, err)
	}
	conflict := f.node("fact")
	if err := f.s.UpsertNuwaNode(context.Background(), &conflict, 0, NuwaWriteReceipt{Key: receipt.Key, Hash: "different-content"}); !errors.Is(err, ErrNuwaConflict) {
		t.Fatalf("same call accepted different content %v", err)
	}
	for _, receipts := range [][]NuwaWriteReceipt{{{Key: "missing-hash"}}, {{Hash: "missing-key"}}, {{Key: "one", Hash: "one"}, {Key: "two", Hash: "two"}}} {
		invalid := f.node("fact")
		if err := f.s.UpsertNuwaNode(context.Background(), &invalid, 0, receipts...); !errors.Is(err, ErrInvalidNuwaNode) {
			t.Fatalf("invalid receipt %v", err)
		}
	}
	if err := f.s.FinishTurn(f.turn.ID, TurnCancelled, "", "stop"); err != nil {
		t.Fatal(err)
	}
	if err := f.s.UpsertNuwaNode(context.Background(), &retry, 0, receipt); !errors.Is(err, ErrInvalidNuwaNode) {
		t.Fatalf("receipt bypassed cancelled owner %v", err)
	}
}

func TestNuwaConcurrentIdenticalToolCallsCommitOneNodeAndReceipt(t *testing.T) {
	f := newNuwaFixture(t)
	start := make(chan struct{})
	results := make(chan NuwaNode, 2)
	failures := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := f.node("fact")
			<-start
			if err := f.s.UpsertNuwaNode(context.Background(), &n, 0, NuwaWriteReceipt{Key: "same-call", Hash: "same-content"}); err != nil {
				failures <- err
			}
			results <- n
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var ids []string
	for n := range results {
		ids = append(ids, n.ID)
	}
	if ids[0] == "" || ids[0] != ids[1] {
		t.Fatalf("same call created different nodes %v", ids)
	}
	if rev, nodes, err := f.s.NuwaSnapshot(context.Background(), f.project.ID); err != nil || rev != 1 || len(nodes) != 1 {
		t.Fatalf("duplicate publication rev=%d nodes=%v err=%v", rev, nodes, err)
	}
}

func TestNuwaWriteReceiptLookupFailureAndCorruptPayloadFailClosed(t *testing.T) {
	f := newNuwaFixture(t)
	n := f.node("fact")
	receipt := NuwaWriteReceipt{Key: "trusted-call", Hash: "canonical-content"}
	fired := failNuwaOperation(t, f.s, "query", "nuwa_revisions", 1)
	if err := f.s.UpsertNuwaNode(context.Background(), &n, 0, receipt); err == nil || !*fired {
		t.Fatalf("failed receipt lookup accepted err=%v", err)
	}
	if err := f.s.UpsertNuwaNode(context.Background(), &n, 0, receipt); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DB().Model(&NuwaRevision{}).Where("project_id = ?", f.project.ID).Update("payload", "invalid-json").Error; err != nil {
		t.Fatal(err)
	}
	retry := f.node("fact")
	if err := f.s.UpsertNuwaNode(context.Background(), &retry, 0, receipt); err == nil {
		t.Fatal("corrupt write receipt accepted")
	}
	if rev, nodes, _ := f.s.NuwaSnapshot(context.Background(), f.project.ID); rev != 1 || len(nodes) != 1 {
		t.Fatal("receipt failure created duplicate publication")
	}
}

func TestNuwaDependencyCorrectionsRequireExplicitRevalidation(t *testing.T) {
	f := newNuwaFixture(t)
	source := f.node("fact")
	if err := f.s.UpsertNuwaNode(context.Background(), &source, 0); err != nil {
		t.Fatal(err)
	}
	result := f.node("artifact")
	result.DependsOn = []string{source.ID}
	if err := f.s.UpsertNuwaNode(context.Background(), &result, 0); err != nil {
		t.Fatal(err)
	}
	if result.DependencyVersions[source.ID] != 1 {
		t.Fatalf("source version not frozen %+v", result)
	}
	source.Summary = "corrected observation"
	if err := f.s.UpsertNuwaNode(context.Background(), &source, 1); err != nil {
		t.Fatal(err)
	}
	_, nodes, err := f.s.NuwaSnapshot(context.Background(), f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if node.ID == result.ID && len(node.StaleDependencies) != 1 {
			t.Fatalf("corrected basis looked current %+v", node)
		}
	}
	result.Summary = "edited without revisiting its basis"
	if err := f.s.UpsertNuwaNode(context.Background(), &result, 1); err != nil {
		t.Fatal(err)
	}
	if result.DependencyVersions[source.ID] != 1 {
		t.Fatal("ordinary edit silently refreshed evidence versions")
	}
	result.DependencyVersions = nil
	if err := f.s.UpsertNuwaNode(context.Background(), &result, 2); err != nil {
		t.Fatal(err)
	}
	if result.DependencyVersions[source.ID] != 2 {
		t.Fatal("explicit dependency revalidation did not record latest basis")
	}
	source.Status = "retracted"
	if err := f.s.UpsertNuwaNode(context.Background(), &source, 2); err != nil {
		t.Fatal(err)
	}
	result.DependencyVersions = nil
	if err := f.s.UpsertNuwaNode(context.Background(), &result, 3); err != nil {
		t.Fatal(err)
	}
	_, nodes, err = f.s.NuwaSnapshot(context.Background(), f.project.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if node.ID == result.ID && len(node.StaleDependencies) != 1 {
			t.Fatalf("retracted source accepted as current %+v", node)
		}
	}
	for _, versions := range []map[string]int64{{}, {source.ID: 0}, {"wrong-id": 2}, {source.ID: 2, "extra": 1}} {
		n := f.node("task")
		n.DependsOn = []string{source.ID}
		n.DependencyVersions = versions
		if err := f.s.UpsertNuwaNode(context.Background(), &n, 0); !errors.Is(err, ErrInvalidNuwaNode) {
			t.Fatalf("invalid source versions accepted %v: %v", versions, err)
		}
	}
}
