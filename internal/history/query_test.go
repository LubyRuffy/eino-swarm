package history

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

func historyFixture(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.sqlite")
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, statement := range []string{
		`CREATE TABLE threads(id TEXT PRIMARY KEY, project_id TEXT, title TEXT)`,
		`CREATE TABLE messages(id INTEGER PRIMARY KEY, thread_id TEXT, seq INTEGER, role TEXT, content TEXT)`,
		`CREATE INDEX message_thread ON messages(thread_id, seq)`,
		`CREATE TABLE events(id INTEGER PRIMARY KEY, thread_id TEXT, seq INTEGER, kind TEXT, text TEXT)`,
		`CREATE TABLE pending_schedule_answers(payload TEXT)`,
		`CREATE TABLE composer_drafts(text TEXT)`,
		`CREATE TABLE remote_devices(token TEXT)`,
		`INSERT INTO threads VALUES ('first', 'project', 'Earlier research'), ('second', 'other', 'Separate topic')`,
		`INSERT INTO messages VALUES (1, 'first', 1, 'user', 'Preserve the original requirement'), (2, 'first', 2, 'assistant', 'A proposal requires verification')`,
		`INSERT INTO events VALUES (1, 'first', 1, 'tool_result', 'original worker evidence'), (2, 'first', 2, 'engine_restart_checkpoint', 'private checkpoint bytes')`,
		`INSERT INTO pending_schedule_answers VALUES ('unpublished candidate')`,
		`INSERT INTO composer_drafts VALUES ('unsent draft')`,
		`INSERT INTO remote_devices VALUES ('private token')`,
		`CREATE VIEW leaked_checkpoint AS SELECT text FROM events WHERE kind='engine_restart_checkpoint'`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	return path, db
}

func TestSchemaColumnNamesAndRowsShareOneEncodedResultBudget(t *testing.T) {
	path, _ := historyFixture(t)
	for _, statement := range []string{"", `SELECT content AS "` + strings.Repeat("long_column", 100) + `" FROM messages`} {
		if _, err := New(path).Query(context.Background(), statement, nil, 300); err == nil {
			t.Fatalf("metadata bypassed budget: %q", statement)
		}
	}
	got, err := New(path).Query(context.Background(), `SELECT content FROM messages ORDER BY seq`, nil, 290)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(got)
	if err != nil || len(body) > 290 {
		t.Fatalf("result exceeds budget: bytes=%d, %v", len(body), err)
	}
	if _, err := fitResult(Result{Rows: [][]any{{math.Inf(1)}}}, 10000); err == nil {
		t.Fatal("nonfinite number accepted")
	}
}

func TestInternalCollectorCannotBeCalledByAgentOrRetainFinishedQueries(t *testing.T) {
	path, db := historyFixture(t)
	for _, statement := range []string{`SELECT zwai_collect_history_row()`, `SELECT zwai_collect_history_row('invalid')`, `SELECT zwai_collect_history_row(0)`} {
		if err := db.QueryRow(statement).Scan(new(any)); err == nil {
			t.Errorf("inactive collector accepted %q", statement)
		}
	}
	if _, err := New(path).Query(context.Background(), `SELECT zwai_collect_history_row(0,content) FROM messages`, nil, 1000); err == nil {
		t.Fatal("agent invoked an application function")
	}
	if _, err := New(path).Query(context.Background(), `SELECT content,content FROM messages`, nil, 1000); err != nil {
		t.Fatalf("duplicate columns lost: %v", err)
	}
	var retained int
	collectors.Range(func(_, _ any) bool { retained++; return true })
	if retained != 0 {
		t.Fatalf("finished query retained %d collectors", retained)
	}
}

func TestParallelHistoryQueriesKeepIndependentResults(t *testing.T) {
	path, _ := historyFixture(t)
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := New(path).Query(context.Background(), `SELECT ?, content FROM messages WHERE seq=1`, []any{i}, 1000)
			if err != nil {
				failures <- err
				return
			}
			if len(got.Rows) != 1 || got.Rows[0][0] != int64(i) {
				failures <- fmt.Errorf("query %d got %#v", i, got)
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}

func TestReadonlySourceFailureLeavesTheOriginalFileUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-database")
	original := []byte("original bytes are not SQLite")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(path).Query(context.Background(), `SELECT * FROM messages`, nil, 1000); err == nil {
		t.Fatal("invalid source accepted")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(original) {
		t.Fatalf("source changed: %q %v", got, err)
	}
}

func TestSnapshotSetupDoesNotIgnoreFailedProtectiveViews(t *testing.T) {
	for _, table := range []string{"events(kind TEXT)", "pending_schedule_answers(payload TEXT)"} {
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("CREATE TABLE " + table + "; PRAGMA query_only=ON"); err != nil {
			db.Close()
			t.Fatal(err)
		}
		tx, err := db.Begin()
		if err != nil {
			db.Close()
			t.Fatal(err)
		}
		if _, _, err := prepareSnapshot(context.Background(), tx); err == nil {
			t.Errorf("failed protective view accepted for %s", table)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}

func TestSchemaDescribesOnlyRetainedPublicTables(t *testing.T) {
	path, _ := historyFixture(t)
	got, err := New(path).Query(context.Background(), " -- schema\n /* columns */ ", nil, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Schema) != 3 || got.Schema[0].Name != "events" || got.Schema[1].Name != "messages" || len(got.Schema[1].Columns) != 5 {
		t.Fatalf("schema = %#v", got.Schema)
	}
	for _, table := range got.Schema {
		for _, col := range table.Columns {
			if col.Type == "" {
				t.Fatalf("missing SQL type: %#v", col)
			}
		}
	}
	if _, err := New(path).Query(context.Background(), "", []any{1}, 100); err == nil {
		t.Fatal("schema accepted parameters")
	}
}

func TestQueriesSupportCTEsAggregatesWindowsAndQuotedLiterals(t *testing.T) {
	path, _ := historyFixture(t)
	for _, query := range []string{
		`WITH chosen AS (SELECT * FROM messages) SELECT role, count(*), group_concat(content), lower(role) FROM chosen GROUP BY role ORDER BY role`,
		`SELECT seq, row_number() OVER (ORDER BY seq), sum(seq) OVER (ORDER BY seq) FROM messages ORDER BY seq`,
		`SELECT m.content FROM messages m WHERE m.thread_id='first' OR m.thread_id='second' ORDER BY m.seq`,
		`SELECT [content], "content", ` + "`content`" + ` FROM [messages] LIMIT 1`,
		`SELECT 'main','temp','semi; -- /*','escaped '' string', "quoted""identifier" FROM (SELECT 'value' AS "quoted""identifier"); -- trailing comment`,
		`VALUES(1),(2)`,
		`SELECT DISTINCT role FROM messages`,
	} {
		got, err := New(path).Query(context.Background(), query, nil, 10000)
		if err != nil || len(got.Rows) == 0 {
			t.Fatalf("query %q = %#v, %v", query, got, err)
		}
	}
}

func TestInvalidAndUnterminatedQueriesDoNotChangeTheSource(t *testing.T) {
	path, _ := historyFixture(t)
	for _, query := range []string{`SELECT 1 /*`, `SELECT 'open`, `SELECT "open`, "SELECT `open", `SELECT [open`, `SELECT 1;;`, `SELECT missing_column FROM messages`, `SELECT abs(-9223372036854775808)`, `SELECT 9e999`, `SELECT * FROM temp.events`} {
		if got, err := New(path).Query(context.Background(), query, nil, 10000); err == nil {
			t.Errorf("invalid query %q = %#v", query, got)
		}
	}
	for _, path := range []string{"", ":memory:", filepath.Join(t.TempDir(), "absent.sqlite")} {
		if _, err := New(path).Query(context.Background(), "SELECT 1", nil, 100); err == nil {
			t.Fatalf("opened unavailable %q", path)
		}
	}
	if _, err := New(path).Query(context.Background(), "SELECT 1", nil, 0); err == nil {
		t.Fatal("accepted empty output budget")
	}
	missing := filepath.Join(t.TempDir(), "readonly-does-not-create.sqlite")
	if _, err := New(missing).Query(context.Background(), "SELECT 1", nil, 100); err == nil {
		t.Fatal("a missing source was accepted")
	}
	if _, err := os.Stat(missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing source was created: %v", err)
	}
}

func TestCancelledQueryKeepsItsCauseAndDoesNotBlockTheWriter(t *testing.T) {
	path, db := historyFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := New(path).Query(ctx, `WITH RECURSIVE scan(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM scan) SELECT sum(n) FROM scan`, nil, 1000)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("query did not preserve cancellation: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO messages VALUES (3,'first',3,'user','writer remains usable')`); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("human stopped history query")
	cancelled, stop := context.WithCancelCause(context.Background())
	stop(cause)
	if _, err := New(path).Query(cancelled, "SELECT 1", nil, 100); !errors.Is(err, cause) {
		t.Fatalf("lost human cancellation: %v", err)
	}
}

func TestSnapshotAdmissionAndExecutionCannotSeeDifferentSchemas(t *testing.T) {
	path, writer := historyFixture(t)
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	p, _, err := prepareSnapshot(context.Background(), tx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(`ALTER TABLE messages RENAME TO previous_messages; CREATE TABLE messages(content TEXT); INSERT INTO messages VALUES ('new schema bytes')`); err != nil {
		t.Fatal(err)
	}
	query := `SELECT content FROM messages ORDER BY seq`
	if err := p.admit(context.Background(), tx, query, nil); err != nil {
		t.Fatal(err)
	}
	got, err := collectQuery(context.Background(), tx, query, nil, 10000)
	if err != nil || len(got.Rows) != 2 || got.Rows[0][0] != "Preserve the original requirement" {
		t.Fatalf("snapshot changed: %#v, %v", got, err)
	}
}

func TestDefenceRejectsActualWritableAndPrivatePlans(t *testing.T) {
	path, _ := historyFixture(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	p, _, err := prepareSnapshot(context.Background(), tx)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`DELETE FROM messages`, `BEGIN IMMEDIATE`, `SELECT * FROM main.pending_schedule_answers`, `SELECT * FROM pragma_database_list`} {
		if err := p.admit(context.Background(), tx, query, nil); err == nil {
			t.Errorf("unsafe plan admitted: %s", query)
		}
	}
	p.functions = map[string]bool{}
	if err := p.admit(context.Background(), tx, `SELECT lower('private')`, nil); err == nil {
		t.Fatal("unapproved function executed")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepareSnapshot(context.Background(), tx); err == nil {
		t.Fatal("lost ended snapshot failure")
	}
	if _, err := describeTable(context.Background(), tx, "messages"); err == nil {
		t.Fatal("described ended snapshot")
	}
	if err := p.admit(context.Background(), tx, "SELECT 1", nil); err == nil {
		t.Fatal("admitted ended snapshot")
	}
	if _, err := collectQuery(context.Background(), tx, "SELECT 1", nil, 1000); err == nil {
		t.Fatal("collected an ended snapshot")
	}
}

func TestNullEventKindAndSourceTriggersDoNotHideOrChangeRawHistory(t *testing.T) {
	path, db := historyFixture(t)
	if _, err := db.Exec(`INSERT INTO events VALUES (3,'first',3,NULL,'legacy raw event'); CREATE TRIGGER source_unchanged AFTER INSERT ON messages BEGIN INSERT INTO composer_drafts VALUES ('trigger ran'); END;`); err != nil {
		t.Fatal(err)
	}
	got, err := New(path).Query(context.Background(), `SELECT text FROM events ORDER BY seq`, nil, 10000)
	if err != nil || len(got.Rows) != 2 || got.Rows[1][0] != "legacy raw event" {
		t.Fatalf("legacy events = %#v, %v", got, err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM composer_drafts`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("read fired a source trigger: count=%d, %v", n, err)
	}
}

func TestAgentCanReadOriginalHistoryWithJoinsAndParameters(t *testing.T) {
	path, _ := historyFixture(t)
	got, err := New(path).Query(context.Background(), `SELECT m.seq, m.content FROM messages m JOIN threads t ON t.id=m.thread_id WHERE t.project_id=? ORDER BY m.seq;`, []any{"project"}, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Rows) != 2 || got.Rows[0][1] != "Preserve the original requirement" || got.Truncated {
		t.Fatalf("unexpected original history: %#v", got)
	}
}

func TestOriginalTextWithEmbeddedNULAndBlobBytesStayUnchanged(t *testing.T) {
	path, db := historyFixture(t)
	original := "before\x00after"
	if _, err := db.Exec(`INSERT INTO messages VALUES (3,'first',3,'user',?)`, original); err != nil {
		t.Fatal(err)
	}
	got, err := New(path).Query(context.Background(), `SELECT content,CAST(content AS BLOB) FROM messages WHERE seq=3`, nil, 10000)
	if err != nil || len(got.Rows) != 1 || got.Rows[0][0] != original {
		t.Fatalf("original text changed: %#v %v", got, err)
	}
	if blob, ok := got.Rows[0][1].([]byte); !ok || string(blob) != original {
		t.Fatalf("blob bytes changed: %#v", got.Rows[0][1])
	}
}

func TestEmptyTextEmptyBlobAndNullRemainDistinctOriginalValues(t *testing.T) {
	path, _ := historyFixture(t)
	got, err := New(path).Query(context.Background(), `SELECT '' AS empty_text, x'' AS empty_blob, NULL AS absent, 'before'||char(0)||'after' AS original_text`, nil, 10000)
	if err != nil || len(got.Rows) != 1 {
		t.Fatalf("empty original values=%#v %v", got, err)
	}
	row := got.Rows[0]
	if row[0] != "" || row[2] != nil || row[3] != "before\x00after" {
		t.Fatalf("empty values changed type or text: %#v", row)
	}
	if blob, ok := row[1].([]byte); !ok || len(blob) != 0 {
		t.Fatalf("empty BLOB changed: %#v", row[1])
	}
}

func TestHistoryExpressionsAreEvaluatedOnceBeforeTypeTransport(t *testing.T) {
	path, _ := historyFixture(t)
	got, err := New(path).Query(context.Background(), `WITH RECURSIVE scan(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM scan WHERE n<100) SELECT CASE random()%2 WHEN 0 THEN 'text' ELSE 42 END AS value FROM scan`, nil, 10000)
	if err != nil || len(got.Rows) != 100 {
		t.Fatalf("variable SQL types changed during transport: rows=%d %v", len(got.Rows), err)
	}
	for _, row := range got.Rows {
		if row[0] != "text" && row[0] != int64(42) {
			t.Fatalf("unexpected source expression: %#v", row)
		}
	}
	c := collector{result: Result{Columns: []string{"source"}}}
	if _, err := c.append(nil); err == nil {
		t.Fatal("collector accepted incomplete source columns")
	}
	if _, err := c.append([]driver.Value{"text", int64(42)}); err == nil {
		t.Fatal("collector accepted a malformed text transport")
	}
}

func TestPrivateRecoveryAndUnsentTextNeverBecomeHistory(t *testing.T) {
	path, _ := historyFixture(t)
	r := New(path)
	got, err := r.Query(context.Background(), `SELECT kind,text FROM events ORDER BY seq`, nil, 10000)
	if err != nil || len(got.Rows) != 1 || got.Rows[0][1] != "original worker evidence" {
		t.Fatalf("events = %#v, %v", got, err)
	}
	for _, query := range []string{
		`SELECT * FROM pending_schedule_answers`,
		`SELECT * FROM composer_drafts`,
		`SELECT * FROM remote_devices`,
		`SELECT * FROM leaked_checkpoint`,
		`SELECT text FROM main.events`,
		`SELECT text FROM "MAIN" /* gap */ . [events]`,
		"SELECT text FROM `main`.`events`",
		`SELECT text FROM 'main'.'events'`,
		`SELECT * FROM pragma_table_info('pending_schedule_answers')`,
		`SELECT * FROM sqlite_master`,
		`SELECT text FROM events UNION ALL SELECT payload FROM pending_schedule_answers`,
		`WITH secret AS (SELECT payload FROM pending_schedule_answers) SELECT * FROM secret`,
	} {
		if got, err := r.Query(context.Background(), query, nil, 10000); err == nil {
			t.Errorf("private query accepted %q: %#v", query, got)
		}
	}
}

func TestHistoryQueryCannotWriteOrAttachThroughPrepareTail(t *testing.T) {
	path, db := historyFixture(t)
	r := New(path)
	for _, query := range []string{
		`DELETE FROM messages`,
		`UPDATE messages SET content='changed' RETURNING content`,
		`PRAGMA query_only=OFF`,
		`SELECT 1); DELETE FROM messages; SELECT (1`,
		`SELECT 1; ATTACH ':memory:' AS extra`,
		`SELECT 1; PRAGMA query_only=OFF`,
		`SELECT 1` + "\x00" + `; DELETE FROM messages`,
		`SELECT load_extension('untrusted')`,
		`SELECT * FROM pragma_database_list`,
	} {
		if got, err := r.Query(context.Background(), query, nil, 10000); err == nil {
			t.Errorf("mutation accepted %q: %#v", query, got)
		}
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM messages`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("history changed: count=%d err=%v", n, err)
	}
}

func TestWholeRowsAreOmittedInsteadOfTruncatingSourceText(t *testing.T) {
	path, db := historyFixture(t)
	if _, err := db.Exec(`INSERT INTO messages VALUES (3,'first',3,'user',?)`, strings.Repeat("raw source", 200)); err != nil {
		t.Fatal(err)
	}
	got, err := New(path).Query(context.Background(), `SELECT content FROM messages ORDER BY seq DESC`, nil, 300)
	if err != nil || !got.Truncated || len(got.Rows) != 0 || !strings.Contains(got.Reason, "budget") {
		t.Fatalf("result = %#v, %v", got, err)
	}
}
