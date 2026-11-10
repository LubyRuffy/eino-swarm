package server_test

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
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
	if busy["code"] != "backup_busy" {
		t.Fatalf("busy = %v", busy)
	}
	stopped := h.do(http.MethodPost, "/api/threads/"+id+"/interrupt", nil)
	stopped.Body.Close()
	h.waitTurnDone(id)
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
