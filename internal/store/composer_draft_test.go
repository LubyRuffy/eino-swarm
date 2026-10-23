package store

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"
)

func TestComposerDraftSurvivesReopenWithOriginalAttachments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "drafts.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	in := ComposerDraft{Key: "new:home", Text: "未发送\nkeep spaces  ", State: "editing", Quotes: []DraftQuote{{ID: "q1", Text: "source"}}, Files: []DraftFile{{ID: "f1", Name: "input.bin", MIME: "application/octet-stream", Kind: "file", Size: 4, LastModified: 123}}}
	saved, err := s.SaveComposerDraft(context.Background(), in, map[string][]byte{"f1": {0, 1, 2, 255}})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Revision != 1 || saved.UpdatedAt.IsZero() {
		t.Fatalf("missing durable version: %+v", saved)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, err := s.GetComposerDraft(context.Background(), in.Key)
	if err != nil || got.Text != in.Text || len(got.Quotes) != 1 || got.Quotes[0] != in.Quotes[0] || len(got.Files) != 1 || got.Files[0] != in.Files[0] {
		t.Fatalf("reopen lost snapshot: %+v, %v", got, err)
	}
	data, err := s.GetComposerDraftFile(context.Background(), in.Key, "f1")
	if err != nil || !bytes.Equal(data, []byte{0, 1, 2, 255}) {
		t.Fatalf("attachment changed: %x %v", data, err)
	}
}

