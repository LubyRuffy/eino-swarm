// Package history provides a bounded, read-only SQL view of retained history.
package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Reader opens a separate read-only snapshot for each retained-history query.
type Reader struct {
	path         string
	projectID    string
	projectBound bool
}

// Result preserves complete source rows and reports an explicit output limit.
type Result struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	RowCount  int      `json:"row_count"`
	Truncated bool     `json:"truncated"`
	Reason    string   `json:"reason,omitempty"`
	Schema    []Table  `json:"schema,omitempty"`
}

// Table describes a currently available raw history table.
type Table struct {
	Name    string   `json:"name"`
	Columns []Column `json:"columns"`
}

// Column records a live SQLite column name and its declared type.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// New binds an unscoped reader for standalone conversations to an existing
// SQLite file. It retains the global history view and does not open or migrate it.
func New(path string) *Reader { return &Reader{path: path} }

// NewProject binds original-history queries to one existing project. Empty or
// missing project identities fail on Query instead of falling back to New.
func NewProject(path, projectID string) *Reader {
	return &Reader{path: path, projectID: projectID, projectBound: true}
}

// Query runs a SELECT against retained raw history. Empty SQL describes its
// current columns. Each call owns a read snapshot and never occupies the
// engine's single writer connection. The caller supplies its context deadline.
func (r *Reader) Query(ctx context.Context, statement string, params []any, maxChars int) (result Result, err error) {
	defer func() {
		if ctx.Err() != nil {
			result = Result{}
			err = errors.Join(err, context.Cause(ctx))
		}
	}()
	if maxChars <= 0 {
		return Result{}, fmt.Errorf("history result budget must be positive")
	}
	if captureRegistrationError != nil {
		return Result{}, fmt.Errorf("prepare history row collector: %w", captureRegistrationError)
	}
	if strings.TrimSpace(r.path) == "" || r.path == ":memory:" {
		return Result{}, fmt.Errorf("history needs an existing database file")
	}
	clean, err := selectSQL(statement)
	if err != nil {
		return Result{}, err
	}
	abs, err := filepath.Abs(r.path)
	if err != nil {
		return Result{}, err
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	q := u.Query()
	q.Set("mode", "ro")
	q.Add("_pragma", "trusted_schema(OFF)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return Result{}, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Result{}, fmt.Errorf("open history snapshot: %w", err)
	}
	defer func() {
		// No source writes can be committed. Closing the private DB immediately
		// afterwards also releases a snapshot already cancelled by database/sql.
		_ = tx.Rollback()
	}()
	p, schema, err := prepareSnapshot(ctx, tx)
	if err != nil {
		return Result{}, err
	}
	if r.projectBound {
		if err := applyProjectScope(ctx, tx, schema, r.projectID); err != nil {
			return Result{}, err
		}
	}
	if clean == "" {
		if len(params) > 0 {
			return Result{}, fmt.Errorf("schema inspection takes no parameters")
		}
		return fitResult(Result{Columns: []string{}, Rows: [][]any{}, Schema: schema}, maxChars)
	}
	if _, err := tx.ExecContext(ctx, "PRAGMA query_only=ON"); err != nil {
		return Result{}, err
	}
	wrapped := "SELECT * FROM (\n" + clean + "\n) AS history_query"
	if err := p.admit(ctx, tx, wrapped, params); err != nil {
		return Result{}, err
	}
	return collectQuery(ctx, tx, wrapped, params, maxChars)
}

func fitResult(out Result, maxChars int) (Result, error) {
	body, err := json.Marshal(out)
	if err != nil {
		return Result{}, err
	}
	if utf8.RuneCount(body) > maxChars {
		return Result{}, fmt.Errorf("history result metadata exceeds result budget; use narrower columns or increase the configured result limit")
	}
	return out, nil
}
