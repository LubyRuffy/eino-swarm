package backup

import (
	"context"
	"fmt"

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
	both := append(append([]any{}, ids...), ids...)
	steps := []struct {
		table, query string
		args         []any
	}{
		{"graph_subjects", `DELETE FROM ` + prefix + `graph_subjects WHERE ` + byWriter("thread_id"), both},
		{"graph_subject_keys", `DELETE FROM ` + prefix + `graph_subject_keys WHERE owner_id IN (` + marks + `) OR subject_id NOT IN (SELECT id FROM ` + prefix + `graph_subjects)`, ids},
		{"graph_links", `DELETE FROM ` + prefix + `graph_links WHERE ` + byWriter("thread_id") +
			` OR from_id NOT IN (SELECT id FROM ` + prefix + `graph_subjects) OR to_id NOT IN (SELECT id FROM ` + prefix + `graph_subjects)`, both},
		{"graph_todos", `DELETE FROM ` + prefix + `graph_todos WHERE ` + byWriter("thread_id"), both},
		{"graph_todos", `UPDATE ` + prefix + `graph_todos SET about = '' WHERE about <> '' AND about NOT IN (SELECT id FROM ` + prefix + `graph_subjects)`, nil},
		{"graph_revisions", `DELETE FROM ` + prefix + `graph_revisions WHERE ` + byWriter("thread_id"), both},
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
