package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/backup"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestBackupCommandUsesTheRunningEngine(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	dir := t.TempDir()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	var seen map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/meta":
			_ = json.NewEncoder(w).Encode(map[string]any{"data_dir": abs, "mock": true})
		case "/api/backup":
			if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
				t.Error(err)
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"kind": "full"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	if err := lease.Publish(abs, lease.Record{PID: os.Getpid(), URL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.zwai-backup")
	var stdout, stderr bytes.Buffer
	if code := dispatch([]string{"backup", "--data-dir", abs, "--output", out, "--omit-secrets", "--skip-active"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code %d stderr %s", code, stderr.String())
	}
	if seen["output"] != out || seen["omit_secrets"] != true || seen["skip_active"] != true {
		t.Fatalf("request = %v", seen)
	}
	if _, err := os.Stat(filepath.Join(abs, "zwai.db")); !os.IsNotExist(err) {
		t.Fatal("backup opened the database while an engine was running")
	}
}

func TestRestoreWithoutReplacePrintsThePreview(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	project := &store.Project{Name: "kept"}
	if err := st.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	db, err := st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "full.zwai-backup")
	if _, err := backup.Create(context.Background(), backup.Options{
		DataDir: cfg.DataDir(), DB: db, Kind: backup.KindFull, Output: out,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := dispatch([]string{"restore", out, "--data-dir", cfg.DataDir()}, &stdout, &stderr); code != 2 {
		t.Fatalf("code %d stdout %s stderr %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "--replace") {
		t.Fatalf("stderr %s", stderr.String())
	}
	st, err = store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	changed := "changed"
	if err := st.UpdateProject(project.ID, map[string]any{"name": changed}); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	stdout.Reset()
	stderr.Reset()
	if code := dispatch([]string{"restore", out, "--replace", "--data-dir", cfg.DataDir()}, &stdout, &stderr); code != 0 {
		t.Fatalf("code %d stderr %s", code, stderr.String())
	}
	st, err = store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err := st.GetProject(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "kept" {
		t.Fatalf("name = %q", got.Name)
	}
}
