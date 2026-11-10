package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// copyOrder is parents before children. Thread and turn inserts are checked
// by the store's ownership triggers, and search_docs inserts maintain the
// keyword index. thread_fts and its shadow tables are rebuilt by those
// triggers, so they are never copied.
var copyOrder = []string{
	"projects",
	"threads",
	"turns",
	"events",
	"messages",
	"attachments",
	"followups",
	"llm_calls",
	"schedules",
	"schedule_runs",
	"nuwa_nodes",
	"nuwa_revisions",
	"nuwa_verify_slots",
	"thread_wake_decisions",
	"pending_schedule_answers",
	"composer_drafts",
	"composer_draft_blobs",
	"search_docs",
	"search_chunks",
	"remote_devices",
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// failAfter is zero outside tests. A test counts down the steps that can
// fail so each caller is shown returning that error instead of continuing.
var failAfter int

func failNext() error {
	if failAfter == 0 {
		return nil
	}
	failAfter--
	if failAfter == 0 {
		return errors.New("backup: injected failure")
	}
	return nil
}

var autoincTables = []string{
	"messages", "events", "llm_calls", "attachments",
	"nuwa_revisions", "nuwa_verify_slots", "search_docs", "search_chunks",
}

type column struct {
	name    string
	typ     string
	integer bool
}

func quoteIdent(name string) (string, error) {
	if !identRe.MatchString(name) {
		return "", invalidf("identifier %q", name)
	}
	return `"` + name + `"`, nil
}

// quoteSQLPath is false in production. The bound form works on the current
// driver; a test sets this so the quoted fallback is still executed.
var quoteSQLPath bool

func vacuumInto(ctx context.Context, conn *sql.Conn, dest string) error {
	if err := failNext(); err != nil {
		return err
	}
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		return err
	}
	if !quoteSQLPath {
		if _, err := conn.ExecContext(ctx, `VACUUM INTO ?`, dest); err == nil {
			return nil
		}
	}
	_ = os.Remove(dest)
	quoted := "'" + strings.ReplaceAll(dest, "'", "''") + "'"
	if _, err := conn.ExecContext(ctx, `VACUUM INTO `+quoted); err != nil {
		return fmt.Errorf("backup: snapshot database: %w", err)
	}
	return nil
}

func attach(ctx context.Context, conn *sql.Conn, schema, path string) error {
	if err := failNext(); err != nil {
		return err
	}
	if !identRe.MatchString(schema) {
		return invalidf("schema")
	}
	if !quoteSQLPath {
		if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS `+schema, path); err == nil {
			return nil
		}
	}
	quoted := "'" + strings.ReplaceAll(path, "'", "''") + "'"
	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE `+quoted+` AS `+schema); err != nil {
		return fmt.Errorf("backup: attach database: %w", err)
	}
	return nil
}

func detach(ctx context.Context, conn *sql.Conn, schema string) {
	if identRe.MatchString(schema) {
		_, _ = conn.ExecContext(ctx, `DETACH DATABASE `+schema)
	}
}

func withFKOff(ctx context.Context, conn *sql.Conn, fn func() error) error {
	var on int
	if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&on); err != nil {
		return err
	}
	if err := failNext(); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), fmt.Sprintf(`PRAGMA foreign_keys = %d`, on))
	}()
	return fn()
}

func knownTables(ctx context.Context, conn *sql.Conn, schema string) error {
	if err := failNext(); err != nil {
		return err
	}
	if !identRe.MatchString(schema) {
		return invalidf("schema")
	}
	known := make(map[string]struct{}, len(copyOrder))
	for _, name := range copyOrder {
		known[name] = struct{}{}
	}
	found := map[string]struct{}{}
	if err := readStrings(ctx, conn, `SELECT name FROM `+schema+`.sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`, nil, 1, func(v []string) error {
		name := v[0]
		if name == "thread_fts" || strings.HasPrefix(name, "thread_fts_") {
			return nil
		}
		if _, ok := known[name]; !ok {
			return invalidf("unexpected table %s", name)
		}
		found[name] = struct{}{}
		return nil
	}); err != nil {
		return err
	}
	for _, name := range copyOrder {
		if _, ok := found[name]; !ok {
			return invalidf("missing table %s", name)
		}
	}
	return nil
}

