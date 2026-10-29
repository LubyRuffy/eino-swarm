package history

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// applyProjectScope replaces only this connection's temporary views. Its
// membership reads share the existing source snapshot, and model SQL cannot
// select main.* to bypass them. A missing bound project never means global.
func applyProjectScope(ctx context.Context, tx *sql.Tx, tables []Table, projectID string) error {
	if projectID == "" || strings.ContainsRune(projectID, 0) {
		return fmt.Errorf("history requires a nonempty bound project identity")
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM main.projects WHERE id=?)`, projectID).Scan(&exists); err != nil {
		return fmt.Errorf("read bound history project: %w", err)
	}
	if !exists {
		return fmt.Errorf("bound history project does not exist")
	}
	predicates := projectScopePredicates(projectID)
	for _, table := range tables {
		predicate, ok := predicates[table.Name]
		if !ok {
			return fmt.Errorf("table %q has no project history ownership rule", table.Name)
		}
		// prepareSnapshot already filters private events. Replace that view,
		// retaining its exclusion alongside the stronger project predicate.
		if _, err := tx.ExecContext(ctx, `DROP VIEW IF EXISTS temp.`+quote(table.Name)); err != nil {
			return fmt.Errorf("replace project history view: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `CREATE TEMP VIEW `+quote(table.Name)+` AS SELECT * FROM main.`+quote(table.Name)+` WHERE `+predicate); err != nil {
			return fmt.Errorf("create project history view: %w", err)
		}
	}
	return nil
}

func projectScopePredicates(projectID string) map[string]string {
	// This value comes from the engine's bound conversation identity, never
	// from model arguments. Preserve it exactly, including literal quotes.
	literal := "'" + strings.ReplaceAll(projectID, "'", "''") + "'"
	threads := `(SELECT id FROM main.threads WHERE project_id=` + literal + `)`
	member := `thread_id IN ` + threads
	noThread := `(thread_id IS NULL OR thread_id='')`
	// Scheduling ownership follows the configured destination. Origin is a
	// creation reference and cannot make a foreign destination visible.
	schedules := `(` + member + ` OR (` + noThread + ` AND project_id=` + literal + `))`
	out := map[string]string{
		"projects":  `id=` + literal,
		"threads":   `project_id=` + literal,
		"events":    `kind IS NOT 'engine_restart_checkpoint' AND ` + member,
		"schedules": schedules,
		// A result with its own thread follows that thread even if the job
		// was later retargeted. Only an unattached run inherits its job scope.
		"schedule_runs": `(` + member + ` OR (` + noThread + ` AND schedule_id IN (SELECT id FROM main.schedules WHERE ` + schedules + `)))`,
	}
	for _, table := range []string{"messages", "turns", "attachments", "llm_calls"} {
		out[table] = member
	}
	return out
}
