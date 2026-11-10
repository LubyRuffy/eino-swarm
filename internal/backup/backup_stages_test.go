package backup

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestStageFailuresStayOnTheFailingStep(t *testing.T) {
	ctx := context.Background()
	dir, st, conn := opened(t)
	defer st.Close()

	t.Run("vacuum cannot replace a directory", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "blocked")
		if err := os.Mkdir(dest, 0o700); err != nil {
			t.Fatal(err)
		}
		writeText(t, filepath.Join(dest, "inside"), "x")
		if err := vacuumInto(ctx, conn, dest); err == nil {
			t.Fatal("vacuum removed a non-empty directory")
		}
	})
	t.Run("attach rejects a bad schema", func(t *testing.T) {
		if err := attach(ctx, conn, "bad-name", filepath.Join(t.TempDir(), "a.db")); !errors.Is(err, ErrInvalid) {
			t.Fatalf("attach: %v", err)
		}
	})
	t.Run("missing table has no columns", func(t *testing.T) {
		if _, err := tableColumns(ctx, conn, "main", "not_a_table"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("columns: %v", err)
		}
	})
	t.Run("hyphenated column is not an identifier", func(t *testing.T) {
		left := rawSQLite(t)
		if _, err := left.Exec(`CREATE TABLE projects ("bad-name" TEXT)`); err != nil {
			t.Fatal(err)
		}
		rightPath := filepath.Join(t.TempDir(), "right.db")
		execSQLite(t, rightPath, `CREATE TABLE projects ("bad-name" TEXT)`)
		c, err := left.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if err := attach(ctx, c, "other", rightPath); err != nil {
			t.Fatal(err)
		}
		if err := copyTable(ctx, c, "main", "other", "projects", "", nil, 0); !errors.Is(err, ErrInvalid) {
			t.Fatalf("copy: %v", err)
		}
	})
	t.Run("full build stops when the snapshot cannot be read", func(t *testing.T) {
		if _, _, cleanup, err := buildFull(ctx, closedConn(t), dir, false); err == nil {
			if cleanup != nil {
				cleanup()
			}
			t.Fatal("built from a closed database")
		}
	})
	t.Run("full build stops on a broken config", func(t *testing.T) {
		broken := t.TempDir()
		clearEnv(t)
		if _, err := config.Load(broken); err != nil {
			t.Fatal(err)
		}
		writeText(t, filepath.Join(broken, "config.yaml"), ":\n")
		bst, err := store.Open(filepath.Join(broken, "zwai.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer bst.Close()
		bdb, err := bst.DB().DB()
		if err != nil {
			t.Fatal(err)
		}
		bconn, err := bdb.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer bconn.Close()
		if _, _, _, err := buildFull(ctx, bconn, broken, false); err == nil {
			t.Fatal("built with a broken config")
		}
	})
	t.Run("full build stops on a linked tree", func(t *testing.T) {
		linked := t.TempDir()
		clearEnv(t)
		if _, err := config.Load(linked); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(filepath.Join(linked, "projects")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), filepath.Join(linked, "projects")); err != nil {
			t.Fatal(err)
		}
		lst, err := store.Open(filepath.Join(linked, "zwai.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer lst.Close()
		ldb, err := lst.DB().DB()
		if err != nil {
			t.Fatal(err)
		}
		lconn, err := ldb.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer lconn.Close()
		if _, _, _, err := buildFull(ctx, lconn, linked, false); err == nil {
			t.Fatal("built a symlink tree")
		}
	})
	t.Run("project build stops on a closed database", func(t *testing.T) {
		if _, _, _, err := buildProject(ctx, closedConn(t), dir, "pj_missing"); err == nil {
			t.Fatal("built a project from a closed database")
		}
	})
	t.Run("export stops when the live schema gains a table", func(t *testing.T) {
		if _, err := conn.ExecContext(ctx, `CREATE TABLE extra(id INTEGER)`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = conn.ExecContext(context.Background(), `DROP TABLE extra`) })
		if err := exportProjectRows(ctx, conn, "pj_missing", filepath.Join(t.TempDir(), "out.db"), true); !errors.Is(err, ErrInvalid) {
			t.Fatalf("export: %v", err)
		}
	})
	t.Run("replace stops on a directory", func(t *testing.T) {
		if err := replaceAll(ctx, conn, t.TempDir()); err == nil {
			t.Fatal("replaced from a directory")
		}
	})
	t.Run("a delete guard stops a full replace", func(t *testing.T) {
		incoming := filepath.Join(t.TempDir(), "incoming.db")
		ist, err := store.Open(incoming)
		if err != nil {
			t.Fatal(err)
		}
		if err := ist.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO projects(id, name, created_at, updated_at) VALUES('pj_block', 'block', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, `CREATE TRIGGER block_project_delete BEFORE DELETE ON projects BEGIN SELECT RAISE(ABORT, 'no'); END`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = conn.ExecContext(context.Background(), `DROP TRIGGER block_project_delete`)
		})
		if err := replaceAll(ctx, conn, incoming); err == nil {
			t.Fatal("replace deleted through the guard")
		}
	})
	t.Run("conflict scan stops when a later table is gone", func(t *testing.T) {
		incoming := filepath.Join(t.TempDir(), "incoming.db")
		ist, err := store.Open(incoming)
		if err != nil {
			t.Fatal(err)
		}
		if err := ist.Close(); err != nil {
			t.Fatal(err)
		}
		execSQLite(t, incoming, `DROP TABLE composer_drafts`)
		if err := attach(ctx, conn, "incoming", incoming); err != nil {
			t.Fatal(err)
		}
		defer detach(context.Background(), conn, "incoming")
		if _, err := findConflicts(ctx, conn, "pj_missing", false); err == nil {
			t.Fatal("conflicts ignored a missing table")
		}
	})
	t.Run("fifo in a tree is not a backup file", func(t *testing.T) {
		root := filepath.Join(dir, "inputs")
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := syscallMkfifo(filepath.Join(root, "pipe")); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(filepath.Join(root, "pipe")) })
		if _, err := collectTree(dir, "inputs"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("fifo: %v", err)
		}
	})
	t.Run("preview and restore reject a file that is not a backup", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "nope.zwai-backup")
		writeText(t, bad, "not a zip")
		if _, err := Preview(ctx, dir, nil, bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("preview: %v", err)
		}
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := Restore(cancelCtx, RestoreOptions{DataDir: dir, Path: bad, Confirm: true, Replace: true}); !errors.Is(err, context.Canceled) {
			t.Fatalf("restore: %v", err)
		}
	})
	t.Run("archive writes refuse a file used as a directory", func(t *testing.T) {
		parent := filepath.Join(t.TempDir(), "file")
		writeText(t, parent, "x")
		if _, err := writeArchive(filepath.Join(parent, "out.zwai-backup"), nil, Manifest{}); err == nil {
			t.Fatal("created a directory through a file")
		}
		if _, err := openArchive(parent); !errors.Is(err, ErrInvalid) {
			t.Fatalf("open: %v", err)
		}
	})
	t.Run("generation rename refuses a directory", func(t *testing.T) {
		gen := t.TempDir()
		if err := os.Mkdir(filepath.Join(gen, generationFile), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := AdvanceGeneration(gen); err == nil {
			t.Fatal("renamed the generation onto a directory")
		}
	})
}