func tableColumns(ctx context.Context, q queryer, schema, table string) ([]column, error) {
	if !identRe.MatchString(schema) || !identRe.MatchString(table) {
		return nil, invalidf("identifier")
	}
	var cols []column
	if err := readStrings(ctx, q, `SELECT name, type FROM pragma_table_info('`+table+`', '`+schema+`') ORDER BY cid`, nil, 2, func(v []string) error {
		cols = append(cols, column{name: v[0], typ: v[1], integer: strings.Contains(strings.ToUpper(v[1]), "INT")})
		return nil
	}); err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, invalidf("missing table %s", table)
	}
	return cols, nil
}

func sameColumns(a, b []column) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].name != b[i].name || a[i].integer != b[i].integer {
			return false
		}
	}
	return true
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func copyTable(ctx context.Context, q queryer, src, dst, table, where string, args []any, offset int64) error {
	if err := failNext(); err != nil {
		return err
	}
	srcCols, err := tableColumns(ctx, q, src, table)
	if err != nil {
		return err
	}
	dstCols, err := tableColumns(ctx, q, dst, table)
	if err != nil {
		return err
	}
	if !sameColumns(srcCols, dstCols) {
		return invalidf("schema mismatch for %s", table)
	}
	names := make([]string, len(srcCols))
	exprs := make([]string, len(srcCols))
	for i, c := range srcCols {
		quoted, err := quoteIdent(c.name)
		if err != nil {
			return err
		}
		names[i] = quoted
		exprs[i] = quoted
		if offset > 0 && c.integer && (c.name == "id" || c.name == "event_id") {
			exprs[i] = fmt.Sprintf(`CASE WHEN %s IS NULL OR %s = 0 THEN %s ELSE %s + %d END`, quoted, quoted, quoted, quoted, offset)
		}
	}
	srcName, err := quoteIdent(src)
	if err != nil {
		return err
	}
	dstName, err := quoteIdent(dst)
	if err != nil {
		return err
	}
	tableName, err := quoteIdent(table)
	if err != nil {
		return err
	}
	query := fmt.Sprintf(`INSERT INTO %s.%s (%s) SELECT %s FROM %s.%s`, dstName, tableName, strings.Join(names, ", "), strings.Join(exprs, ", "), srcName, tableName)
	if where != "" {
		query += ` WHERE ` + where
	}
	if _, err := q.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("backup: copy %s: %w", table, err)
	}
	return nil
}

func projectFilter(table, projectID string) (string, []any, bool, error) {
	if !validID(projectID) {
		return "", nil, false, invalidf("project id")
	}
	threads := `SELECT id FROM main.threads WHERE project_id = ?`
	switch table {
	case "remote_devices":
		return "", nil, true, nil
	case "projects":
		return `id = ?`, []any{projectID}, false, nil
	case "threads":
		return `project_id = ?`, []any{projectID}, false, nil
	case "schedules":
		return `project_id = ? OR thread_id IN (` + threads + `)`, []any{projectID, projectID}, false, nil
	case "schedule_runs":
		return `thread_id IN (` + threads + `) OR schedule_id IN (SELECT id FROM main.schedules WHERE project_id = ? OR thread_id IN (` + threads + `))`,
			[]any{projectID, projectID, projectID}, false, nil
	case "nuwa_nodes", "nuwa_revisions", "nuwa_verify_slots":
		return `project_id = ?`, []any{projectID}, false, nil
	case "composer_drafts", "composer_draft_blobs":
		return `draft_key = ? OR draft_key IN (SELECT 'thread:' || id FROM main.threads WHERE project_id = ?)`,
			[]any{"new:" + projectID, projectID}, false, nil
	case "turns", "events", "messages", "attachments", "followups", "llm_calls", "thread_wake_decisions", "pending_schedule_answers", "search_docs", "search_chunks":
		return `thread_id IN (` + threads + `)`, []any{projectID}, false, nil
	default:
		return "", nil, false, invalidf("no project filter for %s", table)
	}
}

