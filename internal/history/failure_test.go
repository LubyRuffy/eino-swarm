package history

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
)

// A transparent connection wrapper injects storage failure at the real
// metadata read; all successful statements still use the actual SQLite file.
// A partial schema cannot authorize execution when a required read failed.
func TestIncompleteSchemaAdmissionFailsClosedOnStorageErrors(t *testing.T) {
	path, source := historyFixture(t)
	for _, queryPrefix := range []string{"PRAGMA main.table_info", "PRAGMA function_list"} {
		t.Run(queryPrefix, func(t *testing.T) {
			failure := errors.New("required history metadata read failed")
			db := sql.OpenDB(&historyFaultConnector{base: source.Driver(), path: path, prefix: queryPrefix, failure: failure})
			defer db.Close()
			tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback() }()
			_, schema, err := prepareSnapshot(context.Background(), tx)
			if !errors.Is(err, failure) || schema != nil {
				t.Fatalf("incomplete admission returned schema=%#v error=%v", schema, err)
			}
		})
	}
}

type historyFaultConnector struct {
	base         driver.Driver
	path, prefix string
	failure      error
}

func (c *historyFaultConnector) Driver() driver.Driver { return c.base }
func (c *historyFaultConnector) Connect(context.Context) (driver.Conn, error) {
	conn, err := c.base.Open("file:" + c.path + "?mode=ro")
	if err != nil {
		return nil, err
	}
	return &historyFaultConn{Conn: conn, prefix: c.prefix, failure: c.failure}, nil
}

type historyFaultConn struct {
	driver.Conn
	prefix  string
	failure error
}

func (c *historyFaultConn) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, options)
}

func (c *historyFaultConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(query, c.prefix) {
		return nil, c.failure
	}
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, query, args)
}

func (c *historyFaultConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(query, c.prefix) {
		return nil, c.failure
	}
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, query, args)
}
