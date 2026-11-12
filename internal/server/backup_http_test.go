package server_test

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupRestoreRoundTripOverHTTP(t *testing.T) {
	h := newHarness(t)
	meta := h.json(http.MethodGet, "/api/meta", nil, http.StatusOK)
	if meta["data_generation"] != float64(0) {
		t.Fatalf("generation = %v", meta["data_generation"])
	}
	created := h.json(http.MethodPost, "/api/projects", map[string]any{"name": "kept"}, http.StatusCreated)
	project := created["project"].(map[string]any)
	id := project["id"].(string)
	out := filepath.Join(t.TempDir(), "full.zwai-backup")
	h.json(http.MethodPost, "/api/backup", map[string]any{"kind": "full", "output": out}, http.StatusOK)
	changed := "changed"
	h.json(http.MethodPatch, "/api/projects/"+id, map[string]any{"name": changed}, http.StatusOK)
	preview := h.json(http.MethodPost, "/api/backup/preview", map[string]any{"path": out}, http.StatusOK)
	if preview["action"] != "replace-all" {
		t.Fatalf("preview = %v", preview["action"])
	}
	refused := h.json(http.MethodPost, "/api/backup/restore", map[string]any{"path": out, "confirm": true}, http.StatusBadRequest)
	if refused["code"] != "confirmation_required" {
		t.Fatalf("restore without replace = %v", refused)
	}
	h.json(http.MethodPost, "/api/backup/restore", map[string]any{"path": out, "confirm": true, "replace": true}, http.StatusOK)
	got := h.json(http.MethodGet, "/api/projects/"+id, nil, http.StatusOK)
	if got["project"].(map[string]any)["name"] != "kept" {
		t.Fatalf("project = %v", got["project"])
	}
	meta = h.json(http.MethodGet, "/api/meta", nil, http.StatusOK)
	if meta["data_generation"] != float64(1) {
		t.Fatalf("generation after restore = %v", meta["data_generation"])
	}
}

func TestBackupDownloadAndBusyTurn(t *testing.T) {
	h := newHarness(t)
	resp := h.do(http.MethodPost, "/api/backup", map[string]any{"kind": "full"})
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if !bytes.HasPrefix(body, []byte("PK")) {
		t.Fatal("download is not a zip")
	}
	id := h.newThread()
	h.json(http.MethodPost, "/api/threads/"+id+"/turns", map[string]any{"text": "look into this"}, http.StatusAccepted)
	busy := h.json(http.MethodPost, "/api/backup/restore", map[string]any{"path": filepath.Join(t.TempDir(), "missing.zwai-backup"), "confirm": true, "replace": true}, http.StatusConflict)
	if busy["code"] != "backup_busy" || busy["skippable"] == true {
		t.Fatalf("restore busy = %v", busy)
	}
	if active, _ := busy["active"].([]any); len(active) == 0 {
		t.Fatalf("restore hid the running conversation: %v", busy)
	}
	stopped := h.do(http.MethodPost, "/api/threads/"+id+"/interrupt", nil)
	stopped.Body.Close()
	h.waitTurnDone(id)
}

