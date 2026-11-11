package backup

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestProjectRestoreFailureRemovesAnAddedProject(t *testing.T) {
	src := seed(t)
	out := filepath.Join(t.TempDir(), "project.zwai-backup")
	sqlDB, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Create(context.Background(), Options{DataDir: src.dir, DB: sqlDB, Kind: KindProject, ProjectID: src.project.ID, Output: out}); err != nil {
		t.Fatal(err)
	}
	destDir := t.TempDir()
	t.Cleanup(func() { beforeFileSwap = nil })
	beforeFileSwap = func() error { return errors.New("disk full") }
	_, err = Restore(context.Background(), RestoreOptions{DataDir: destDir, Path: out, Confirm: true})
	if err == nil {
		t.Fatal("added project restore succeeded despite the failure")
	}
	st, err := store.Open(filepath.Join(destDir, "zwai.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.GetProject(src.project.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("failed add left the project: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "projects", src.project.ID, "memory", "MEMORY.md")); !os.IsNotExist(err) {
		t.Fatal("failed add left project files")
	}
	if _, err := os.Stat(filepath.Join(destDir, generationFile)); !os.IsNotExist(err) {
		t.Fatal("failed add advanced the generation")
	}
}

func TestProjectReplaceFailureRestoresThePreviousRows(t *testing.T) {
	src := seed(t)
	out := filepath.Join(t.TempDir(), "project.zwai-backup")
	sqlDB, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Create(context.Background(), Options{DataDir: src.dir, DB: sqlDB, Kind: KindProject, ProjectID: src.project.ID, Output: out}); err != nil {
		t.Fatal(err)
	}
	if err := src.st.DB().Model(&store.Message{}).Where("thread_id = ?", src.thread.ID).Update("content", "edited-message").Error; err != nil {
		t.Fatal(err)
	}
	writeText(t, filepath.Join(src.cfg.ProjectMemoryDir(src.project.ID), "MEMORY.md"), "edited-memory")
	t.Cleanup(func() { beforeFileSwap = nil })
	beforeFileSwap = func() error { return errors.New("disk full") }
	_, err = Restore(context.Background(), RestoreOptions{DataDir: src.dir, DB: sqlDB, Path: out, Confirm: true, ReplaceProject: true})
	if err == nil {
		t.Fatal("replace succeeded despite the failure")
	}
	var content string
	if err := src.st.DB().Model(&store.Message{}).Where("thread_id = ?", src.thread.ID).Select("content").Take(&content).Error; err != nil {
		t.Fatal(err)
	}
	if content != "edited-message" || readText(t, filepath.Join(src.cfg.ProjectMemoryDir(src.project.ID), "MEMORY.md")) != "edited-memory" {
		t.Fatalf("content %q after failed replace", content)
	}
}

