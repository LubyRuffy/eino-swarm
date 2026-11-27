package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestOmittingAMovedConversationPurgesAllAffectedReviewReceipts(t *testing.T) {
	src := seed(t)
	ctx := t.Context()
	project := func(name string) *store.Project {
		t.Helper()
		p := &store.Project{Name: name}
		if err := src.st.CreateProject(p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	queueOwner := project("Former queue owner")
	threadAnchorOwner := project("Saved conversation anchor")
	turnAnchorOwner := project("Legacy turn anchor")
	untouched := project("Unrelated")
	conversation := func(owner *store.Project) (*store.Thread, store.Turn) {
		t.Helper()
		thread := &store.Thread{ProjectID: owner.ID}
		if err := src.st.CreateThread(thread); err != nil {
			t.Fatal(err)
		}
		turn := store.Turn{ProjectID: owner.ID, ThreadID: thread.ID, Status: store.TurnDone}
		if err := src.st.CreateTurn(&turn); err != nil {
			t.Fatal(err)
		}
		event := store.Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: "user_message", Text: "Original observation."}
		if err := src.st.AppendEvent(&event); err != nil {
			t.Fatal(err)
		}
		return thread, turn
	}
	start := func(owner *store.Project, thread *store.Thread) string {
		t.Helper()
		job := store.ProjectDistillation{ProjectID: owner.ID, ID: "receipt-" + owner.ID, State: "running"}
		if total, err := src.st.StartDistillationQueue(ctx, job, thread.ID, func(int) ([]byte, error) {
			return []byte("{\"state\":\"running\"}"), nil
		}); err != nil || total != 1 {
			t.Fatalf("start review without a saved anchor: count=%d %v", total, err)
		}
		return job.ID
	}
	save := func(owner *store.Project, receipt []byte) {
		t.Helper()
		job := store.ProjectDistillation{ProjectID: owner.ID, ID: "completed-" + owner.ID, State: "completed", Receipt: receipt}
		if err := src.st.StartProjectDistillation(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	moved, originalTurn := conversation(src.other)
	if originalTurn.ProjectID != src.other.ID {
		t.Fatal("fixture did not snapshot the original turn's project")
	}
	if err := src.st.SetThreadProject(moved.ID, queueOwner.ID); err != nil {
		t.Fatal(err)
	}
	privateQueue := start(queueOwner, moved)
	if err := src.st.SetThreadProject(moved.ID, src.project.ID); err != nil {
		t.Fatal(err)
	}
	save(src.project, nil)
	save(src.other, []byte("{\"state\":\"completed\",\"changes\":[{\"text\":\"omitted testimony from the original owner\"}]}"))
	save(threadAnchorOwner, []byte(fmt.Sprintf("{\"thread_id\":%q}", moved.ID)))
	save(turnAnchorOwner, []byte(fmt.Sprintf("{\"turn_id\":%q}", originalTurn.ID)))
	peer, _ := conversation(untouched)
	unrelatedQueue := start(untouched, peer)

	standalone := store.Thread{}
	if err := src.st.CreateThread(&standalone); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "without-moved.zip")
	if _, err := Create(ctx, Options{DataDir: src.dir, Kind: KindFull, Output: path,
		OmitThreads: []OmittedThread{{ID: moved.ID, Reason: ReasonRunning}, {ID: standalone.ID, Reason: ReasonRunning}}}); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if _, err := Restore(ctx, RestoreOptions{DataDir: target, Path: path, Confirm: true, Replace: true}); err != nil {
		t.Fatal(err)
	}
	dst, err := store.Open(filepath.Join(target, "zwai.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	for _, threadID := range []string{moved.ID, standalone.ID} {
		if _, err := dst.GetThread(threadID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("omitted conversation was restored: %v", err)
		}
	}
	for _, p := range []*store.Project{src.project, src.other, queueOwner, threadAnchorOwner, turnAnchorOwner} {
		if receipt, err := dst.ProjectDistillationReceipt(ctx, p.ID); err != nil || receipt != nil {
			t.Fatalf("%s restored an omitted conversation's receipt, error=%v", p.Name, err)
		}
	}
	if window, err := dst.DistillationWindow(ctx, privateQueue, 0, 8); err != nil || len(window) != 0 {
		t.Fatalf("omitted source queue survived: count=%d %v", len(window), err)
	}
	if window, err := dst.DistillationWindow(ctx, unrelatedQueue, 0, 8); err != nil || len(window) != 1 || window[0].ThreadID != peer.ID {
		t.Fatalf("omission removed an unrelated source queue: count=%d %v", len(window), err)
	}
	if row, err := dst.ProjectDistillationReceipt(ctx, untouched.ID); err != nil || row == nil || row.ID != unrelatedQueue {
		t.Fatalf("omission removed an unrelated review: %+v %v", row, err)
	}
}

// An upgraded database can still hold the removed Wiki build's receipts. They
// summarise the omitted conversation's sources, so omission must reach them,
// and a trigger that refuses the erase must stop the archive being published.
func TestBackupPublishesNothingWhenAnOmittedLegacyReceiptCannotBeErased(t *testing.T) {
	src := seed(t)
	ctx := t.Context()
	if err := src.st.DB().Exec(`CREATE TABLE project_wiki_builds (project_id TEXT PRIMARY KEY, receipt BLOB)`).Error; err != nil {
		t.Fatal(err)
	}
	receipt := fmt.Sprintf(`{"thread_id":%q}`, src.thread.ID)
	if err := src.st.DB().Exec(`INSERT INTO project_wiki_builds (project_id, receipt) VALUES (?, ?)`, src.project.ID, receipt).Error; err != nil {
		t.Fatal(err)
	}
	if err := src.st.DB().Exec(`CREATE TRIGGER fixture_refuse_wiki_receipt_omission
		BEFORE DELETE ON project_wiki_builds
		BEGIN SELECT RAISE(ABORT, 'fixture refused to erase the Wiki receipt'); END`).Error; err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "without-private-review.zip")
	_, err := Create(ctx, Options{DataDir: src.dir, Kind: KindFull, Output: path,
		OmitThreads: []OmittedThread{{ID: src.thread.ID, Reason: ReasonRunning}}})
	if err == nil || !strings.Contains(err.Error(), "backup: omit project_wiki_builds:") || !strings.Contains(err.Error(), "fixture refused to erase the Wiki receipt") {
		t.Fatalf("omission failure lost its cause: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed privacy filtering published an archive: %v", err)
	}
	if n := legacyRows(t, src.st, "project_wiki_builds", "project_id", src.project.ID); n != 1 {
		t.Fatalf("failed archive creation changed the live legacy receipt: %d rows", n)
	}
}
