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
	runArgs := make([]any, 0, len(args)*2)
	runArgs = append(runArgs, args...)
	runArgs = append(runArgs, args...)
	steps := []struct {
		table string
		query string
		args  []any
	}{
		{"schedule_runs", `DELETE FROM ` + prefix + `schedule_runs WHERE ` + in("thread_id") +
			` OR schedule_id IN (SELECT id FROM ` + prefix + `schedules WHERE ` + in("thread_id") + `)`, runArgs},
		{"composer_draft_blobs", `DELETE FROM ` + prefix + `composer_draft_blobs WHERE ` + in("draft_key"), drafts},
		{"composer_drafts", `DELETE FROM ` + prefix + `composer_drafts WHERE ` + in("draft_key"), drafts},
		{"search_chunks", `DELETE FROM ` + prefix + `search_chunks WHERE ` + in("thread_id"), args},
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
		if _, err := q.ExecContext(ctx, step.query, step.args...); err != nil {
			return fmt.Errorf("backup: omit %s: %w", step.table, err)
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