func TestCreateWithoutAnOpenPoolAndRejectsAMissingProject(t *testing.T) {
	src := seed(t)
	if err := src.st.Close(); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "closed.zwai-backup")
	manifest, err := Create(context.Background(), Options{DataDir: src.dir, Kind: KindProject, ProjectID: src.project.ID, Output: out})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Counts.Projects != 1 {
		t.Fatalf("manifest %+v", manifest)
	}
	dest := t.TempDir()
	clearEnv(t)
	if _, err := Restore(context.Background(), RestoreOptions{DataDir: dest, Path: out, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(context.Background(), Options{DataDir: src.dir, Kind: KindProject, ProjectID: "pj_missing", Output: filepath.Join(t.TempDir(), "missing.zwai-backup")}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing project: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Create(ctx, Options{DataDir: src.dir, Kind: KindFull, Output: filepath.Join(t.TempDir(), "cancelled.zwai-backup")}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled create: %v", err)
	}
	if _, err := Create(context.Background(), Options{DataDir: src.dir, Kind: "other", Output: filepath.Join(t.TempDir(), "bad.zwai-backup")}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("kind: %v", err)
	}
	if _, err := Create(context.Background(), Options{DataDir: src.dir, Kind: KindProject, ProjectID: "../x", Output: filepath.Join(t.TempDir(), "bad.zwai-backup")}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("project id: %v", err)
	}
	if _, err := Create(context.Background(), Options{DataDir: src.dir, Kind: KindFull, Output: t.TempDir()}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("directory output: %v", err)
	}
}

func TestPreviewOfAForeignDraftScheduleAndNode(t *testing.T) {
	src := seed(t)
	out := filepath.Join(t.TempDir(), "project.zwai-backup")
	sqlDB, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Create(context.Background(), Options{DataDir: src.dir, DB: sqlDB, Kind: KindProject, ProjectID: src.project.ID, Output: out}); err != nil {
		t.Fatal(err)
	}
	var schedule store.Schedule
	if err := src.st.DB().Where("project_id = ?", src.project.ID).First(&schedule).Error; err != nil {
		t.Fatal(err)
	}
	var turn store.Turn
	if err := src.st.DB().Where("thread_id = ?", src.thread.ID).First(&turn).Error; err != nil {
		t.Fatal(err)
	}
	var node store.NuwaNode
	if err := src.st.DB().Where("project_id = ?", src.project.ID).First(&node).Error; err != nil {
		t.Fatal(err)
	}
	destDir := t.TempDir()
	clearEnv(t)
	st, err := store.Open(filepath.Join(destDir, "zwai.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	owner := &store.Project{Name: "owner"}
	if err := st.CreateProject(owner); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateThread(&store.Thread{ID: src.thread.ID, Title: "taken", ProjectID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateTurn(&store.Turn{ID: turn.ID, ThreadID: src.thread.ID, ProjectID: owner.ID, UserText: "kept", Status: store.TurnDone}); err != nil {
		t.Fatal(err)
	}
	schedule.ProjectID = owner.ID
	schedule.ThreadID = src.thread.ID
	schedule.Prompt = "kept-schedule"
	if err := st.DB().Create(&schedule).Error; err != nil {
		t.Fatal(err)
	}
	node.ProjectID = owner.ID
	node.ThreadID = src.thread.ID
	node.DependsOn = []string{}
	node.DependencyVersions = map[string]int64{}
	node.Evidence = []store.NuwaEvidence{}
	if err := st.DB().Create(&node).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveComposerDraft(context.Background(), store.ComposerDraft{
		Key: "thread:" + src.thread.ID, Text: "kept-draft", State: "editing",
	}, nil); err != nil {
		t.Fatal(err)
	}
	db, err := st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	preview, err := Preview(context.Background(), destDir, db, out)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Action != ActionBlocked {
		t.Fatalf("action %s", preview.Action)
	}
	seen := map[string]bool{}
	for _, c := range preview.Conflicts {
		seen[c.Table] = true
	}
	for _, table := range []string{"threads", "turns", "schedules", "nuwa_nodes", "composer_drafts"} {
		if !seen[table] {
			t.Fatalf("conflicts %+v missing %s", preview.Conflicts, table)
		}
	}
	_, err = Restore(context.Background(), RestoreOptions{DataDir: destDir, DB: db, Path: out, Confirm: true, ReplaceProject: true})
	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("error %v", err)
	}
	got, err := st.GetThread(src.thread.ID)
	if err != nil || got.Title != "taken" {
		t.Fatalf("thread %+v %v", got, err)
	}
}

func TestArchiveRejectionsAndGenerationFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := Generation(dir); err != nil {
		t.Fatal(err)
	}
	writeText(t, filepath.Join(dir, generationFile), "nope")
	if _, err := Generation(dir); !errors.Is(err, ErrInvalid) {
		t.Fatalf("text generation: %v", err)
	}
	writeText(t, filepath.Join(dir, generationFile), "-3")
	if _, err := Generation(dir); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative generation: %v", err)
	}
	if _, err := AdvanceGeneration(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("advance in a missing directory succeeded")
	}
	for _, name := range []string{"", `a\b`, "a:b", "/abs", "..", "../x", "foo/../../x"} {
		if _, err := safeZipPath(name); !errors.Is(err, ErrInvalid) {
			t.Fatalf("path %q: %v", name, err)
		}
	}
	if got, err := safeZipPath("files/ok"); err != nil || got != "files/ok" {
		t.Fatalf("clean path %q %v", got, err)
	}
	for _, id := range []string{"", "../x", "has.dot", strings.Repeat("a", 65)} {
		if validID(id) {
			t.Fatalf("accepted id %q", id)
		}
	}

	empty := filepath.Join(t.TempDir(), "empty.zwai-backup")
	writeZip(t, empty, func(zw *zip.Writer) {})
	if _, err := Preview(context.Background(), t.TempDir(), nil, empty); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty: %v", err)
	}
	missing := filepath.Join(t.TempDir(), "nomanifest.zwai-backup")
	writeZip(t, missing, func(zw *zip.Writer) {
		_, _ = zw.Create("database.db")
	})
	if _, err := Preview(context.Background(), t.TempDir(), nil, missing); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing manifest: %v", err)
	}
	badJSON := manifestZip(t, []byte("{"), nil)
	if _, err := openArchive(badJSON); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad manifest: %v", err)
	}
	wrongVer := manifestZip(t, mustJSON(t, Manifest{Format: 2, Kind: KindFull}), nil)
	if _, err := openArchive(wrongVer); !errors.Is(err, ErrInvalid) {
		t.Fatalf("version: %v", err)
	}
	badProject := manifestZip(t, mustJSON(t, Manifest{Format: 1, Kind: KindProject, ProjectID: "../x"}), nil)
	if _, err := openArchive(badProject); !errors.Is(err, ErrInvalid) {
		t.Fatalf("project id: %v", err)
	}
	mismatch := manifestZip(t, mustJSON(t, Manifest{Format: 1, Kind: KindFull, SHA256: map[string]string{databaseName: "00"}}), map[string][]byte{databaseName: []byte("db"), "config.yaml": []byte("x")})
	if _, err := openArchive(mismatch); !errors.Is(err, ErrInvalid) {
		t.Fatalf("checksum: %v", err)
	}
	unexpected := manifestZip(t, mustJSON(t, Manifest{Format: 1, Kind: KindFull, SHA256: map[string]string{"notes.txt": hashOf([]byte("x"))}}), map[string][]byte{"notes.txt": []byte("x")})
	if _, err := openArchive(unexpected); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unexpected: %v", err)
	}
	noDB := manifestZip(t, mustJSON(t, Manifest{Format: 1, Kind: KindFull, SHA256: map[string]string{"config.yaml": hashOf([]byte("x"))}}), map[string][]byte{"config.yaml": []byte("x")})
	if _, err := openArchive(noDB); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing database: %v", err)
	}
	withConfig := manifestZip(t, mustJSON(t, Manifest{Format: 1, Kind: KindProject, ProjectID: "pj_abc", SHA256: map[string]string{databaseName: hashOf([]byte("db")), "config.yaml": hashOf([]byte("x"))}}), map[string][]byte{databaseName: []byte("db"), "config.yaml": []byte("x")})
	if _, err := openArchive(withConfig); !errors.Is(err, ErrInvalid) {
		t.Fatalf("project config: %v", err)
	}
	dup := filepath.Join(t.TempDir(), "dup.zwai-backup")
	writeZip(t, dup, func(zw *zip.Writer) {
		_, _ = zw.Create("database.db")
		_, _ = zw.Create("database.db")
	})
	if _, err := openArchive(dup); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate: %v", err)
	}

	prevEntries := maxZipEntries
	maxZipEntries = 1
	t.Cleanup(func() { maxZipEntries = prevEntries })
	tooMany := filepath.Join(t.TempDir(), "many.zwai-backup")
	writeZip(t, tooMany, func(zw *zip.Writer) {
		_, _ = zw.Create("a")
		_, _ = zw.Create("b")
	})
	if _, err := openArchive(tooMany); !errors.Is(err, ErrInvalid) {
		t.Fatalf("entry cap: %v", err)
	}
	maxZipEntries = prevEntries
	prevBytes := maxArchiveBytes
	maxArchiveBytes = 4
	t.Cleanup(func() { maxArchiveBytes = prevBytes })
	huge := manifestZip(t, mustJSON(t, Manifest{Format: 1, Kind: KindFull, SHA256: map[string]string{databaseName: hashOf([]byte("12345")), "config.yaml": hashOf([]byte("x"))}}), map[string][]byte{databaseName: []byte("12345"), "config.yaml": []byte("x")})
	if _, err := openArchive(huge); !errors.Is(err, ErrInvalid) {
		t.Fatalf("byte cap: %v", err)
	}
}

