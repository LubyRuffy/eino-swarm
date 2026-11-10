package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/backup"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func runBackup(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dataDir := fs.String("data-dir", "", "data directory")
	output := fs.String("output", "", "backup file to write")
	projectID := fs.String("project", "", "back up one project instead of all data")
	omit := fs.Bool("omit-secrets", false, "leave API keys and remote credentials out of the backup")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"data-dir": true, "output": true, "project": true})); err != nil {
		return usageError{err.Error()}
	}
	if strings.TrimSpace(*output) == "" {
		return usageError{"usage: zwai backup --output FILE [--project ID] [--omit-secrets] [--data-dir DIR]"}
	}
	cfg, err := config.Load(*dataDir)
	if err != nil {
		return err
	}
	out, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	kind := backup.Kind(backup.KindFull)
	if strings.TrimSpace(*projectID) != "" {
		kind = backup.KindProject
	}
	body := map[string]any{"kind": string(kind), "project_id": *projectID, "omit_secrets": *omit, "output": out}
	return attachOrRun(cfg.DataDir(), func(url string) error {
		if _, err := postEngine(url, "/api/backup", body); err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	}, func() error {
		db, closeDB, err := openLocalDB(cfg.DataDir())
		if err != nil {
			return err
		}
		defer closeDB()
		if _, err := backup.Create(context.Background(), backup.Options{
			DataDir: cfg.DataDir(), DB: db, Kind: kind, ProjectID: *projectID, OmitSecrets: *omit, Output: out,
		}); err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	})
}

func runRestore(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dataDir := fs.String("data-dir", "", "data directory")
	replace := fs.Bool("replace", false, "replace all local data with a full backup")
	replaceProject := fs.Bool("replace-project", false, "replace the matching project with the backup")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"data-dir": true})); err != nil {
		return usageError{err.Error()}
	}
	if fs.NArg() != 1 {
		return usageError{"usage: zwai restore FILE [--replace | --replace-project] [--data-dir DIR]"}
	}
	cfg, err := config.Load(*dataDir)
	if err != nil {
		return err
	}
	file, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return err
	}
	return attachOrRun(cfg.DataDir(), func(url string) error {
		return restoreThroughEngine(url, file, *replace, *replaceProject)
	}, func() error {
		return restoreLocal(cfg.DataDir(), file, *replace, *replaceProject)
	})
}

func restoreThroughEngine(url, file string, replace, replaceProject bool) error {
	report, err := postEngine(url, "/api/backup/preview", map[string]any{"path": file})
	if err != nil {
		return err
	}
	action, _ := report["action"].(string)
	if !confirmedRestore(action, replace, replaceProject) {
		return printPreview(report, action)
	}
	if _, err := postEngine(url, "/api/backup/restore", map[string]any{
		"path": file, "confirm": true, "replace": replace, "replace_project": replaceProject,
	}); err != nil {
		return err
	}
	fmt.Println("restored", file)
	return nil
}

func restoreLocal(dataDir, file string, replace, replaceProject bool) error {
	db, closeDB, err := openLocalDB(dataDir)
	if err != nil {
		return err
	}
	defer closeDB()
	report, err := backup.Preview(context.Background(), dataDir, db, file)
	if err != nil {
		return err
	}
	if !confirmedRestore(report.Action, replace, replaceProject) {
		raw, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		if report.Action == backup.ActionReplaceAll {
			return usageError{"full restore needs --replace"}
		}
		return usageError{"restore needs an explicit choice"}
	}
	if _, err := backup.Restore(context.Background(), backup.RestoreOptions{
		DataDir: dataDir, DB: db, Path: file, Confirm: true, Replace: replace, ReplaceProject: replaceProject,
	}); err != nil {
		return err
	}
	fmt.Println("restored", file)
	return nil
}

func confirmedRestore(action string, replace, replaceProject bool) bool {
	switch action {
	case backup.ActionReplaceAll:
		return replace
	case backup.ActionReplaceProject:
		return replaceProject
	case backup.ActionAddProject:
		return true
	default:
		return false
	}
}

func printPreview(report map[string]any, action string) error {
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	if action == backup.ActionReplaceAll {
		return usageError{"full restore needs --replace"}
	}
	return usageError{"restore needs an explicit choice"}
}

func attachOrRun(dataDir string, remote func(string) error, local func() error) error {
	rec, err := discoverEngine(dataDir)
	if err == nil {
		return remote(rec.URL)
	}
	if !errors.Is(err, lease.ErrNotRunning) {
		return err
	}
	hold, err := lease.Acquire(dataDir)
	if errors.Is(err, lease.ErrHeld) {
		rec, err = discoverEngine(dataDir)
		if err != nil {
			return err
		}
		return remote(rec.URL)
	}
	if err != nil {
		return err
	}
	defer hold.Release()
	return local()
}

func discoverEngine(dataDir string) (lease.Record, error) {
	rec, err := lease.Discover(dataDir, false)
	if errors.Is(err, lease.ErrMockMismatch) {
		return lease.Discover(dataDir, true)
	}
	return rec, err
}

func openLocalDB(dataDir string) (*sql.DB, func(), error) {
	st, err := store.Open(filepath.Join(dataDir, "zwai.db"))
	if err != nil {
		return nil, nil, err
	}
	db, err := st.DB().DB()
	if err != nil {
		_ = st.Close()
		return nil, nil, err
	}
	return db, func() { _ = st.Close() }, nil
}

func postEngine(base, path string, body map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(base, "/")+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s", strings.TrimSpace(string(payload)))
	}
	if len(bytes.TrimSpace(payload)) == 0 {
		return map[string]any{}, nil
	}
	var out map[string]any
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, err
	}
	return out, nil
}
