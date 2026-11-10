package backup

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestReadStringsRejectsNullAndStopsOnTheRow(t *testing.T) {
	db := rawSQLite(t)
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := readStrings(context.Background(), conn, `SELECT NULL`, nil, 1, func([]string) error { return nil }); err == nil {
		t.Fatal("scanned NULL into text")
	}
	if err := readStrings(context.Background(), conn, `SELECT 'x'`, nil, 1, func([]string) error { return errors.New("stop") }); err == nil || err.Error() != "stop" {
		t.Fatalf("row: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := readStrings(context.Background(), conn, `SELECT 'x'`, nil, 1, func([]string) error { return nil }); err == nil {
		t.Fatal("read a closed connection")
	}
}

func TestSmallFailuresCoverTheRemainingChecks(t *testing.T) {
	ro := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(ro, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	if _, err := tempFile(ro, "backup-*"); err == nil {
		t.Fatal("created a temp file in a read-only directory")
	}
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	writeText(t, blocked, "x")
	if _, _, err := acquire(context.Background(), blocked, nil); err == nil {
		t.Fatal("opened a database inside a file")
	}
	if err := copyTree(filepath.Join(t.TempDir(), "missing"), filepath.Join(t.TempDir(), "dst")); err == nil {
		t.Fatal("copied a missing file")
	}
	parent := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(parent, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o755) })
	if _, err := collectTree(parent, "child"); err == nil {
		t.Fatal("read a directory without permission")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink("target", link); err != nil {
		t.Fatal(err)
	}
	fileParent := filepath.Join(t.TempDir(), "file")
	writeText(t, fileParent, "x")
	if err := copyTree(link, filepath.Join(fileParent, "child")); err == nil {
		t.Fatal("created a directory through a file")
	}

	ctx := context.Background()
	left := rawSQLite(t)
	if _, err := left.Exec(`CREATE TABLE projects(id TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := left.Exec(`INSERT INTO projects(id) VALUES ('')`); err != nil {
		t.Fatal(err)
	}
	rightPath := filepath.Join(t.TempDir(), "right.db")
	execSQLite(t, rightPath, `CREATE TABLE projects(id TEXT)`)
	execSQLite(t, rightPath, `INSERT INTO projects(id) VALUES ('')`)
	conn, err := left.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := attach(ctx, conn, "incoming", rightPath); err != nil {
		t.Fatal(err)
	}
	defer detach(context.Background(), conn, "incoming")
	if _, err := findConflicts(ctx, conn, "pj_ok", false); err == nil {
		t.Fatal("conflicts accepted a database without the other tables")
	}
}

func TestQuotedSnapshotAndAttachStillRoundTrip(t *testing.T) {
	dir, st, conn := liveConn(t)
	defer st.Close()
	quoteSQLPath = true
	t.Cleanup(func() { quoteSQLPath = false })
	dest := filepath.Join(dir, "o'clock.db")
	if err := vacuumInto(context.Background(), conn, dest); err != nil {
		t.Fatal(err)
	}
	if err := attach(context.Background(), conn, "quoted", dest); err != nil {
		t.Fatal(err)
	}
	detach(context.Background(), conn, "quoted")
	detach(context.Background(), conn, "not-an-ident")
	if _, err := safeZipPath("."); !errors.Is(err, ErrInvalid) {
		t.Fatalf("dot path: %v", err)
	}
}

func TestClosedDatabaseAndBadIdentifiersFailClosed(t *testing.T) {
	conn := closedConn(t)
	ctx := context.Background()
	if err := vacuumInto(ctx, conn, filepath.Join(t.TempDir(), "v.db")); err == nil {
		t.Fatal("vacuum on a closed database succeeded")
	}
	if err := attach(ctx, conn, "snap", filepath.Join(t.TempDir(), "a.db")); err == nil {
		t.Fatal("attach on a closed database succeeded")
	}
	if err := checkpoint(ctx, conn); err == nil {
		t.Fatal("checkpoint on a closed database succeeded")
	}
	if err := knownTables(ctx, conn, "main"); err == nil {
		t.Fatal("known tables on a closed database succeeded")
	}
	if err := knownTables(ctx, conn, "bad-name"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("schema: %v", err)
	}
	if _, err := tableColumns(ctx, conn, "bad-name", "projects"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("columns: %v", err)
	}
	if err := copyTable(ctx, conn, "main", "main", "projects", "", nil, 0); err == nil {
		t.Fatal("copy on a closed database succeeded")
	}
	if err := deleteProjectRows(ctx, conn, "pj_missing"); err == nil {
		t.Fatal("delete on a closed database succeeded")
	}
	if _, err := maxAutoinc(ctx, conn); err == nil {
		t.Fatal("max id on a closed database succeeded")
	}
	if err := bumpSequences(ctx, conn, "main"); err == nil {
		t.Fatal("sequence on a closed database succeeded")
	}
	if _, err := countSchema(ctx, conn, "main"); err == nil {
		t.Fatal("count on a closed database succeeded")
	}
	if _, err := countSchema(ctx, conn, "bad-name"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("count schema: %v", err)
	}
	if _, err := listThreads(ctx, conn, "main", "pj_ok"); err == nil {
		t.Fatal("threads on a closed database succeeded")
	}
	if _, err := listThreads(ctx, conn, "main", "../x"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("thread project: %v", err)
	}
	if _, err := listExternal(ctx, conn, "main", "pj_ok"); err == nil {
		t.Fatal("external on a closed database succeeded")
	}
	if _, err := listExternal(ctx, conn, "bad-name", "pj_ok"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("external schema: %v", err)
	}
	if conflicts, err := findConflicts(ctx, conn, "pj_ok", false); err == nil || conflicts != nil {
		t.Fatalf("conflicts %v %v", conflicts, err)
	}
	if _, err := projectExists(ctx, conn, "pj_ok"); err == nil {
		t.Fatal("exists on a closed database succeeded")
	}
	owner, owned := draftOwner(ctx, conn, "thread:missing", "pj_ok")
	if owner != "" || owned {
		t.Fatalf("draft owner %q %v", owner, owned)
	}
	if owner, owned = draftOwner(ctx, conn, "new:pj_ok", "pj_ok"); owner != "pj_ok" || !owned {
		t.Fatalf("owned draft %q %v", owner, owned)
	}
	if owner, owned = draftOwner(ctx, conn, "new:pj_other", "pj_ok"); owner != "pj_other" || owned {
		t.Fatalf("foreign draft %q %v", owner, owned)
	}
	if owner, owned = draftOwner(ctx, conn, "plain", "pj_ok"); owner != "" || owned {
		t.Fatalf("plain draft %q %v", owner, owned)
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if err := withFKOff(cancelCtx, conn, func() error { return nil }); err == nil {
		t.Fatal("foreign keys on a cancelled context succeeded")
	}
	if err := withTx(cancelCtx, conn, func(*sql.Tx) error { return nil }); err == nil {
		t.Fatal("transaction on a cancelled context succeeded")
	}
}

func TestSchemaGuardsRejectASurpriseTableAndAMismatch(t *testing.T) {
	_, st, conn := liveConn(t)
	defer st.Close()
	ctx := context.Background()
	if _, err := conn.ExecContext(ctx, `CREATE TABLE extra(id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if err := knownTables(ctx, conn, "main"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("extra table: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `DROP TABLE extra`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `DROP TABLE remote_devices`); err != nil {
		t.Fatal(err)
	}
	if err := knownTables(ctx, conn, "main"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing table: %v", err)
	}

	left := rawSQLite(t)
	rightPath := filepath.Join(t.TempDir(), "right.db")
	if _, err := left.Exec(`CREATE TABLE projects (id TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := left.Exec(`INSERT INTO projects(id) VALUES ('x')`); err != nil {
		t.Fatal(err)
	}
	execSQLite(t, rightPath, `CREATE TABLE projects (id TEXT CHECK(id <> 'x'))`)
	leftConn, err := left.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer leftConn.Close()
	if err := attach(ctx, leftConn, "other", rightPath); err != nil {
		t.Fatal(err)
	}
	if err := copyTable(ctx, leftConn, "main", "other", "projects", "", nil, 0); err == nil {
		t.Fatal("copy ignored the destination check")
	}
	detach(context.Background(), leftConn, "other")
	execSQLite(t, rightPath, `DROP TABLE projects`)
	execSQLite(t, rightPath, `CREATE TABLE projects (id TEXT, name TEXT)`)
	if err := attach(ctx, leftConn, "other", rightPath); err != nil {
		t.Fatal(err)
	}
	if err := copyTable(ctx, leftConn, "main", "other", "projects", "", nil, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("schema mismatch: %v", err)
	}

	if err := withTx(ctx, leftConn, func(*sql.Tx) error { return errors.New("rollback") }); err == nil {
		t.Fatal("failed transaction committed")
	}
}

func TestSequenceRepairAndRejectedThreadIDs(t *testing.T) {
	plain := rawSQLite(t)
	if _, err := plain.Exec(`CREATE TABLE messages(id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	plainConn, err := plain.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer plainConn.Close()
	if err := bumpSequences(context.Background(), plainConn, "main"); err != nil {
		t.Fatal(err)
	}

	seq := rawSQLite(t)
	for _, table := range autoincTables {
		if _, err := seq.Exec(`CREATE TABLE ` + table + `(id INTEGER PRIMARY KEY AUTOINCREMENT)`); err != nil {
			t.Fatal(err)
		}
	}
	empty, err := seq.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	n, err := maxAutoinc(context.Background(), empty)
	_ = empty.Close()
	if err != nil || n != 0 {
		t.Fatalf("max %d %v", n, err)
	}
	if _, err := seq.Exec(`INSERT INTO messages(id) VALUES (3)`); err != nil {
		t.Fatal(err)
	}
	if _, err := seq.Exec(`DELETE FROM sqlite_sequence`); err != nil {
		t.Fatal(err)
	}
	seqConn, err := seq.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer seqConn.Close()
	if err := bumpSequences(context.Background(), seqConn, "main"); err != nil {
		t.Fatal(err)
	}
	var got int64
	if err := seqConn.QueryRowContext(context.Background(), `SELECT seq FROM sqlite_sequence WHERE name = 'messages'`).Scan(&got); err != nil || got != 3 {
		t.Fatalf("inserted seq %d %v", got, err)
	}
	if _, err := seqConn.ExecContext(context.Background(), `UPDATE sqlite_sequence SET seq = 1 WHERE name = 'messages'`); err != nil {
		t.Fatal(err)
	}
	if err := bumpSequences(context.Background(), seqConn, "main"); err != nil {
		t.Fatal(err)
	}
	if err := seqConn.QueryRowContext(context.Background(), `SELECT seq FROM sqlite_sequence WHERE name = 'messages'`).Scan(&got); err != nil || got != 3 {
		t.Fatalf("raised seq %d %v", got, err)
	}
	if _, err := seqConn.ExecContext(context.Background(), `UPDATE sqlite_sequence SET seq = 9 WHERE name = 'messages'`); err != nil {
		t.Fatal(err)
	}
	if err := bumpSequences(context.Background(), seqConn, "main"); err != nil {
		t.Fatal(err)
	}
	if err := seqConn.QueryRowContext(context.Background(), `SELECT seq FROM sqlite_sequence WHERE name = 'messages'`).Scan(&got); err != nil || got != 9 {
		t.Fatalf("kept seq %d %v", got, err)
	}
	if err := bumpSequences(context.Background(), seqConn, "bad-name"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("sequence schema: %v", err)
	}

	_, st, conn := liveConn(t)
	defer st.Close()
	// The store keeps a single connection. Release the one this test checked
	// out before asking the store to write, then take it back to read.
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	project := &store.Project{Name: "ids"}
	if err := st.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateThread(&store.Thread{ID: "bad.id", Title: "bad", ProjectID: project.ID}); err != nil {
		t.Fatal(err)
	}
	db, err := st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	conn, err = db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := listThreads(context.Background(), conn, "main", project.ID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad thread id: %v", err)
	}
	if _, err := listExternal(context.Background(), conn, "main", "../x"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("external project: %v", err)
	}
}

func TestProjectArchiveMustHoldExactlyTheNamedProject(t *testing.T) {
	incoming := filepath.Join(t.TempDir(), "incoming.db")
	st, err := store.Open(incoming)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateProject(&store.Project{Name: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateProject(&store.Project{Name: "two"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	_, dest, conn := liveConn(t)
	defer dest.Close()
	if err := applyProject(context.Background(), conn, incoming, "pj_target", false, false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("two projects: %v", err)
	}

	one := filepath.Join(t.TempDir(), "one.db")
	st, err = store.Open(one)
	if err != nil {
		t.Fatal(err)
	}
	only := &store.Project{Name: "only"}
	if err := st.CreateProject(only); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if err := applyProject(context.Background(), conn, one, "pj_other", false, false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong project: %v", err)
	}
	if err := prepareExportDB(t.TempDir()); err == nil {
		t.Fatal("prepared a directory as a database")
	}
}

func TestFileSnapshotRestoreAndRejectedTrees(t *testing.T) {
	dir := t.TempDir()
	writeText(t, filepath.Join(dir, "projects", "pj_a", "memory", "MEMORY.md"), "original")
	writeText(t, filepath.Join(dir, "config.yaml"), "name: original")
	snap, err := snapshotPaths(dir, []string{"projects/pj_a", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	defer snap.remove()
	writeText(t, filepath.Join(dir, "projects", "pj_a", "memory", "MEMORY.md"), "changed")
	writeText(t, filepath.Join(dir, "config.yaml"), "name: changed")
	if err := snap.restore(); err != nil {
		t.Fatal(err)
	}
	if readText(t, filepath.Join(dir, "projects", "pj_a", "memory", "MEMORY.md")) != "original" {
		t.Fatal("restore left the edited memory")
	}
	if readText(t, filepath.Join(dir, "config.yaml")) != "name: original" {
		t.Fatal("restore left the edited config")
	}
	if err := snap.apply(t.TempDir(), []byte("name: applied")); err != nil {
		t.Fatal(err)
	}
	if readText(t, filepath.Join(dir, "config.yaml")) != "name: applied" {
		t.Fatal("apply did not write config")
	}
	if _, err := os.Stat(filepath.Join(dir, "projects", "pj_a")); !os.IsNotExist(err) {
		t.Fatal("apply kept a tree the extracted directory did not have")
	}

	link := filepath.Join(dir, "library")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	if _, err := collectTree(dir, "library"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("symlink tree: %v", err)
	}
	if items, err := collectTree(dir, "does-not-exist"); err != nil || items != nil {
		t.Fatalf("missing tree %+v %v", items, err)
	}
	fifoDir := filepath.Join(dir, "plans")
	if err := os.MkdirAll(fifoDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscallMkfifo(filepath.Join(fifoDir, "pipe")); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshotPaths(dir, []string{"plans"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("fifo snapshot: %v", err)
	}
	modeFile := filepath.Join(dir, "zero")
	writeText(t, modeFile, "body")
	if err := os.Chmod(modeFile, 0); err != nil {
		t.Fatal(err)
	}
	if err := copyTree(modeFile, filepath.Join(t.TempDir(), "zero")); err == nil {
		t.Fatal("copied a file the process cannot read")
	}

	if _, err := projectRels("../x", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("project rels: %v", err)
	}
	if _, err := projectRels("pj_a", []string{"../y"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("thread rels: %v", err)
	}
	rels, err := projectRels("pj_a", []string{"th_a", "th_a"})
	if err != nil || len(rels) != 4 {
		t.Fatalf("rels %+v %v", rels, err)
	}
	if err := extractRel(t.TempDir(), "notes.txt", []byte("note"), false); err != nil {
		t.Fatal(err)
	}
	if err := extractRel(t.TempDir(), "dir", nil, true); err != nil {
		t.Fatal(err)
	}
	if err := extractRel(t.TempDir(), "../escape", []byte("x"), false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("escape: %v", err)
	}
}

func TestReadOnlyDirectoryAndCancelledOpenStopBeforeWriting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := acquire(ctx, t.TempDir(), nil); err == nil {
		t.Fatal("acquire used a cancelled context")
	}
	ro := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(ro, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
	if _, err := snapshotPaths(ro, []string{"projects"}); err == nil {
		t.Fatal("snapshot wrote into a read-only directory")
	}
	if _, err := AdvanceGeneration(ro); err == nil {
		t.Fatal("generation advanced in a read-only directory")
	}
	genDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(genDir, generationFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Generation(genDir); err == nil {
		t.Fatal("read a directory as the generation")
	}

	manifest, err := writeArchive(filepath.Join(ro, "out.zwai-backup"), nil, Manifest{})
	if err == nil {
		t.Fatalf("wrote into a read-only directory: %+v", manifest)
	}
	missing := filepath.Join(t.TempDir(), "missing-src.zwai-backup")
	if _, err := writeArchive(missing, []zipItem{{name: "database.db", path: filepath.Join(t.TempDir(), "absent")}}, Manifest{Kind: KindFull}); err == nil {
		t.Fatal("archived a missing file")
	}
	blocked := filepath.Join(t.TempDir(), "blocked")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := writeArchive(blocked, []zipItem{{name: "database.db", body: []byte("db")}}, Manifest{Kind: KindFull}); err == nil {
		t.Fatal("renamed a backup onto a directory")
	}
}

func TestPreviewWithoutADatabaseAndAStandaloneIdentity(t *testing.T) {
	src := seed(t)
	out := filepath.Join(t.TempDir(), "project.zwai-backup")
	sqlDB, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Create(context.Background(), Options{DataDir: src.dir, DB: sqlDB, Kind: KindProject, ProjectID: src.project.ID, Output: out}); err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir()
	preview, err := Preview(context.Background(), empty, nil, out)
	if err != nil || preview.Action != ActionAddProject || len(preview.Conflicts) != 0 {
		t.Fatalf("empty preview %+v %v", preview, err)
	}
	if _, err := os.Stat(filepath.Join(empty, "zwai.db")); !os.IsNotExist(err) {
		t.Fatal("preview created a database")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Preview(ctx, empty, nil, out); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled preview: %v", err)
	}
	if _, err := Restore(context.Background(), RestoreOptions{DataDir: empty, Path: out}); !errors.Is(err, ErrConfirm) {
		t.Fatalf("unconfirmed restore: %v", err)
	}

	destDir := t.TempDir()
	clearEnv(t)
	st, err := store.Open(filepath.Join(destDir, "zwai.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.CreateThread(&store.Thread{ID: src.thread.ID, Title: "standalone"}); err != nil {
		t.Fatal(err)
	}
	db, err := st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := Preview(context.Background(), destDir, db, out)
	if err != nil || blocked.Action != ActionBlocked {
		t.Fatalf("standalone preview %+v %v", blocked, err)
	}
	var saw bool
	for _, c := range blocked.Conflicts {
		if c.Table == "threads" && c.Owner == "standalone" {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("conflicts %+v", blocked.Conflicts)
	}
}

func TestSecretOverlayAndConfigSnapshot(t *testing.T) {
	dst := &config.Config{
		Models: config.ModelsConfig{Providers: []config.Provider{{ID: "p", APIKey: ""}}},
		MCP:    config.MCPConfig{Servers: []config.MCPServer{{ID: "s"}}},
	}
	src := &config.Config{
		Models: config.ModelsConfig{Providers: []config.Provider{{ID: "p", APIKey: "kept"}}},
		MCP: config.MCPConfig{Servers: []config.MCPServer{{
			ID: "s", Env: map[string]string{"TOKEN": "v"}, Headers: map[string]string{"Authorization": "v"},
		}}},
		Tools: config.ToolsConfig{Proxy: config.ProxyConfig{HTTP: "http://proxy", HTTPS: "http://secure-proxy"}},
	}
	overlayKeptSecrets(dst, src)
	if dst.Models.Providers[0].APIKey != "kept" || dst.MCP.Servers[0].Headers["Authorization"] != "v" || dst.Tools.Proxy.HTTPS != "http://secure-proxy" {
		t.Fatalf("overlay %+v", dst)
	}
	if err := keepDestinationSecrets(t.TempDir(), ""); err != nil {
		t.Fatalf("empty destination: %v", err)
	}
	missing := filepath.Join(t.TempDir(), "none.yaml")
	if _, err := readConfigFile(missing); err == nil {
		t.Fatal("read a missing config")
	}
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	writeText(t, bad, ":\n")
	if _, err := readConfigFile(bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad config: %v", err)
	}
}

func TestCraftedArchiveRejectsAMissingConfigAndAMissingHash(t *testing.T) {
	dbHash := hashOf([]byte("db"))
	noConfig := manifestZip(t, mustJSON(t, Manifest{Format: 1, Kind: KindFull, SHA256: map[string]string{databaseName: dbHash}}), map[string][]byte{databaseName: []byte("db")})
	if _, err := openArchive(noConfig); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing config: %v", err)
	}
	missingHash := manifestZip(t, mustJSON(t, Manifest{Format: 1, Kind: KindFull, SHA256: map[string]string{databaseName: dbHash, "config.yaml": hashOf([]byte("x")), "files/ghost": "00"}}), map[string][]byte{databaseName: []byte("db"), "config.yaml": []byte("x")})
	if _, err := openArchive(missingHash); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing entry: %v", err)
	}
}

func liveConn(t *testing.T) (string, *store.Store, *sql.Conn) {
	t.Helper()
	dir := t.TempDir()
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

func closedConn(t *testing.T) *sql.Conn {
	t.Helper()
	_, st, conn := liveConn(t)
	t.Cleanup(func() { _ = st.Close() })
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	return conn
}

func rawSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "raw.db")+"?_pragma=busy_timeout(1000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func execSQLite(t *testing.T, path, query string) {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(1000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(query); err != nil {
		t.Fatal(err)
	}
}

func syscallMkfifo(path string) error {
	return syscall.Mkfifo(path, 0o600)
}