func TestCopiedSymlinkAndHelperGuards(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	writeText(t, target, "body")
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "link")
	if err := copyTree(link, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(dst)
	if err != nil || got != target {
		t.Fatalf("link %q %v", got, err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(fifo, filepath.Join(t.TempDir(), "fifo")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("fifo: %v", err)
	}
	if _, _, skip, err := projectFilter("remote_devices", "pj_a"); err != nil || !skip {
		t.Fatalf("remote filter skip=%v %v", skip, err)
	}
	if _, _, _, err := projectFilter("not_a_table", "pj_a"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown table: %v", err)
	}
	if _, _, _, err := projectFilter("projects", "../x"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad project filter: %v", err)
	}
	for _, table := range copyOrder {
		if table == "remote_devices" {
			continue
		}
		if _, _, skip, err := projectFilter(table, "pj_a"); err != nil || skip {
			t.Fatalf("filter %s skip=%v %v", table, skip, err)
		}
	}
	if err := outsideDataDir(dir, filepath.Join(filepath.Dir(dir), "backup.zwai-backup")); err != nil {
		t.Fatal(err)
	}
	if _, err := quoteIdent("bad-name"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ident: %v", err)
	}
	if stripUserinfo("http://user:secret@example.invalid/x") != "http://example.invalid/x" {
		t.Fatalf("stripped %s", stripUserinfo("http://user:secret@example.invalid/x"))
	}
	if stripUserinfo("not a url") != "not a url" || stripUserinfo("") != "" {
		t.Fatal("plain proxy changed")
	}
	if !sameColumns(nil, nil) || sameColumns([]column{{name: "id"}}, nil) || sameColumns([]column{{name: "id", integer: true}}, []column{{name: "id"}}) {
		t.Fatal("column comparison")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func manifestZip(t *testing.T, manifest []byte, files map[string][]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "crafted.zwai-backup")
	writeZip(t, path, func(zw *zip.Writer) {
		w, err := zw.Create(manifestName)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(manifest); err != nil {
			t.Fatal(err)
		}
		for name, body := range files {
			w, err := zw.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(body); err != nil {
				t.Fatal(err)
			}
		}
	})
	return path
}