func TestComposerDraftCASPreservesTheNewestSnapshotAndTombstone(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	got, err := s.GetComposerDraft(ctx, "new:home")
	if err != nil || got.Revision != 0 || got.State != "editing" || got.Files == nil || got.Quotes == nil {
		t.Fatalf("empty draft: %+v %v", got, err)
	}
	first, err := s.SaveComposerDraft(ctx, ComposerDraft{Key: "new:home", Text: "first"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveComposerDraft(ctx, ComposerDraft{Key: first.Key, Text: "stale"}, nil); !errors.Is(err, ErrDraftConflict) {
		t.Fatalf("stale save accepted: %v", err)
	}
	first.Text, first.State = "accepted?", "sending"
	second, err := s.SaveComposerDraft(ctx, first, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClearComposerDraft(ctx, first.Key, first.Revision); !errors.Is(err, ErrDraftConflict) {
		t.Fatalf("stale clear accepted: %v", err)
	}
	cleared, err := s.ClearComposerDraft(ctx, first.Key, second.Revision)
	if err != nil || cleared.Revision != 3 || cleared.Text != "" || cleared.State != "editing" {
		t.Fatalf("clear: %+v %v", cleared, err)
	}
	if _, err := s.SaveComposerDraft(ctx, first, nil); !errors.Is(err, ErrDraftConflict) {
		t.Fatalf("old page resurrected sent draft: %v", err)
	}
}

func TestComposerDraftReferencesStayInTheirOwnScope(t *testing.T) {
	s := open(t)
	if err := s.CreateProject(&Project{ID: "other", Name: "other"}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	a := ComposerDraft{Key: "new:home", Text: "A", Files: []DraftFile{{ID: "f1", Name: "a", Kind: "file", Size: 3}}}
	a, err := s.SaveComposerDraft(ctx, a, map[string][]byte{"f1": []byte("one")})
	if err != nil {
		t.Fatal(err)
	}
	b := a
	b.Key = "new:other"
	b.Revision = 0
	if _, err := s.SaveComposerDraft(ctx, b, nil); !errors.Is(err, ErrInvalidDraft) {
		t.Fatalf("foreign file reference accepted: %v", err)
	}
	a.Text = "A edited"
	a, err = s.SaveComposerDraft(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := s.GetComposerDraftFile(ctx, a.Key, "f1")
	if err != nil || string(data) != "one" {
		t.Fatalf("metadata save lost bytes: %q %v", data, err)
	}
	a.Files = nil
	if _, err := s.SaveComposerDraft(ctx, a, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetComposerDraftFile(ctx, a.Key, "f1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removed file retained: %v", err)
	}
}

func TestComposerDraftRejectsInvalidSnapshotsWithoutPartialWrites(t *testing.T) {
	for _, mutate := range []struct {
		name  string
		apply func(*ComposerDraft, map[string][]byte)
	}{
		{"negative revision", func(d *ComposerDraft, _ map[string][]byte) { d.Revision = -1 }},
		{"state", func(d *ComposerDraft, _ map[string][]byte) { d.State = "done" }},
		{"duplicate file", func(d *ComposerDraft, _ map[string][]byte) { d.Files = append(d.Files, d.Files[0]) }},
		{"size", func(d *ComposerDraft, _ map[string][]byte) { d.Files[0].Size = 2 }},
		{"kind", func(d *ComposerDraft, _ map[string][]byte) { d.Files[0].Kind = "html" }},
		{"extra bytes", func(_ *ComposerDraft, b map[string][]byte) { b["extra"] = []byte("unclaimed") }},
		{"file id", func(d *ComposerDraft, _ map[string][]byte) { d.Files[0].ID = "../escape" }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			s := open(t)
			ctx := context.Background()
			d := ComposerDraft{Key: "new:home", Text: "do not partially store", Files: []DraftFile{{ID: "f1", Name: "a", Kind: "file", Size: 3}}}
			b := map[string][]byte{"f1": []byte("one")}
			mutate.apply(&d, b)
			if _, err := s.SaveComposerDraft(ctx, d, b); !errors.Is(err, ErrInvalidDraft) {
				t.Fatalf("invalid accepted: %v", err)
			}
			got, err := s.GetComposerDraft(ctx, d.Key)
			if err != nil || got.Revision != 0 || got.Text != "" {
				t.Fatalf("partial snapshot: %+v %v", got, err)
			}
		})
	}
}

func TestComposerDraftWriteFailureRollsBackBytesAndManifest(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	d := ComposerDraft{Key: "new:home", Text: "original"}
	d, err := s.SaveComposerDraft(ctx, d, nil)
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("disk write failed")
	if err := s.db.Callback().Create().Before("gorm:create").Register("draft_disk_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "composer_draft_blobs" {
			tx.AddError(boom)
		}
	}); err != nil {
		t.Fatal(err)
	}
	d.Text = "new"
	d.Files = []DraftFile{{ID: "f1", Name: "a", Kind: "file", Size: 3}}
	if _, err := s.SaveComposerDraft(ctx, d, map[string][]byte{"f1": []byte("one")}); !errors.Is(err, boom) {
		t.Fatalf("write error lost: %v", err)
	}
	got, err := s.GetComposerDraft(ctx, d.Key)
	if err != nil || got.Text != "original" || got.Revision != 1 || len(got.Files) != 0 {
		t.Fatalf("non-atomic save: %+v %v", got, err)
	}
}

func TestComposerDraftConcurrentPagesCannotOverwriteEachOther(t *testing.T) {
	s := open(t)
	var wg sync.WaitGroup
	out := make(chan error, 2)
	for _, text := range []string{"page one", "page two"} {
		wg.Add(1)
		go func(text string) {
			defer wg.Done()
			_, err := s.SaveComposerDraft(context.Background(), ComposerDraft{Key: "new:home", Text: text}, nil)
			out <- err
		}(text)
	}
	wg.Wait()
	close(out)
	success, conflict := 0, 0
	for err := range out {
		if err == nil {
			success++
		} else if errors.Is(err, ErrDraftConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}

func TestComposerDraftLimitsAndReadFailuresAreExplicit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*ComposerDraft)
	}{
		{"empty key", func(d *ComposerDraft) { d.Key = "" }},
		{"long key", func(d *ComposerDraft) { d.Key = strings.Repeat("a", 129) }},
		{"traversal key", func(d *ComposerDraft) { d.Key = "new:../home" }},
		{"large text", func(d *ComposerDraft) { d.Text = strings.Repeat("a", MaxDraftTextBytes+1) }},
		{"many quotes", func(d *ComposerDraft) { d.Quotes = make([]DraftQuote, 33) }},
		{"quote identity", func(d *ComposerDraft) { d.Quotes = []DraftQuote{{Text: "source"}} }},
		{"large quotes", func(d *ComposerDraft) {
			d.Quotes = []DraftQuote{{ID: "q", Text: strings.Repeat("a", MaxDraftTextBytes+1)}}
		}},
		{"many files", func(d *ComposerDraft) { d.Files = make([]DraftFile, 33) }},
		{"empty id", func(d *ComposerDraft) { d.Files[0].ID = "" }},
		{"long id", func(d *ComposerDraft) { d.Files[0].ID = strings.Repeat("f", 129) }},
		{"negative size", func(d *ComposerDraft) { d.Files[0].Size = -1 }},
		{"large file", func(d *ComposerDraft) { d.Files[0].Size = MaxDraftBytes + 1 }},
		{"large sum", func(d *ComposerDraft) {
			d.Files[0].Size = MaxDraftBytes
			d.Files = append(d.Files, DraftFile{ID: "f2", Name: "b", Kind: "file", Size: 1})
		}},
		{"large image", func(d *ComposerDraft) { d.Files[0].Kind = "image"; d.Files[0].Size = 8<<20 + 1 }},
		{"many images", func(d *ComposerDraft) {
			d.Files = nil
			for i := 0; i < 9; i++ {
				d.Files = append(d.Files, DraftFile{ID: NewID("image_"), Name: "a", Kind: "image", Size: 0})
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := ComposerDraft{Key: "new:home", State: "editing", Files: []DraftFile{{ID: "AZ_09-az", Name: "a", Kind: "file"}}}
			tc.change(&d)
			if err := validateComposerDraft(d, nil); !errors.Is(err, ErrInvalidDraft) {
				t.Fatalf("limit accepted: %v", err)
			}
		})
	}
	s := open(t)
	ctx := context.Background()
	d := ComposerDraft{Key: "new:home", Files: []DraftFile{{ID: "AZ_09-az", Name: "a", Kind: "file", Size: 1}}}
	d, err := s.SaveComposerDraft(ctx, d, map[string][]byte{"AZ_09-az": []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveComposerDraft(ctx, d, map[string][]byte{"AZ_09-az": []byte("x")}); !errors.Is(err, ErrInvalidDraft) {
		t.Fatal("existing bytes were mutable", err)
	}
	d.Files[0].Name = "changed"
	if _, err := s.SaveComposerDraft(ctx, d, nil); !errors.Is(err, ErrInvalidDraft) {
		t.Fatal("metadata changed existing identity", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetComposerDraft(ctx, d.Key); err == nil {
		t.Fatal("closed database looked like empty draft")
	}
	if _, err := s.GetComposerDraftFile(ctx, d.Key, "AZ_09-az"); err == nil {
		t.Fatal("closed database looked like absent file")
	}
	if _, err := s.SaveComposerDraft(ctx, ComposerDraft{Key: "new:home"}, nil); err == nil {
		t.Fatal("closed database accepted save")
	}
}

func TestComposerDraftDeletionFollowsThreadAndProjectOwnership(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	project := &Project{ID: "draft_project", Name: "drafts"}
	if err := s.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	thread := &Thread{ID: "draft_thread", ProjectID: project.ID}
	if err := s.CreateThread(thread); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"thread:" + thread.ID, "new:" + project.ID, "new:home"} {
		_, err := s.SaveComposerDraft(ctx, ComposerDraft{Key: key, Text: "keep until explicit deletion", Files: []DraftFile{{ID: "f", Name: "a", Kind: "file", Size: 1}}}, map[string][]byte{"f": []byte("x")})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteThread(thread.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveComposerDraft(ctx, ComposerDraft{Key: "thread:" + thread.ID, Text: "late save after deletion"}, nil); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted conversation was resurrected by draft save", err)
	}
	if _, err := s.GetComposerDraftFile(ctx, "thread:"+thread.ID, "f"); !errors.Is(err, ErrNotFound) {
		t.Fatal("thread bytes leaked", err)
	}
	if err := s.DeleteProject(project.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetComposerDraftFile(ctx, "new:"+project.ID, "f"); !errors.Is(err, ErrNotFound) {
		t.Fatal("project bytes leaked", err)
	}
	if data, err := s.GetComposerDraftFile(ctx, "new:home", "f"); err != nil || string(data) != "x" {
		t.Fatal("unrelated draft deleted", err)
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error { return deleteComposerDraftScopes(tx, nil) }); err != nil {
		t.Fatal(err)
	}
}

func TestComposerDraftCleanupFailureKeepsConversationAndItsOriginalBytes(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	thread := &Thread{ID: "draft_delete_failure"}
	if err := s.CreateThread(thread); err != nil {
		t.Fatal(err)
	}
	key := "thread:" + thread.ID
	_, err := s.SaveComposerDraft(ctx, ComposerDraft{Key: key, Text: "still owned", Files: []DraftFile{{ID: "f", Name: "a", Kind: "file", Size: 1}}}, map[string][]byte{"f": []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.Exec("CREATE TRIGGER draft_delete_failure BEFORE DELETE ON composer_draft_blobs BEGIN SELECT RAISE(ABORT, 'disk failure'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteThread(thread.ID); err == nil {
		t.Fatal("cleanup failure acknowledged deletion")
	}
	if _, err := s.GetThread(thread.ID); err != nil {
		t.Fatal("failed draft cleanup deleted the owning conversation", err)
	}
	if data, err := s.GetComposerDraftFile(ctx, key, "f"); err != nil || string(data) != "x" {
		t.Fatal("rollback lost original attachment", err)
	}
	if _, err := s.SaveComposerDraft(ctx, ComposerDraft{Key: "unknown:scope"}, nil); !errors.Is(err, ErrInvalidDraft) {
		t.Fatal("unknown owner accepted", err)
	}
	if err := s.db.Exec("DROP TABLE projects").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveComposerDraft(ctx, ComposerDraft{Key: "new:unreadable"}, nil); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatal("owner lookup failure disguised as an absent project", err)
	}
}

// This callback schedules a normal CreateThread+draft save after the deletion's
// list query completes if it is outside the delete transaction. Inside the
// transaction, the single SQLite connection excludes that interleaving.
func TestProjectDeletionCannotLeaveDraftsOutsideItsOwnershipSnapshot(t *testing.T) {
	s := open(t)
	p := &Project{ID: "review_project", Name: "review"}
	if err := s.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	first := &Thread{ID: "review_first", ProjectID: p.ID}
	if err := s.CreateThread(first); err != nil {
		t.Fatal(err)
	}
	inserted, protected := false, false
	if err := s.db.Callback().Query().After("gorm:query").Register("review_insert_after_listing", func(tx *gorm.DB) {
		if inserted || tx.Statement.Table != "threads" {
			return
		}
		if _, ok := tx.Statement.Dest.(*[]string); !ok {
			return
		}
		if tx.Statement.ConnPool != s.db.Statement.ConnPool {
			protected = true
			return
		}
		inserted = true
		second := &Thread{ID: "review_second", ProjectID: p.ID}
		if err := s.CreateThread(second); err != nil {
			t.Fatal(err)
		}
		_, err := s.SaveComposerDraft(context.Background(), ComposerDraft{Key: "thread:" + second.ID, Files: []DraftFile{{ID: "f", Name: "private.bin", Kind: "file", Size: 1}}}, map[string][]byte{"f": []byte("x")})
		if err != nil {
			t.Fatal(err)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	if !inserted && !protected {
		t.Fatal("proof did not reach the ownership query")
	}
	if _, err := s.GetThread("review_second"); !errors.Is(err, ErrNotFound) {
		t.Fatal("owning thread unexpectedly survived", err)
	}
	if _, err := s.GetComposerDraftFile(context.Background(), "thread:review_second", "f"); !errors.Is(err, ErrNotFound) {
		t.Fatal("project deletion acknowledged while deleted owner's attachment bytes remain", err)
	}
}

func TestProjectDraftCleanupErrorsCannotAcknowledgeOwnerDeletion(t *testing.T) {
	for _, failure := range []string{"membership read", "blob delete"} {
		t.Run(failure, func(t *testing.T) {
			s := open(t)
			project := &Project{ID: "private_project", Name: "private"}
			if err := s.CreateProject(project); err != nil {
				t.Fatal(err)
			}
			thread := &Thread{ID: "private_thread", ProjectID: project.ID}
			if err := s.CreateThread(thread); err != nil {
				t.Fatal(err)
			}
			key := "thread:" + thread.ID
			_, err := s.SaveComposerDraft(context.Background(), ComposerDraft{Key: key, Text: "private", Files: []DraftFile{{ID: "f", Name: "a", Kind: "file", Size: 1}}}, map[string][]byte{"f": []byte("x")})
			if err != nil {
				t.Fatal(err)
			}
			if failure == "membership read" {
				if err := s.db.Exec("DROP TABLE threads").Error; err != nil {
					t.Fatal(err)
				}
			} else if err := s.db.Exec("CREATE TRIGGER fail_project_blobs BEFORE DELETE ON composer_draft_blobs BEGIN SELECT RAISE(ABORT, 'disk failure'); END").Error; err != nil {
				t.Fatal(err)
			}
			if err := s.DeleteProject(project.ID); err == nil {
				t.Fatal("failed private-data cleanup acknowledged project deletion")
			}
			if _, err := s.GetProject(project.ID); err != nil {
				t.Fatal("cleanup failure deleted owner", err)
			}
			if got, err := s.GetComposerDraftFile(context.Background(), key, "f"); err != nil || string(got) != "x" {
				t.Fatal("failed cleanup lost original bytes", err)
			}
		})
	}
}