func TestRestoreRollbackReportsAMissingSnapshot(t *testing.T) {
	clearEnv(t)
	srcDir := t.TempDir()
	if _, err := config.Load(srcDir); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(srcDir, "zwai.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateProject(&store.Project{Name: "kept"}); err != nil {
		t.Fatal(err)
	}
	db, err := st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "full.zwai-backup")
	if _, err := Create(context.Background(), Options{DataDir: srcDir, DB: db, Kind: KindFull, Output: out}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if _, err := config.Load(dest); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { beforeFileSwap = nil })
	beforeFileSwap = func() error {
		matches, err := filepath.Glob(filepath.Join(dest, ".backup-vacuum-*"))
		if err != nil {
			return err
		}
		for _, match := range matches {
			if err := os.Remove(match); err != nil {
				return err
			}
		}
		return errors.New("disk full")
	}
	_, err = Restore(context.Background(), RestoreOptions{DataDir: dest, Path: out, Confirm: true, Replace: true})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("restore error %v", err)
	}
}

func TestCallersReturnAnInjectedFailure(t *testing.T) {
	if backupRace {
		t.Skip("injected-failure sweep is sequential; the race run covers the same calls without the counter")
	}
	t.Cleanup(func() { failAfter = 0 })
	clearEnv(t)
	dir := t.TempDir()
	if _, err := config.Load(dir); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "zwai.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	project := &store.Project{Name: "one"}
	if err := st.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	db, err := st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(t.TempDir(), "full.zwai-backup")
	one := filepath.Join(t.TempDir(), "one.zwai-backup")
	ctx := context.Background()
	for n := 1; n <= 320; n++ {
		failAfter = n
		err := exerciseBackup(t, ctx, dir, db, project.ID, full, one)
		failAfter = 0
		if err == nil {
			return
		}
		if !strings.Contains(err.Error(), "injected failure") {
			t.Fatalf("step %d: %v", n, err)
		}
	}
	t.Fatal("injected failures never ran out")
}

func exerciseBackup(t *testing.T, ctx context.Context, dir string, db *sql.DB, projectID, full, one string) error {
	t.Helper()
	if _, err := Create(ctx, Options{DataDir: dir, DB: db, Kind: KindFull, Output: full}); err != nil {
		return err
	}
	if _, err := Restore(ctx, RestoreOptions{DataDir: t.TempDir(), Path: full, Confirm: true, Replace: true}); err != nil {
		return err
	}
	omit := full + ".omit"
	if _, err := Create(ctx, Options{DataDir: dir, DB: db, Kind: KindFull, OmitSecrets: true, Output: omit}); err != nil {
		return err
	}
	if _, err := Restore(ctx, RestoreOptions{DataDir: t.TempDir(), Path: omit, Confirm: true, Replace: true}); err != nil {
		return err
	}
	if _, err := Create(ctx, Options{DataDir: dir, DB: db, Kind: KindProject, ProjectID: projectID, Output: one}); err != nil {
		return err
	}
	if _, err := Restore(ctx, RestoreOptions{DataDir: t.TempDir(), Path: one, Confirm: true}); err != nil {
		return err
	}
	_, err := Restore(ctx, RestoreOptions{DataDir: t.TempDir(), Path: one, Confirm: true, ReplaceProject: true})
	return err
}

func opened(t *testing.T) (string, *store.Store, *sql.Conn) {
	t.Helper()
	dir := t.TempDir()
	clearEnv(t)
	if _, err := config.Load(dir); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "zwai.db"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return dir, st, conn
}
