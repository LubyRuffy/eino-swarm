package backup

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
)

// omitGraphRows removes what omitted conversations wrote into a graph. A graph
// owned by an omitted conversation goes entirely; a project's graph loses only
// the rows and batches those conversations wrote, and whatever pointed at a
// removed subject.
func omitGraphRows(ctx context.Context, q queryer, prefix string, ids []any) error {
	if len(ids) == 0 {
		return nil
	}
	schema := prefix[:len(prefix)-1]
	marks := placeholders(len(ids))
	byWriter := func(thread string) string {
		return `owner_id IN (` + marks + `) OR ` + thread + ` IN (` + marks + `)`
	}
	// Receipts identify every row a skipped conversation wrote. The final
	// writer alone cannot establish provenance for a merged row.
	touched := func(resultKey string) string {
		return `id IN (SELECT json_extract(change.value, '$.id') FROM ` + prefix + `graph_revisions AS batch
			JOIN json_each(batch.payload, '$.` + resultKey + `') AS change
			WHERE batch.thread_id IN (` + marks + `))`
	}
	both := append(append([]any{}, ids...), ids...)
	thrice := append(append([]any{}, both...), ids...)
	allWriters := append(append([]any{}, thrice...), ids...)
	steps := []struct {
		table, query string
		args         []any
	}{
		{"graph_subjects", `DELETE FROM ` + prefix + `graph_subjects WHERE ` + byWriter("thread_id") +
			` OR updated_thread_id IN (` + marks + `) OR ` + touched("subjects"), allWriters},
		{"graph_subject_keys", `DELETE FROM ` + prefix + `graph_subject_keys WHERE owner_id IN (` + marks + `) OR subject_id NOT IN (SELECT id FROM ` + prefix + `graph_subjects)`, ids},
		{"graph_links", `DELETE FROM ` + prefix + `graph_links WHERE ` + byWriter("thread_id") + ` OR ` + touched("links") +
			` OR from_id NOT IN (SELECT id FROM ` + prefix + `graph_subjects) OR to_id NOT IN (SELECT id FROM ` + prefix + `graph_subjects)`, thrice},
		{"graph_todos", `DELETE FROM ` + prefix + `graph_todos WHERE ` + byWriter("thread_id") +
			` OR updated_thread_id IN (` + marks + `) OR ` + touched("todos"), allWriters},
		{"graph_todos", `UPDATE ` + prefix + `graph_todos SET about = '' WHERE about <> '' AND about NOT IN (SELECT id FROM ` + prefix + `graph_subjects)`, nil},
		{"graph_subjects", `UPDATE ` + prefix + `graph_subjects SET found_by_todo = '' WHERE found_by_todo <> '' AND found_by_todo NOT IN (SELECT id FROM ` + prefix + `graph_todos)`, nil},
		{"graph_links", `UPDATE ` + prefix + `graph_links SET found_by_todo = '' WHERE found_by_todo <> '' AND found_by_todo NOT IN (SELECT id FROM ` + prefix + `graph_todos)`, nil},
		{"graph_revisions", `DELETE FROM ` + prefix + `graph_revisions WHERE ` + byWriter("thread_id") + `
			OR EXISTS (SELECT 1 FROM json_each(payload, '$.subjects') WHERE json_extract(value, '$.id') NOT IN (SELECT id FROM ` + prefix + `graph_subjects))
			OR EXISTS (SELECT 1 FROM json_each(payload, '$.links') WHERE json_extract(value, '$.id') NOT IN (SELECT id FROM ` + prefix + `graph_links))
			OR EXISTS (SELECT 1 FROM json_each(payload, '$.todos') WHERE json_extract(value, '$.id') NOT IN (SELECT id FROM ` + prefix + `graph_todos))`, both},
	}
	for _, step := range steps {
		if present, err := presentOrOptional(ctx, q, schema, step.table); err != nil {
			return err
		} else if !present {
			return nil
		}
		if _, err := q.ExecContext(ctx, step.query, step.args...); err != nil {
			return fmt.Errorf("backup: omit %s: %w", step.table, err)
		}
	}
	return nil
}

