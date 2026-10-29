package history

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func projectScopeFixture(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "project-history.sqlite")
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	statements := []string{
		`CREATE TABLE projects(id TEXT PRIMARY KEY,name TEXT)`,
		`CREATE TABLE threads(id TEXT PRIMARY KEY,project_id TEXT,archived INTEGER)`,
		`CREATE TABLE messages(id TEXT PRIMARY KEY,thread_id TEXT,content TEXT)`,
		`CREATE TABLE turns(id TEXT PRIMARY KEY,thread_id TEXT,final TEXT)`,
		`CREATE TABLE events(id TEXT PRIMARY KEY,thread_id TEXT,kind TEXT,text TEXT)`,
		`CREATE TABLE attachments(id TEXT PRIMARY KEY,thread_id TEXT,name TEXT)`,
		`CREATE TABLE llm_calls(id TEXT PRIMARY KEY,thread_id TEXT,model TEXT)`,
		`CREATE TABLE schedules(id TEXT PRIMARY KEY,thread_id TEXT,project_id TEXT,origin_thread_id TEXT)`,
		`CREATE TABLE schedule_runs(id TEXT PRIMARY KEY,thread_id TEXT,schedule_id TEXT,summary TEXT)`,
		`CREATE TABLE pending_schedule_answers(payload TEXT)`,
		`INSERT INTO projects VALUES ('current','Current'),('foreign','Foreign')`,
		`INSERT INTO threads VALUES ('current-thread','current',0),('archived-thread','current',1),('foreign-thread','foreign',0),('standalone-thread','',0)`,
		`INSERT INTO schedules VALUES ('current-wake','current-thread','','foreign-thread'),('current-job','','current','foreign-thread'),('foreign-target-stale-project','foreign-thread','current','current-thread'),('foreign-job','','foreign','current-thread'),('global-job','','','current-thread'),('missing-thread','missing','current','current-thread'),('null-thread-current',NULL,'current','foreign-thread')`,
		`INSERT INTO schedule_runs VALUES ('current-result','current-thread','foreign-job','own result'),('foreign-result','foreign-thread','current-job','foreign result'),('current-empty','','current-job','own empty'),('current-null',NULL,'current-wake','own null'),('global-empty','','global-job','global'),('foreign-empty','','foreign-job','foreign'),('orphan-empty','','missing','orphan'),('missing-thread','missing','current-job','unknown')`,
		`INSERT INTO events VALUES ('checkpoint','current-thread','engine_restart_checkpoint','private checkpoint')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, thread := range []string{"current-thread", "archived-thread", "foreign-thread", "standalone-thread"} {
		for _, table := range []string{"messages", "turns", "events", "attachments", "llm_calls"} {
			statement := `INSERT INTO ` + table + ` VALUES (?,?,?)`
			args := []any{thread, thread, "original bytes"}
			if table == "events" {
				statement = `INSERT INTO events VALUES (?,?,?,?)`
				args = []any{thread, thread, "tool_result", "original observation"}
			}
			if _, err := db.Exec(statement, args...); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := db.Exec(`INSERT INTO llm_calls VALUES ('background','','auxiliary')`); err != nil {
		t.Fatal(err)
	}
	return path, db
}

func queryScopeIDs(t *testing.T, reader *Reader, statement string, params []any) []string {
	t.Helper()
	got, err := reader.Query(context.Background(), statement, params, 10000)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, row := range got.Rows {
		out = append(out, row[0].(string))
	}
	return out
}

func TestProjectQueriesUseRealOwnershipAcrossAllNineRawTables(t *testing.T) {
	path, _ := projectScopeFixture(t)
	reader := NewProject(path, "current")
	want := map[string][]string{
		"projects": {"current"}, "threads": {"archived-thread", "current-thread"},
		"messages": {"archived-thread", "current-thread"}, "turns": {"archived-thread", "current-thread"},
		"events": {"archived-thread", "current-thread"}, "attachments": {"archived-thread", "current-thread"}, "llm_calls": {"archived-thread", "current-thread"},
		"schedules":     {"current-job", "current-wake", "null-thread-current"},
		"schedule_runs": {"current-empty", "current-null", "current-result"},
	}
	for table, ids := range want {
		t.Run(table, func(t *testing.T) {
			got := queryScopeIDs(t, reader, "SELECT id FROM "+table+" ORDER BY id", nil)
			if !reflect.DeepEqual(got, ids) {
				t.Fatalf("project view exposed wrong owners: table=%s got=%v want=%v", table, got, ids)
			}
		})
	}
}

func TestProjectScopeSurvivesMissingWhereCTEsAliasesUnionAndIgnoredArguments(t *testing.T) {
	path, _ := projectScopeFixture(t)
	reader := NewProject(path, "current")
	for _, query := range []string{
		`SELECT id FROM threads ORDER BY id`,
		`WITH copied AS (SELECT * FROM threads) SELECT id FROM copied ORDER BY id`,
		`SELECT t.id FROM threads t JOIN projects p ON p.id=t.project_id ORDER BY t.id`,
		`SELECT id FROM threads UNION SELECT thread_id FROM messages ORDER BY id`,
	} {
		if got := queryScopeIDs(t, reader, query, []any{"current"}); !reflect.DeepEqual(got, []string{"archived-thread", "current-thread"}) {
			t.Fatalf("unfiltered/ignored-param query leaked other projects: %s -> %v", query, got)
		}
	}
	if got := queryScopeIDs(t, reader, `SELECT id FROM threads WHERE project_id=?`, []any{"foreign"}); len(got) != 0 {
		t.Fatalf("model supplied a different project: %v", got)
	}
}

func TestMissingOrEmptyBoundProjectNeverFallsBackToGlobalHistory(t *testing.T) {
	path, db := projectScopeFixture(t)
	for _, project := range []string{"", "missing", "current\x00foreign"} {
		for _, query := range []string{"", `SELECT * FROM messages`} {
			if got, err := NewProject(path, project).Query(context.Background(), query, nil, 10000); err == nil {
				t.Fatalf("invalid bound project %q returned %#v", project, got)
			}
		}
	}
	reader := NewProject(path, "current")
	if _, err := db.Exec(`DELETE FROM projects WHERE id='current'`); err != nil {
		t.Fatal(err)
	}
	if got, err := reader.Query(context.Background(), `SELECT * FROM messages`, nil, 10000); err == nil {
		t.Fatalf("deleted project fell back to global history: %#v", got)
	}
}

func TestStandaloneHistoryReaderKeepsItsExistingGlobalScope(t *testing.T) {
	path, _ := projectScopeFixture(t)
	got := queryScopeIDs(t, New(path), `SELECT id FROM threads ORDER BY id`, nil)
	want := []string{"archived-thread", "current-thread", "foreign-thread", "standalone-thread"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("standalone scope changed: got=%v", got)
	}
}

func TestBoundProjectIdentityIsLiteralAndSourceViewsCannotBypassScope(t *testing.T) {
	path, db := projectScopeFixture(t)
	projectID := "quoted' OR 1=1 --"
	if _, err := db.Exec(`INSERT INTO projects VALUES (?,?)`, projectID, "Literal project"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO threads VALUES ('quoted-thread',?,0)`, projectID); err != nil {
		t.Fatal(err)
	}
	if got := queryScopeIDs(t, NewProject(path, projectID), `SELECT id FROM threads`, nil); !reflect.DeepEqual(got, []string{"quoted-thread"}) {
		t.Fatalf("bound ID became SQL syntax: %v", got)
	}
	if _, err := db.Exec(`CREATE VIEW foreign_originals AS SELECT * FROM messages WHERE thread_id='foreign-thread'`); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`SELECT * FROM foreign_originals`, `SELECT * FROM main.messages`, `SELECT * FROM temp.messages`, `SELECT * FROM pending_schedule_answers`, `SELECT * FROM pragma_database_list`} {
		if got, err := NewProject(path, "current").Query(context.Background(), query, nil, 10000); err == nil {
			t.Fatalf("project scope bypass accepted: %s -> %#v", query, got)
		}
	}
	got := queryScopeIDs(t, NewProject(path, "current"), `SELECT id FROM threads WHERE 1=1 OR project_id='foreign' ORDER BY id`, nil)
	if !reflect.DeepEqual(got, []string{"archived-thread", "current-thread"}) {
		t.Fatalf("outer OR bypassed scoped view: %v", got)
	}
}

