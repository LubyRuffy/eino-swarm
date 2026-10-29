package history

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	sqlite "github.com/glebarez/go-sqlite"
)

const captureFunction = "zwai_collect_history_row"
const truncatedReason = "result budget exceeded; use SQL LIMIT, narrower columns, or substr to page a large value"

var resultBudgetReached = errors.New("history result budget reached")
var collectors sync.Map
var collectorSequence atomic.Int64

// Registration happens during package initialization, before Store can open
// connections. This driver stores its function registry in an ordinary map.
// User SQL cannot invoke this function: admission only accepts innocuous
// built-ins, and this application function has neither property.
var captureRegistrationError = sqlite.RegisterScalarFunction(captureFunction, -1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("history collector identity is missing")
	}
	id, ok := args[0].(int64)
	if !ok {
		return nil, fmt.Errorf("invalid history collector identity")
	}
	value, ok := collectors.Load(id)
	if !ok {
		return nil, fmt.Errorf("history collector is not active")
	}
	return value.(*collector).append(args[1:])
})

type collector struct {
	result               Result
	maxChars, used, base int
	stop                 error
}

func collectQuery(ctx context.Context, tx *sql.Tx, statement string, params []any, maxChars int) (Result, error) {
	rows, err := tx.QueryContext(ctx, statement, params...)
	if err != nil {
		return Result{}, fmt.Errorf("prepare history columns: %w", err)
	}
	columns, err := rows.Columns()
	closeErr := rows.Close()
	if err != nil {
		return Result{}, err
	}
	if closeErr != nil {
		return Result{}, closeErr
	}
	c := &collector{result: Result{Columns: columns, Rows: [][]any{}}, maxChars: maxChars}
	reserve := c.result
	reserve.Truncated = true
	reserve.Reason = truncatedReason
	encoded, err := json.Marshal(reserve)
	if err != nil {
		return Result{}, err
	}
	c.base = utf8.RuneCount(encoded)
	if _, err := fitResult(reserve, maxChars); err != nil {
		return Result{}, err
	}
	id := collectorSequence.Add(1)
	collectors.Store(id, c)
	defer collectors.Delete(id)
	fields := make([]string, len(columns))
	for i, column := range columns {
		field := "history_rows." + quote(column)
		// The pinned driver's TEXT callback uses a NUL-terminated conversion.
		// Preserve SQLite's explicit text length by transporting bytes as BLOB
		// alongside their type. A one-byte prefix also keeps an empty BLOB's
		// pointer nonzero: this driver dereferences even a zero-length buffer.
		fields[i] = "typeof(" + field + "),CASE WHEN typeof(" + field + ") IN ('text','blob') THEN CAST(x'01'||CAST(" + field + " AS BLOB) AS BLOB) ELSE " + field + " END"
	}
	// The pinned driver only watches cancellation during ExecContext's step;
	// Rows.Next has no watcher. One aggregate step therefore owns the complete
	// scan. SUM also preserves an inner ORDER BY (unlike count/min/max's
	// special order-elision optimization). No results are manually summarized.
	// OFFSET 0 prevents flattening this boundary. Each source expression is
	// evaluated once, even when its type depends on a nondeterministic value.
	wrapper := "SELECT sum(" + captureFunction + "(" + strconv.FormatInt(id, 10) + "," + strings.Join(fields, ",") + ")) FROM (SELECT * FROM (\n" + statement + "\n) LIMIT -1 OFFSET 0) AS history_rows"
	_, err = tx.ExecContext(ctx, wrapper, params...)
	if c.stop != nil && !errors.Is(c.stop, resultBudgetReached) {
		return Result{}, c.stop
	}
	if err != nil && !errors.Is(c.stop, resultBudgetReached) {
		return Result{}, fmt.Errorf("query history: %w", err)
	}
	return fitResult(c.result, maxChars)
}

func (c *collector) append(values []driver.Value) (driver.Value, error) {
	if len(values) != 2*len(c.result.Columns) {
		c.stop = fmt.Errorf("history collector received incomplete columns")
		return nil, c.stop
	}
	row := make([]any, len(c.result.Columns))
	for i := range row {
		kind, value := values[2*i], values[2*i+1]
		if kind == "text" || kind == "blob" {
			bytes, ok := value.([]byte)
			if !ok || len(bytes) == 0 || bytes[0] != 1 {
				c.stop = fmt.Errorf("history collector received invalid byte transport")
				return nil, c.stop
			}
			if kind == "text" {
				row[i] = string(bytes[1:])
			} else {
				row[i] = bytes[1:]
			}
		} else {
			row[i] = value
		}
	}
	body, err := json.Marshal(row)
	if err != nil {
		c.stop = err
		return nil, err
	}
	cost := utf8.RuneCount(body)
	if len(c.result.Rows) > 0 {
		cost++
	}
	if c.base+c.used+cost+len(strconv.Itoa(len(c.result.Rows)+1))-1 > c.maxChars {
		c.result.Truncated = true
		c.result.Reason = truncatedReason
		c.stop = resultBudgetReached
		return nil, c.stop
	}
	c.used += cost
	c.result.Rows = append(c.result.Rows, row)
	c.result.RowCount = len(c.result.Rows)
	return int64(1), nil
}