func deleteProjectRows(ctx context.Context, q queryer, projectID string) error {
	if err := failNext(); err != nil {
		return err
	}
	for i := len(copyOrder) - 1; i >= 0; i-- {
		table := copyOrder[i]
		where, args, skip, err := projectFilter(table, projectID)
		if err != nil {
			return err
		}
		if skip {
			continue
		}
		name, err := quoteIdent(table)
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM `+name+` WHERE `+where, args...); err != nil {
			return fmt.Errorf("backup: remove %s: %w", table, err)
		}
	}
	return nil
}

func checkpoint(ctx context.Context, conn *sql.Conn) error {
	if err := failNext(); err != nil {
		return err
	}
	_, err := conn.ExecContext(ctx, `PRAGMA wal_checkpoint(FULL)`)
	return err
}

func prepareExportDB(path string) error {
	if err := failNext(); err != nil {
		return err
	}
	st, err := store.Open(path)
	if err != nil {
		return err
	}
	sqlDB, err := st.DB().DB()
	if err != nil {
		_ = st.Close()
		return err
	}
	_, _ = sqlDB.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	_, _ = sqlDB.Exec(`PRAGMA journal_mode = DELETE`)
	if err := st.Close(); err != nil {
		return err
	}
	_ = os.Remove(path + "-wal")
	_ = os.Remove(path + "-shm")
	return nil
}

func exportProject(ctx context.Context, conn *sql.Conn, projectID, dest string) error {
	return exportProjectRows(ctx, conn, projectID, dest, false)
}

func exportProjectRows(ctx context.Context, conn *sql.Conn, projectID, dest string, allowEmpty bool) error {
	if err := failNext(); err != nil {
		return err
	}
	if err := prepareExportDB(dest); err != nil {
		return err
	}
	if err := attach(ctx, conn, "dst", dest); err != nil {
		return err
	}
	defer detach(context.Background(), conn, "dst")
	if err := knownTables(ctx, conn, "main"); err != nil {
		return err
	}
	if err := knownTables(ctx, conn, "dst"); err != nil {
		return err
	}
	for _, table := range copyOrder {
		where, args, skip, err := projectFilter(table, projectID)
		if err != nil {
			return err
		}
		if skip {
			continue
		}
		if err := copyTable(ctx, conn, "main", "dst", table, where, args, 0); err != nil {
			return err
		}
	}
	var n int
	var id string
	if err := failNext(); err != nil {
		return err
	}
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM dst.projects`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if allowEmpty {
			return nil
		}
		return store.ErrNotFound
	}
	if n != 1 {
		return invalidf("a project backup holds one project")
	}
	if err := failNext(); err != nil {
		return err
	}
	if err := conn.QueryRowContext(ctx, `SELECT id FROM dst.projects`).Scan(&id); err != nil {
		return err
	}
	if id != projectID {
		return invalidf("project id")
	}
	return bumpSequences(ctx, conn, "dst")
}

func replaceAll(ctx context.Context, conn *sql.Conn, incomingPath string) error {
	if err := failNext(); err != nil {
		return err
	}
	if err := prepareExportDB(incomingPath); err != nil {
		return err
	}
	if err := attach(ctx, conn, "incoming", incomingPath); err != nil {
		return err
	}
	defer detach(context.Background(), conn, "incoming")
	if err := knownTables(ctx, conn, "main"); err != nil {
		return err
	}
	if err := knownTables(ctx, conn, "incoming"); err != nil {
		return err
	}
	return withFKOff(ctx, conn, func() error {
		return withTx(ctx, conn, func(tx *sql.Tx) error {
			for i := len(copyOrder) - 1; i >= 0; i-- {
				name, err := quoteIdent(copyOrder[i])
				if err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `DELETE FROM `+name); err != nil {
					return fmt.Errorf("backup: clear %s: %w", copyOrder[i], err)
				}
			}
			for _, table := range copyOrder {
				if err := copyTable(ctx, tx, "incoming", "main", table, "", nil, 0); err != nil {
					return err
				}
			}
			return bumpSequences(ctx, tx, "main")
		})
	})
}