func TestProjectScopeMembershipAndExecutionShareOneSnapshot(t *testing.T) {
	path, writer := projectScopeFixture(t)
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	p, tables, err := prepareSnapshot(context.Background(), tx)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyProjectScope(context.Background(), tx, tables, "current"); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(`UPDATE threads SET project_id='foreign' WHERE id='current-thread'`); err != nil {
		t.Fatal(err)
	}
	query := `SELECT id FROM threads ORDER BY id`
	if err := p.admit(context.Background(), tx, query, nil); err != nil {
		t.Fatal(err)
	}
	got, err := collectQuery(context.Background(), tx, query, nil, 10000)
	if err != nil || len(got.Rows) != 2 || got.Rows[1][0] != "current-thread" {
		t.Fatalf("membership changed within one snapshot: %#v %v", got, err)
	}
	if next := queryScopeIDs(t, NewProject(path, "current"), query, nil); !reflect.DeepEqual(next, []string{"archived-thread"}) {
		t.Fatalf("new snapshot reused stale membership: %v", next)
	}
}

func TestProjectScopedReadsRetainRawBytesAndCancelWithoutChangingSource(t *testing.T) {
	path, db := projectScopeFixture(t)
	original := "before\x00after"
	if _, err := db.Exec(`UPDATE messages SET content=? WHERE id='current-thread'`, original); err != nil {
		t.Fatal(err)
	}
	reader := NewProject(path, "current")
	got, err := reader.Query(context.Background(), `SELECT content,CAST(content AS BLOB),NULL,x'','' FROM messages WHERE id='current-thread'`, nil, 10000)
	if err != nil || len(got.Rows) != 1 || got.Rows[0][0] != original || string(got.Rows[0][1].([]byte)) != original || got.Rows[0][2] != nil || len(got.Rows[0][3].([]byte)) != 0 || got.Rows[0][4] != "" {
		t.Fatalf("scoped source bytes changed: %#v %v", got, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = reader.Query(ctx, `WITH RECURSIVE scan(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM scan) SELECT sum(n) FROM scan JOIN messages ON messages.id='current-thread'`, nil, 10000)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("scoped query lost cancellation: %v", err)
	}
	var rawCount, views int
	if err := db.QueryRow(`SELECT count(*) FROM messages`).Scan(&rawCount); err != nil || rawCount != 4 {
		t.Fatalf("scoping changed raw source count=%d %v", rawCount, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE type='view'`).Scan(&views); err != nil || views != 0 {
		t.Fatalf("scoping migrated the source schema: views=%d %v", views, err)
	}
	if got := queryScopeIDs(t, reader, `SELECT id FROM messages ORDER BY id`, nil); len(got) != 2 {
		t.Fatalf("cancellation poisoned the next scoped query: %v", got)
	}
}

func TestParallelProjectQueriesKeepConnectionLocalViews(t *testing.T) {
	path, _ := projectScopeFixture(t)
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			project := "current"
			want := 2
			if i%2 != 0 {
				project = "foreign"
				want = 1
			}
			got, err := NewProject(path, project).Query(context.Background(), `SELECT id FROM threads ORDER BY id`, nil, 10000)
			if err != nil {
				failures <- err
				return
			}
			if len(got.Rows) != want {
				failures <- errors.New("connection-local project view leaked between readers")
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}

func TestProjectScopeRequiresCompleteMetadataAndProtectiveViews(t *testing.T) {
	path, source := projectScopeFixture(t)
	for _, prefix := range []string{`SELECT EXISTS`, `DROP VIEW`, `CREATE TEMP VIEW`} {
		t.Run(prefix, func(t *testing.T) {
			failure := errors.New("project scope metadata/write-to-temp failed")
			db := sql.OpenDB(&historyFaultConnector{base: source.Driver(), path: path, prefix: prefix, failure: failure})
			defer func() { _ = db.Close() }()
			tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			err = applyProjectScope(context.Background(), tx, []Table{{Name: "messages"}}, "current")
			if !errors.Is(err, failure) {
				t.Fatalf("scope continued after required failure: %v", err)
			}
		})
	}
	tx, err := source.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := applyProjectScope(context.Background(), tx, []Table{{Name: "unscoped"}}, "current"); err == nil || !strings.Contains(err.Error(), "ownership rule") {
		t.Fatalf("unknown ownership rule was accepted: %v", err)
	}
	missing, _ := historyFixture(t)
	if _, err := NewProject(missing, "current").Query(context.Background(), `SELECT * FROM messages`, nil, 10000); err == nil {
		t.Fatal("missing project catalog was treated as global")
	}
}
