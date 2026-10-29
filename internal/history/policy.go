package history

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

var publicTables = map[string]bool{
	"projects": true, "threads": true, "messages": true, "turns": true, "events": true,
	"attachments": true, "schedules": true, "schedule_runs": true, "llm_calls": true,
}

type policy struct {
	roots     map[int]bool
	functions map[string]bool
}

func prepareSnapshot(ctx context.Context, tx *sql.Tx) (policy, []Table, error) {
	p := policy{roots: map[int]bool{}, functions: map[string]bool{}}
	rows, err := tx.QueryContext(ctx, `SELECT name,tbl_name,type,rootpage FROM main.sqlite_schema WHERE type IN ('table','index','view') ORDER BY name`)
	if err != nil {
		return p, nil, err
	}
	type object struct {
		name, table, kind string
		root              int
	}
	objects := []object{}
	for rows.Next() {
		var o object
		if err := rows.Scan(&o.name, &o.table, &o.kind, &o.root); err != nil {
			rows.Close()
			return p, nil, err
		}
		objects = append(objects, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return p, nil, err
	}
	schema := []Table{}
	for _, o := range objects {
		if publicTables[o.table] && (o.kind == "table" || o.kind == "index") && o.root > 1 {
			p.roots[o.root] = true
		}
		if o.kind == "index" || strings.HasPrefix(o.name, "sqlite_") {
			continue
		}
		if publicTables[o.name] && o.kind == "table" {
			table, err := describeTable(ctx, tx, o.name)
			if err != nil {
				return p, nil, err
			}
			schema = append(schema, table)
			if o.name != "events" {
				continue
			}
			// Checkpoints are private recovery state, not published history.
			_, err = tx.ExecContext(ctx, `CREATE TEMP VIEW events AS SELECT * FROM main.events WHERE kind IS NOT 'engine_restart_checkpoint'`)
			if err != nil {
				return p, nil, err
			}
			continue
		}
		// Shadow every other source table/view so an existing source view
		// cannot bypass the event filter. The root-page check independently
		// refuses direct reads of private tables and sqlite_schema.
		if _, err := tx.ExecContext(ctx, `CREATE TEMP VIEW `+quote(o.name)+` AS SELECT * FROM temp.history_table_unavailable`); err != nil {
			return p, nil, err
		}
	}
	functions, err := tx.QueryContext(ctx, `PRAGMA function_list`)
	if err != nil {
		return p, nil, err
	}
	defer functions.Close()
	for functions.Next() {
		var name, kind, enc string
		var builtin, args, flags int
		if err := functions.Scan(&name, &builtin, &kind, &enc, &args, &flags); err != nil {
			return p, nil, err
		}
		// SQLITE_INNOCUOUS is SQLite's public function-safety flag. A fresh
		// driver supplies no application functions or external-file helpers.
		if builtin == 1 && flags&(1<<21) != 0 {
			p.functions[strings.ToLower(name)] = true
		}
	}
	return p, schema, functions.Err()
}

func quote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func describeTable(ctx context.Context, tx *sql.Tx, name string) (Table, error) {
	out := Table{Name: name, Columns: []Column{}}
	rows, err := tx.QueryContext(ctx, `PRAGMA main.table_info(`+quote(name)+`)`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var def any
		var col Column
		if err := rows.Scan(&cid, &col.Name, &col.Type, &notnull, &def, &pk); err != nil {
			return out, err
		}
		out.Columns = append(out.Columns, col)
	}
	return out, rows.Err()
}

func (p policy) admit(ctx context.Context, tx *sql.Tx, statement string, params []any) error {
	rows, err := tx.QueryContext(ctx, "EXPLAIN "+statement, params...)
	if err != nil {
		return fmt.Errorf("prepare history SELECT: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var addr, p1, p2, p3, p5 int
		var opcode string
		var p4, comment sql.NullString
		if err := rows.Scan(&addr, &opcode, &p1, &p2, &p3, &p4, &p5, &comment); err != nil {
			return err
		}
		switch opcode {
		case "OpenRead", "ReopenIdx":
			if p3 != 0 || p5&16 != 0 || !p.roots[p2] {
				return fmt.Errorf("query reads unavailable history data")
			}
		case "OpenEphemeral", "OpenAutoindex", "OpenPseudo", "OpenDup", "SorterOpen":
			// SELECT sorting, CTEs and automatic indexes use temporary cursors.
		case "Function", "PureFunc", "AggStep", "AggStep1", "AggFinal", "AggValue", "AggInverse":
			name := strings.ToLower(p4.String)
			if index := strings.LastIndexByte(name, '('); index >= 0 {
				name = name[:index]
			}
			if !p.functions[name] {
				return fmt.Errorf("function %q is unavailable in history queries", name)
			}
		case "Transaction":
			if p1 != 0 || p2 != 0 {
				return fmt.Errorf("history query requests a writable transaction")
			}
		case "VOpen", "VUpdate", "VCreate", "VDestroy", "Vacuum", "SqlExec", "ParseSchema", "LoadAnalysis", "CreateBtree", "Destroy", "Clear", "JournalMode", "Checkpoint", "SetCookie":
			return fmt.Errorf("operation %s is unavailable in history queries", opcode)
		default:
			if strings.Contains(opcode, "Open") {
				return fmt.Errorf("unsupported history cursor %s", opcode)
			}
		}
	}
	return rows.Err()
}