func applyProject(ctx context.Context, conn *sql.Conn, incomingPath, projectID string, replace, offsetIDs bool) error {
	if err := failNext(); err != nil {
		return err
	}
	if err := prepareExportDB(incomingPath); err != nil {
		return err
	}
	if err := attach(ctx, conn, "incoming", incomingPath); err != nil {
		return err
	}
	defer detach(context.Background(), conn, "incoming")
	var n int
	var id string
	if err := failNext(); err != nil {
		return err
	}
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM incoming.projects`).Scan(&n); err != nil {
		return err
	}
	if n != 1 {
		return invalidf("a project backup holds one project")
	}
	if err := failNext(); err != nil {
		return err
	}
	if err := conn.QueryRowContext(ctx, `SELECT id FROM incoming.projects`).Scan(&id); err != nil {
		return err
	}
	if id != projectID || !validID(projectID) {
		return invalidf("project id")
	}
	conflicts, err := findConflicts(ctx, conn, projectID, replace)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return &conflictError{Conflicts: conflicts}
	}
	return withFKOff(ctx, conn, func() error {
		return withTx(ctx, conn, func(tx *sql.Tx) error {
			offset := int64(0)
			if replace {
				if err := deleteProjectRows(ctx, tx, projectID); err != nil {
					return err
				}
			}
			if offsetIDs {
				offset, err = maxAutoinc(ctx, tx)
				if err != nil {
					return err
				}
			}
			for _, table := range copyOrder {
				if table == "remote_devices" {
					continue
				}
				if err := copyTable(ctx, tx, "incoming", "main", table, "", nil, offset); err != nil {
					return err
				}
			}
			return bumpSequences(ctx, tx, "main")
		})
	})
}

func withTx(ctx context.Context, conn *sql.Conn, fn func(*sql.Tx) error) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func maxAutoinc(ctx context.Context, q queryer) (int64, error) {
	var max int64
	for _, table := range autoincTables {
		name, err := quoteIdent(table)
		if err != nil {
			return 0, err
		}
		var n sql.NullInt64
		if err := failNext(); err != nil {
			return 0, err
		}
		if err := q.QueryRowContext(ctx, `SELECT MAX(id) FROM `+name).Scan(&n); err != nil {
			return 0, err
		}
		if n.Valid && n.Int64 > max {
			max = n.Int64
		}
	}
	return max, nil
}

func bumpSequences(ctx context.Context, q queryer, schema string) error {
	if !identRe.MatchString(schema) {
		return invalidf("schema")
	}
	var exists int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM `+schema+`.sqlite_master WHERE name = 'sqlite_sequence'`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return nil
	}
	for _, table := range autoincTables {
		name, err := quoteIdent(table)
		if err != nil {
			return err
		}
		var n sql.NullInt64
		if err := q.QueryRowContext(ctx, `SELECT MAX(id) FROM `+schema+`.`+name).Scan(&n); err != nil {
			return err
		}
		if !n.Valid {
			continue
		}
		seqTable := schema + ".sqlite_sequence"
		var current sql.NullInt64
		err = q.QueryRowContext(ctx, `SELECT seq FROM `+seqTable+` WHERE name = ?`, table).Scan(&current)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			_, err = q.ExecContext(ctx, `INSERT INTO `+seqTable+`(name, seq) VALUES(?, ?)`, table, n.Int64)
		case err != nil:
			return err
		case !current.Valid || current.Int64 < n.Int64:
			_, err = q.ExecContext(ctx, `UPDATE `+seqTable+` SET seq = ? WHERE name = ?`, n.Int64, table)
		default:
			err = nil
		}
		if err != nil {
			return fmt.Errorf("backup: sequence %s: %w", table, err)
		}
	}
	return nil
}