// stripGraphSecrets drops credential attributes from a snapshot that must not
// carry secrets. Which attributes those are comes from the kind catalog, so a
// new secret attribute is covered without touching this code.
func stripGraphSecrets(ctx context.Context, q queryer, schema string) error {
	if !identRe.MatchString(schema) {
		return invalidf("schema")
	}
	if present, err := presentOrOptional(ctx, q, schema, "graph_subjects"); err != nil || !present {
		return err
	}
	catalog, err := graph.Default()
	if err != nil {
		return err
	}
	for _, id := range catalog.IDs() {
		secrets := catalog.Kind(id).SecretAttrs()
		for _, name := range secrets {
			if !identRe.MatchString(name) {
				return invalidf("secret attribute name")
			}
			if _, err := q.ExecContext(ctx, `UPDATE `+schema+`.graph_subjects SET attrs = json_remove(attrs, '$.`+name+`') WHERE kind = ? AND json_valid(attrs)`, id); err != nil {
				return fmt.Errorf("backup: omit graph credentials: %w", err)
			}
		}
	}
	return nil
}

// scrubGraphToolPayloads redacts credential values copied into graph tool
// events and transcript rows. The subject table is separate: a full backup
// may still keep those values, and a backup that omits secrets strips them.
func scrubGraphToolPayloads(ctx context.Context, q queryer, schema string) error {
	if !identRe.MatchString(schema) {
		return invalidf("schema")
	}
	tables := []struct {
		name, fields, filter string
		cols                 []string
	}{
		{"events", "text", "work_record_payload = 1 OR kind = 'engine_restart_checkpoint'", []string{"text"}},
		{"messages", "CASE WHEN role = 'tool' THEN content END, NULLIF(tool_calls, '')", "work_record_payload = 1", []string{"content", "tool_calls"}},
	}
	for _, table := range tables {
		present, err := presentOrOptional(ctx, q, schema, table.name)
		if err != nil || !present {
			if err != nil {
				return err
			}
			continue
		}
		query := `SELECT rowid, ` + table.fields + ` FROM ` + schema + `.` + table.name + ` WHERE ` + table.filter
		rows, err := q.QueryContext(ctx, query)
		if err != nil {
			if strings.Contains(err.Error(), "no such column") {
				return nil
			}
			return fmt.Errorf("backup: read graph tool payload: %w", err)
		}
		type change struct {
			id   int64
			sets []string
			args []any
		}
		var changes []change
		scanErr := func() error {
			defer rows.Close()
			for rows.Next() {
				vals := make([]sql.NullString, len(table.cols))
				dest := make([]any, 0, len(table.cols)+1)
				var id int64
				dest = append(dest, &id)
				for i := range vals {
					dest = append(dest, &vals[i])
				}
				if err := rows.Scan(dest...); err != nil {
					return err
				}
				var item change
				item.id = id
				for i, col := range table.cols {
					if !vals[i].Valid {
						continue
					}
					next := graph.RedactToolText(vals[i].String)
					if next != vals[i].String {
						item.sets = append(item.sets, col+` = ?`)
						item.args = append(item.args, next)
					}
				}
				if len(item.sets) > 0 {
					changes = append(changes, item)
				}
			}
			return rows.Err()
		}()
		if scanErr != nil {
			return fmt.Errorf("backup: read graph tool payload: %w", scanErr)
		}
		for _, item := range changes {
			item.args = append(item.args, item.id)
			if _, err := q.ExecContext(ctx, `UPDATE `+schema+`.`+table.name+` SET `+strings.Join(item.sets, ", ")+` WHERE rowid = ?`, item.args...); err != nil {
				return fmt.Errorf("backup: redact graph tool payload: %w", err)
			}
		}
	}
	return nil
}

func placeholders(n int) string {
	out := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, '?')
	}
	return string(out)
}
