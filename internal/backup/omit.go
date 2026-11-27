package backup

import (
	"context"
	"fmt"
	"strings"
)

// omitThreads deletes the named conversations from an attached snapshot.
// The live database is a different schema, so this does not stop the
// conversation or remove its rows from the machine. The snapshot then
// matches a file tree that also left out that conversation's inputs,
// plans and workspace: a later full restore cannot bring it back.
func omitThreads(ctx context.Context, q queryer, schema string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if !identRe.MatchString(schema) {
		return invalidf("schema")
	}
	args := make([]any, len(ids))
	drafts := make([]any, len(ids))
	for i, id := range ids {
		if !validID(id) {
			return invalidf("thread id")
		}
		args[i] = id
		drafts[i] = "thread:" + id
	}
	marks := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	in := func(column string) string { return column + " IN (" + marks + ")" }
	prefix := schema + "."
	// The archive includes SQLite pages, so removing rows must also scrub
	// omitted payloads from released cells in the attached snapshot.
	if _, err := q.ExecContext(ctx, "PRAGMA "+prefix+"secure_delete = ON"); err != nil {
		return fmt.Errorf("backup: enable secure deletion for omitted records: %w", err)
	}
	// Project-scoped receipts may retain summaries of the omitted sources.
	// Compute all affected owners before deleting either receipts or queues.
	if err := omitReviewReceipts(ctx, q, prefix, args); err != nil {
		return err
	}
	runArgs := make([]any, 0, len(args)*2)
	runArgs = append(runArgs, args...)
	runArgs = append(runArgs, args...)
	if err := omitGraphRows(ctx, q, prefix, args); err != nil {
		return err
	}
	steps := []struct {
		table string
		query string
		args  []any
	}{
		{"nuwa_verify_slots", `DELETE FROM ` + prefix + `nuwa_verify_slots WHERE ` + in("thread_id"), args},
		{"nuwa_revisions", `DELETE FROM ` + prefix + `nuwa_revisions WHERE project_id = '' AND ` + in("thread_id"), args},
		{"nuwa_nodes", `DELETE FROM ` + prefix + `nuwa_nodes WHERE scope = 'thread' AND ` + in("thread_id"), args},
		{"schedule_runs", `DELETE FROM ` + prefix + `schedule_runs WHERE ` + in("thread_id") +
			` OR schedule_id IN (SELECT id FROM ` + prefix + `schedules WHERE ` + in("thread_id") + `)`, runArgs},
		{"composer_draft_blobs", `DELETE FROM ` + prefix + `composer_draft_blobs WHERE ` + in("draft_key"), drafts},
		{"composer_drafts", `DELETE FROM ` + prefix + `composer_drafts WHERE ` + in("draft_key"), drafts},
		{"search_chunks", `DELETE FROM ` + prefix + `search_chunks WHERE ` + in("thread_id"), args},
		{"task_panel_change_rows", `DELETE FROM ` + prefix + `task_panel_change_rows WHERE ` + in("thread_id"), args},
		{"task_panel_mutations", `DELETE FROM ` + prefix + `task_panel_mutations WHERE ` + in("thread_id"), args},
		{"task_panel_rows", `DELETE FROM ` + prefix + `task_panel_rows WHERE ` + in("thread_id"), args},
		{"search_docs", `DELETE FROM ` + prefix + `search_docs WHERE ` + in("thread_id"), args},
		{"pending_schedule_answers", `DELETE FROM ` + prefix + `pending_schedule_answers WHERE ` + in("thread_id"), args},
		{"thread_wake_decisions", `DELETE FROM ` + prefix + `thread_wake_decisions WHERE ` + in("thread_id"), args},
		{"llm_calls", `DELETE FROM ` + prefix + `llm_calls WHERE ` + in("thread_id"), args},
		{"followups", `DELETE FROM ` + prefix + `followups WHERE ` + in("thread_id"), args},
		{"attachments", `DELETE FROM ` + prefix + `attachments WHERE ` + in("thread_id"), args},
		{"messages", `DELETE FROM ` + prefix + `messages WHERE ` + in("thread_id"), args},
		{"events", `DELETE FROM ` + prefix + `events WHERE ` + in("thread_id"), args},
		{"turns", `DELETE FROM ` + prefix + `turns WHERE ` + in("thread_id"), args},
		{"schedules", `DELETE FROM ` + prefix + `schedules WHERE ` + in("thread_id"), args},
		{"threads", `DELETE FROM ` + prefix + `threads WHERE ` + in("id"), args},
	}
	for _, step := range steps {
		schemaName := strings.TrimSuffix(prefix, ".")
		if present, err := presentOrOptional(ctx, q, schemaName, step.table); err != nil {
			return err
		} else if !present {
			continue
		}
		if _, err := q.ExecContext(ctx, step.query, step.args...); err != nil {
			return fmt.Errorf("backup: omit %s: %w", step.table, err)
		}
	}
	return nil
}