func countSchema(ctx context.Context, q queryer, schema string) (Counts, error) {
	if err := failNext(); err != nil {
		return Counts{}, err
	}
	if !identRe.MatchString(schema) {
		return Counts{}, invalidf("schema")
	}
	var c Counts
	scans := []struct {
		dest *int
		name string
	}{
		{&c.Projects, "projects"},
		{&c.Threads, "threads"},
		{&c.Messages, "messages"},
		{&c.Turns, "turns"},
		{&c.Schedules, "schedules"},
		{&c.NuwaNodes, "nuwa_nodes"},
	}
	for _, scan := range scans {
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM `+schema+`.`+scan.name).Scan(scan.dest); err != nil {
			return Counts{}, err
		}
	}
	return c, nil
}

func listThreads(ctx context.Context, q queryer, schema, projectID string) ([]string, error) {
	if err := failNext(); err != nil {
		return nil, err
	}
	if !identRe.MatchString(schema) || !validID(projectID) {
		return nil, invalidf("project id")
	}
	var ids []string
	err := readStrings(ctx, q, `SELECT id FROM `+schema+`.threads WHERE project_id = ?`, []any{projectID}, 1, func(v []string) error {
		if !validID(v[0]) {
			return invalidf("thread id")
		}
		ids = append(ids, v[0])
		return nil
	})
	return ids, err
}

func listExternal(ctx context.Context, q queryer, schema, projectID string) ([]ExternalWorkdir, error) {
	if err := failNext(); err != nil {
		return nil, err
	}
	if !identRe.MatchString(schema) {
		return nil, invalidf("schema")
	}
	query := `SELECT id, workdir FROM ` + schema + `.projects WHERE trim(workdir) <> ''`
	var args []any
	if projectID != "" {
		if !validID(projectID) {
			return nil, invalidf("project id")
		}
		query += ` AND id = ?`
		args = append(args, projectID)
	}
	var out []ExternalWorkdir
	err := readStrings(ctx, q, query, args, 2, func(v []string) error {
		out = append(out, ExternalWorkdir{ProjectID: v[0], Path: v[1]})
		return nil
	})
	return out, err
}

func readStrings(ctx context.Context, q queryer, query string, args []any, n int, row func([]string) error) error {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		vals := make([]string, n)
		dest := make([]any, n)
		for i := range vals {
			dest[i] = &vals[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return err
		}
		if err := row(vals); err != nil {
			return err
		}
	}
	return rows.Err()
}

func findConflicts(ctx context.Context, conn *sql.Conn, projectID string, replace bool) ([]Conflict, error) {
	var out []Conflict
	add := func(table, id, owner string, owned bool) {
		if id == "" {
			return
		}
		if replace && owned {
			return
		}
		if owner == "" {
			owner = "standalone"
		}
		out = append(out, Conflict{Table: table, ID: id, Owner: owner})
	}
	type query struct {
		sql  string
		n    int
		take func([]string)
	}
	queries := []query{
		{`SELECT i.id FROM incoming.projects AS i JOIN main.projects AS m ON m.id = i.id`, 1,
			func(v []string) { add("projects", v[0], v[0], replace && v[0] == projectID) }},
		{`SELECT i.id, COALESCE(m.project_id, '') FROM incoming.threads AS i JOIN main.threads AS m ON m.id = i.id`, 2,
			func(v []string) { add("threads", v[0], v[1], v[1] == projectID) }},
		{`SELECT i.id, COALESCE(th.project_id, '') FROM incoming.turns AS i JOIN main.turns AS m ON m.id = i.id LEFT JOIN main.threads AS th ON th.id = m.thread_id`, 2,
			func(v []string) { add("turns", v[0], v[1], v[1] == projectID) }},
		{`SELECT i.id, CASE WHEN COALESCE(m.project_id, '') <> '' THEN m.project_id ELSE COALESCE(th.project_id, '') END FROM incoming.schedules AS i JOIN main.schedules AS m ON m.id = i.id LEFT JOIN main.threads AS th ON th.id = m.thread_id`, 2,
			func(v []string) { add("schedules", v[0], v[1], v[1] == projectID) }},
		{`SELECT i.id, COALESCE(m.project_id, '') FROM incoming.nuwa_nodes AS i JOIN main.nuwa_nodes AS m ON m.id = i.id`, 2,
			func(v []string) { add("nuwa_nodes", v[0], v[1], v[1] == projectID) }},
		{`SELECT i.draft_key FROM incoming.composer_drafts AS i JOIN main.composer_drafts AS m ON m.draft_key = i.draft_key`, 1,
			func(v []string) {
				owner, owned := draftOwner(ctx, conn, v[0], projectID)
				add("composer_drafts", v[0], owner, owned)
			}},
	}
	for _, item := range queries {
		if err := readStrings(ctx, conn, item.sql, nil, item.n, func(v []string) error {
			item.take(v)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func draftOwner(ctx context.Context, conn *sql.Conn, key, projectID string) (string, bool) {
	switch {
	case key == "new:"+projectID:
		return projectID, true
	case strings.HasPrefix(key, "new:"):
		return strings.TrimPrefix(key, "new:"), false
	case strings.HasPrefix(key, "thread:"):
		var owner string
		err := conn.QueryRowContext(ctx, `SELECT COALESCE(project_id, '') FROM main.threads WHERE id = ?`, strings.TrimPrefix(key, "thread:")).Scan(&owner)
		if err != nil {
			return "", false
		}
		return owner, owner == projectID
	default:
		return "", false
	}
}

// restoreProjectSnapshot puts back the rows captured before a project restore.
// The snapshot may contain no project when the restore was adding one.
func restoreProjectSnapshot(ctx context.Context, conn *sql.Conn, snapPath, projectID string) error {
	if err := prepareExportDB(snapPath); err != nil {
		return err
	}
	if err := attach(ctx, conn, "incoming", snapPath); err != nil {
		return err
	}
	defer detach(context.Background(), conn, "incoming")
	return withFKOff(ctx, conn, func() error {
		return withTx(ctx, conn, func(tx *sql.Tx) error {
			if err := deleteProjectRows(ctx, tx, projectID); err != nil {
				return err
			}
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM incoming.projects`).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				return bumpSequences(ctx, tx, "main")
			}
			for _, table := range copyOrder {
				if table == "remote_devices" {
					continue
				}
				if err := copyTable(ctx, tx, "incoming", "main", table, "", nil, 0); err != nil {
					return err
				}
			}
			return bumpSequences(ctx, tx, "main")
		})
	})
}

func projectExists(ctx context.Context, conn *sql.Conn, projectID string) (bool, error) {
	var n int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM main.projects WHERE id = ?`, projectID).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}
