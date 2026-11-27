package backup

import (
	"context"
	"fmt"
)

// legacyTables are written by features the application no longer has. A new
// database never creates them and an older one keeps them on disk, so they are
// optional everywhere a table list is walked. They are still carried and still
// filtered when a conversation is omitted: leaving them out of the omission
// would leak that conversation's content through a table nobody reads.
var legacyTables = map[string]bool{
	"project_wiki_builds": true,
	"wiki_source_scans":   true,
	"nuwa_nodes":          true,
	"nuwa_revisions":      true,
	"nuwa_verify_slots":   true,
}

// graphTables arrived after the first archives were written. An older archive
// has none of them and is still restorable; a current database always has them.
var graphTables = map[string]bool{
	"graph_subjects":     true,
	"graph_subject_keys": true,
	"graph_links":        true,
	"graph_todos":        true,
	"graph_revisions":    true,
}

func tableExists(ctx context.Context, q queryer, schema, table string) (bool, error) {
	if !identRe.MatchString(schema) || !identRe.MatchString(table) {
		return false, invalidf("identifier")
	}
	var n int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM `+schema+`.sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// presentOrOptional reports whether a table-list walk should touch the table
// in schema: required tables always, legacy ones only when they exist.
func presentOrOptional(ctx context.Context, q queryer, schema, table string) (bool, error) {
	if !legacyTables[table] && !graphTables[table] {
		return true, nil
	}
	return tableExists(ctx, q, schema, table)
}

// carryLegacyTable makes sure the destination can receive a legacy table that
// only the source has. The copy keeps the source's columns and none of its
// constraints, which is all a table that is only ever carried needs.
func carryLegacyTable(ctx context.Context, q queryer, src, dst, table string) (bool, error) {
	ok, err := tableExists(ctx, q, src, table)
	if err != nil || !ok {
		return false, err
	}
	have, err := tableExists(ctx, q, dst, table)
	if err != nil {
		return false, err
	}
	if !have {
		name, err := quoteIdent(table)
		if err != nil {
			return false, err
		}
		if _, err := q.ExecContext(ctx, `CREATE TABLE `+dst+`.`+name+` AS SELECT * FROM `+src+`.`+name+` WHERE 0`); err != nil {
			return false, fmt.Errorf("backup: carry legacy table %s: %w", table, err)
		}
	}
	return true, nil
}