func TestBackupLeavesAWorkspaceSymlinkOut(t *testing.T) {
	h := newHarness(t)
	meta := h.json(http.MethodGet, "/api/meta", nil, http.StatusOK)
	dir, _ := meta["data_dir"].(string)
	if dir == "" {
		t.Fatal("meta hid the data directory")
	}
	lab := filepath.Join(dir, "workspaces", "th_link", "lab")
	if err := os.MkdirAll(filepath.Join(lab, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lab, "notes.txt"), []byte("lab-notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "python")
	if err := os.WriteFile(outside, []byte("interpreter-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(lab, "bin", "python")); err != nil {
		t.Fatal(err)
	}
	resp := h.do(http.MethodPost, "/api/backup", map[string]any{"kind": "full"})
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	if resp.Header.Get("X-Zwai-Skipped-Symlinks") != "1" {
		t.Fatalf("header = %q", resp.Header.Get("X-Zwai-Skipped-Symlinks"))
	}
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	var sawNotes bool
	for _, file := range reader.File {
		if strings.Contains(file.Name, "bin/python") {
			t.Fatalf("archive stored the link %s", file.Name)
		}
		if strings.HasSuffix(file.Name, "notes.txt") {
			sawNotes = true
		}
	}
	if !sawNotes {
		t.Fatal("archive dropped the regular file")
	}
}

func TestBackupCanSkipARunningTurn(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodPost, "/api/threads/"+id+"/turns", map[string]any{"text": "look into this"}, http.StatusAccepted)
	t.Cleanup(func() {
		stopped := h.do(http.MethodPost, "/api/threads/"+id+"/interrupt", nil)
		stopped.Body.Close()
	})
	out := filepath.Join(t.TempDir(), "skip.zwai-backup")
	busy := h.json(http.MethodPost, "/api/backup", map[string]any{"kind": "full", "output": out}, http.StatusConflict)
	if busy["code"] != "backup_busy" || busy["skippable"] != true {
		t.Fatalf("busy = %v", busy)
	}
	found := false
	for _, item := range busy["active"].([]any) {
		row, _ := item.(map[string]any)
		if row["id"] == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("active = %v", busy["active"])
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("refused backup wrote a file")
	}
	manifest := h.json(http.MethodPost, "/api/backup", map[string]any{"kind": "full", "output": out, "skip_active": true}, http.StatusOK)
	omitted, _ := manifest["omitted_threads"].([]any)
	if len(omitted) != 1 || omitted[0].(map[string]any)["id"] != id {
		t.Fatalf("manifest = %v", manifest["omitted_threads"])
	}
	h.json(http.MethodGet, "/api/threads/"+id, nil, http.StatusOK)
	preview := h.json(http.MethodPost, "/api/backup/preview", map[string]any{"path": out}, http.StatusOK)
	previewManifest, _ := preview["manifest"].(map[string]any)
	previewOmitted, _ := previewManifest["omitted_threads"].([]any)
	if len(previewOmitted) != 1 {
		t.Fatalf("preview = %v", previewManifest)
	}
}

func TestBackupUploadRestoresAProjectBesideAnother(t *testing.T) {
	source := newHarness(t)
	created := source.json(http.MethodPost, "/api/projects", map[string]any{"name": "carried"}, http.StatusCreated)
	id := created["project"].(map[string]any)["id"].(string)
	resp := source.do(http.MethodPost, "/api/backup", map[string]any{"kind": "project", "project_id": id})
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !bytes.HasPrefix(body, []byte("PK")) {
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}
	dest := newHarness(t)
	dest.json(http.MethodPost, "/api/projects", map[string]any{"name": "already"}, http.StatusCreated)
	req, err := http.NewRequest(http.MethodPost, dest.srv.URL+"/api/backup/preview", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	previewResp, err := dest.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer previewResp.Body.Close()
	if previewResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(previewResp.Body)
		t.Fatalf("preview %d: %s", previewResp.StatusCode, raw)
	}
	req, err = http.NewRequest(http.MethodPost, dest.srv.URL+"/api/backup/restore?confirm=1", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	restoreResp, err := dest.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer restoreResp.Body.Close()
	if restoreResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(restoreResp.Body)
		t.Fatalf("restore %d: %s", restoreResp.StatusCode, raw)
	}
	got := dest.json(http.MethodGet, "/api/projects/"+id, nil, http.StatusOK)
	if got["project"].(map[string]any)["name"] != "carried" {
		t.Fatalf("joined project = %v", got["project"])
	}
	if _, err := os.Stat(filepath.Join(dest.app.Engine.Config().DataDir(), "zwai.db")); err != nil {
		t.Fatal(err)
	}
}

func TestBackupProgressIsIdleWhenNothingIsTransferring(t *testing.T) {
	h := newHarness(t)
	body := h.json(http.MethodGet, "/api/backup/progress", nil, http.StatusOK)
	if body["running"] == true || body["phase"] != nil || body["done"] != float64(0) {
		t.Fatalf("progress = %v", body)
	}
}
