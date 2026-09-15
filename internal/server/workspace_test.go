// Tests for the workspace file endpoints: upload, list, download, delete, and
// the desktop-only reveal.
package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/server"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestUploadListDownloadDelete(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()

	resp := h.upload(id, map[string]string{"data.csv": "a,b\n1,2\n"})
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status %d: %s", resp.StatusCode, raw)
	}
	var uploaded struct {
		Files []store.Attachment `json:"files"`
	}
	if err := json.Unmarshal(raw, &uploaded); err != nil {
		t.Fatal(err)
	}
	if len(uploaded.Files) != 1 || uploaded.Files[0].RelPath != "uploads/data.csv" {
		t.Fatalf("uploads=%+v", uploaded.Files)
	}

	listed := h.json(http.MethodGet, "/api/threads/"+id+"/files", nil, http.StatusOK)
	if listed["workspace"] == "" {
		t.Fatal("the Files panel needs the workspace path to show")
	}
	var foundUploaded bool
	for _, f := range listed["files"].([]any) {
		entry := f.(map[string]any)
		if entry["path"] == "uploads/data.csv" && entry["uploaded"] == true {
			foundUploaded = true
		}
	}
	if !foundUploaded {
		t.Fatalf("the upload is not marked as human-provided: %v", listed["files"])
	}

	// download
	dl := h.do(http.MethodGet, "/api/threads/"+id+"/download/uploads/data.csv", nil)
	defer dl.Body.Close()
	body, _ := io.ReadAll(dl.Body)
	if dl.StatusCode != http.StatusOK || string(body) != "a,b\n1,2\n" {
		t.Fatalf("download status=%d body=%q", dl.StatusCode, body)
	}
	// workspace content must not be renderable in the app's own origin
	if cd := dl.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Fatalf("workspace files must be served as downloads, got %q", cd)
	}
	if dl.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("missing nosniff on a user-controlled file")
	}

	del := h.do(http.MethodDelete, "/api/threads/"+id+"/download/uploads/data.csv", nil)
	del.Body.Close()
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status %d", del.StatusCode)
	}
	h.json(http.MethodGet, "/api/threads/"+id+"/download/uploads/data.csv", nil, http.StatusNotFound)
}

// The download endpoint is unauthenticated on loopback; a traversal would turn
// the app into a file server for the whole machine.
func TestDownloadRefusesTraversal(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	for _, p := range []string{
		"/api/threads/" + id + "/download/../../../../etc/passwd",
		"/api/threads/" + id + "/download/uploads/../../../../etc/passwd",
	} {
		resp := h.do(http.MethodGet, p, nil)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK && strings.Contains(string(body), "root:") {
			t.Fatalf("%s served a host file", p)
		}
	}
	// a directory is not a download
	root := h.app.Engine.WorkspaceDir(id)
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o700); err != nil {
		t.Fatal(err)
	}
	h.json(http.MethodGet, "/api/threads/"+id+"/download/notes", nil, http.StatusBadRequest)
}

// Uploading the same name twice must keep both files: the second one is
// usually the corrected version, and silently overwriting loses work.
func TestUploadingTheSameNameTwiceKeepsBoth(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()

	for _, content := range []string{"first", "second"} {
		resp := h.upload(id, map[string]string{"报告 v1.md": content})
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("upload: %d %s", resp.StatusCode, body)
		}
	}

	listed := h.json(http.MethodGet, "/api/threads/"+id+"/files", nil, http.StatusOK)
	var names []string
	for _, f := range listed["files"].([]any) {
		entry := f.(map[string]any)
		if entry["uploaded"] == true {
			names = append(names, entry["path"].(string))
		}
	}
	if len(names) != 2 {
		t.Fatalf("want both uploads kept, got %v", names)
	}

	// and a non-ASCII name survives the round trip through the download header
	dl := h.do(http.MethodGet, "/api/threads/"+id+"/download/"+names[0], nil)
	body, _ := io.ReadAll(dl.Body)
	dl.Body.Close()
	if dl.StatusCode != http.StatusOK || len(body) == 0 {
		t.Fatalf("download %s: %d", names[0], dl.StatusCode)
	}
	if cd := dl.Header.Get("Content-Disposition"); !strings.Contains(cd, "UTF-8''") {
		t.Fatalf("a non-ASCII name needs RFC 5987 encoding, got %q", cd)
	}
}

func TestUploadValidation(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()

	resp := h.upload(id, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("an upload with no files should fail: %d", resp.StatusCode)
	}
	plain := h.do(http.MethodPost, "/api/threads/"+id+"/files", map[string]any{"nope": 1})
	plain.Body.Close()
	if plain.StatusCode != http.StatusBadRequest {
		t.Fatalf("a non-multipart upload should fail: %d", plain.StatusCode)
	}
}

func (h *harness) upload(threadID string, files map[string]string) *http.Response {
	h.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for name, content := range files {
		part, err := w.CreateFormFile("files", name)
		if err != nil {
			h.t.Fatal(err)
		}
		if _, err := part.Write([]byte(content)); err != nil {
			h.t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		h.t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost,
		h.srv.URL+"/api/threads/"+threadID+"/files", &buf)
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	return resp
}

// Revealing a file needs a desktop shell; in a browser it must fail clearly
// rather than looking broken.
func TestRevealIsDesktopOnly(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodPost, "/api/threads/"+id+"/reveal",
		map[string]any{"path": "uploads"}, http.StatusNotImplemented)
}

func TestRevealOnDesktop(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()

	var revealed string
	srv, err := server.New(server.Options{
		Engine: h.app.Engine,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Mode:   server.ModeDesktop,
		Reveal: func(path string) error { revealed = path; return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	post := func(body any) *http.Response {
		raw, _ := json.Marshal(body)
		resp, err := ts.Client().Post(ts.URL+"/api/threads/"+id+"/reveal",
			"application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	resp := post(map[string]any{})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reveal status %d", resp.StatusCode)
	}
	if revealed != h.app.Engine.WorkspaceDir(id) {
		t.Fatalf("an empty path should reveal the workspace, got %q", revealed)
	}
	resp = post(map[string]any{"path": "../../outside"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("reveal must refuse a path outside the workspace: %d", resp.StatusCode)
	}

	meta := map[string]any{}
	mresp, err := ts.Client().Get(ts.URL + "/api/meta")
	if err != nil {
		t.Fatal(err)
	}
	defer mresp.Body.Close()
	raw, _ := io.ReadAll(mresp.Body)
	_ = json.Unmarshal(raw, &meta)
	if meta["capabilities"].(map[string]any)["reveal"] != true {
		t.Fatalf("desktop mode must advertise reveal: %v", meta)
	}
}