// A moved conversation can remain in an earlier owner's frozen queue or
// completed receipt. The original turn owner is a fallback for receipts that
// no longer have a source queue or a saved anchor.
func omitReviewReceipts(ctx context.Context, q queryer, prefix string, threadIDs []any) error {
	values := strings.TrimRight(strings.Repeat("(?),", len(threadIDs)), ",")
	// The removed Wiki build kept receipts of its own; an older database may
	// still hold them, and they summarise the omitted sources too.
	wiki := ""
	if present, err := tableExists(ctx, q, strings.TrimSuffix(prefix, "."), "project_wiki_builds"); err != nil {
		return err
	} else if present {
		wiki = "UNION ALL SELECT project_id, CAST(receipt AS TEXT) FROM " + prefix + "project_wiki_builds"
	}
	query := `WITH omitted_threads(id) AS (VALUES ` + values + `),
		omitted_turns AS (SELECT id, project_id FROM ` + prefix + `turns WHERE thread_id IN omitted_threads),
		review_receipts AS (
			SELECT project_id, CAST(receipt AS TEXT) AS body FROM ` + prefix + `project_distillations
			` + wiki + `
		)
		SELECT project_id FROM (
			SELECT project_id FROM ` + prefix + `threads WHERE id IN omitted_threads
			UNION SELECT project_id FROM omitted_turns
			UNION SELECT project_id FROM ` + prefix + `project_distillation_sources WHERE thread_id IN omitted_threads
			UNION SELECT project_id FROM review_receipts WHERE CASE WHEN json_valid(body) THEN
				json_extract(body, '$.thread_id') IN omitted_threads OR
				json_extract(body, '$.turn_id') IN (SELECT id FROM omitted_turns)
			END
		) WHERE project_id <> ''`
	var affected []any
	if err := readStrings(ctx, q, query, threadIDs, 1, func(row []string) error {
		affected = append(affected, row[0])
		return nil
	}); err != nil {
		return fmt.Errorf("backup: identify reviews of omitted conversations: %w", err)
	}
	if len(affected) == 0 {
		return nil
	}
	marks := strings.TrimRight(strings.Repeat("?,", len(affected)), ",")
	for _, table := range []string{"project_distillation_sources", "project_distillations", "project_wiki_builds"} {
		if present, err := presentOrOptional(ctx, q, strings.TrimSuffix(prefix, "."), table); err != nil {
			return err
		} else if !present {
			continue
		}
		if _, err := q.ExecContext(ctx, "DELETE FROM "+prefix+table+" WHERE project_id IN ("+marks+")", affected...); err != nil {
			return fmt.Errorf("backup: omit %s: %w", table, err)
		}
	}
	return nil
}

func omittedIDs(threads []OmittedThread) []string {
	if len(threads) == 0 {
		return nil
	}
	ids := make([]string, 0, len(threads))
	seen := map[string]struct{}{}
	for _, thread := range threads {
		if _, ok := seen[thread.ID]; ok {
			continue
		}
		seen[thread.ID] = struct{}{}
		ids = append(ids, thread.ID)
	}
	return ids
}
