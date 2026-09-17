package server_test

import (
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LubyRuffy/eino-swarm/internal/server"
)

// Every endpoint has to answer even when the database is gone. A panic here
// takes the whole app down, and a hang leaves the UI spinning forever.
func TestEndpointsFailCleanlyWithoutADatabase(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	if err := h.app.Store.Close(); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/threads", nil},
		{http.MethodPost, "/api/threads", map[string]any{"title": "x"}},
		{http.MethodPut, "/api/threads/reorder", map[string]any{"ids": []string{id}}},
		{http.MethodPut, "/api/projects/reorder", map[string]any{"ids": []string{"pj_1"}}},
		{http.MethodGet, "/api/threads/" + id, nil},
		{http.MethodPatch, "/api/threads/" + id, map[string]any{"title": "y"}},
		{http.MethodDelete, "/api/threads/" + id, nil},
		{http.MethodGet, "/api/threads/" + id + "/turns", nil},
		{http.MethodPost, "/api/threads/" + id + "/turns", map[string]any{"text": "go"}},
		{http.MethodPost, "/api/threads/" + id + "/steer", map[string]any{"text": "go"}},
		{http.MethodGet, "/api/threads/" + id + "/followups", nil},
		{http.MethodPost, "/api/threads/" + id + "/followups", map[string]any{"text": "go"}},
		{http.MethodPatch, "/api/threads/" + id + "/followups/fu_x", map[string]any{"text": "go"}},
		{http.MethodPost, "/api/threads/" + id + "/interrupt", nil},
		{http.MethodPost, "/api/threads/" + id + "/continue", map[string]any{"continue": true}},
		{http.MethodPost, "/api/threads/" + id + "/compact", nil},
		{http.MethodGet, "/api/threads/" + id + "/files", nil},
		{http.MethodGet, "/api/threads/" + id + "/input-images/img_deadbeef", nil},
		{http.MethodGet, "/api/threads/" + id + "/events", nil},
		{http.MethodGet, "/api/threads/" + id + "/log", nil},
		{http.MethodGet, "/api/trace/anything", nil},
	} {
		resp := h.do(tc.method, tc.path, tc.body)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode < 400 || resp.StatusCode >= 500 {
			t.Fatalf("%s %s: status %d, want a client-visible error: %s",
				tc.method, tc.path, resp.StatusCode, body)
		}
		if !strings.Contains(string(body), "error") {
			t.Fatalf("%s %s: the UI cannot show this: %s", tc.method, tc.path, body)
		}
	}
	// and settings still read, because they live in a file, not the database
	h.json(http.MethodGet, "/api/settings", nil, http.StatusOK)
}

func TestMalformedBodiesAreRejected(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	for _, path := range []string{
		"/api/threads",
		"/api/threads/" + id + "/turns",
		"/api/threads/" + id + "/steer",
		"/api/threads/" + id + "/followups",
		"/api/threads/" + id + "/continue",
		"/api/threads/" + id + "/reveal",
		"/api/open",
	} {
		resp := h.do(http.MethodPost, path, "\"not an object\"")
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusNotImplemented {
			t.Fatalf("POST %s accepted garbage: %d", path, resp.StatusCode)
		}
	}
	h.json(http.MethodPatch, "/api/threads/"+id, "\"nope\"", http.StatusBadRequest)
	// a PATCH that changes nothing is not an error, it is a no-op
	h.json(http.MethodPatch, "/api/threads/"+id, map[string]any{}, http.StatusOK)
	h.json(http.MethodPatch, "/api/threads/"+id+"/followups/fu_x", "\"nope\"", http.StatusBadRequest)
}

// Deleting is idempotent: two clicks on the same delete button, or a retry
// after a dropped response, must not produce an error the user has to think
// about.
func TestDeletingAMissingFileSucceeds(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	resp := h.do(http.MethodDelete, "/api/threads/"+id+"/download/uploads/ghost.txt", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status %d", resp.StatusCode)
	}
	// but a path outside the workspace is still refused
	resp = h.do(http.MethodDelete, "/api/threads/"+id+"/download/../../../etc/hosts", nil)
	resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		t.Fatal("delete accepted a path outside the workspace")
	}
}

// When the file manager refuses to open, say so instead of pretending it
// worked: the user is standing there watching for a window to appear.
func TestRevealReportsAFailingFileManager(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	srv, err := server.New(server.Options{
		Engine: h.app.Engine,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Mode:   server.ModeDesktop,
		Reveal: func(string) error { return errors.New("no file manager on this machine") },
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := ts.Client().Post(ts.URL+"/api/threads/"+id+"/reveal",
		"application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError ||
		!strings.Contains(string(body), "no file manager") {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
}

// A workspace the process cannot write to has to produce an error the user
// can read, not a half-written file.
func TestUploadIntoAnUnwritableWorkspace(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permissions are not enforced")
	}
	h := newHarness(t)
	id := h.newThread()
	uploads := filepath.Join(h.app.Engine.WorkspaceDir(id), "uploads")
	if err := os.MkdirAll(uploads, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(uploads, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(uploads, 0o700) })

	resp := h.upload(id, map[string]string{"blocked.txt": "x"})
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Fatalf("an unwritable workspace accepted an upload: %d %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "error") {
		t.Fatalf("the failure is not reportable: %s", body)
	}
}

// A build with no front end still has to serve the API: that is how the
// handler tests and `zwai trace` run.
func TestAServerWithNoBundleStillServesTheAPI(t *testing.T) {
	h := newHarness(t)
	srv, err := server.New(server.Options{
		Engine: h.app.Engine,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		// an "assets" tree without an index.html is a broken build, not a
		// reason to refuse to start
		Assets: fs.FS(fstest.MapFS{"robots.txt": &fstest.MapFile{Data: []byte("x")}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api/meta")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("meta status %d", resp.StatusCode)
	}
	resp, err = ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("with no bundle the root should 404, got %d", resp.StatusCode)
	}
}

// A POST to a path that only exists for GET must not be answered with the app
// shell, or a failed request looks like a successful page load.
func TestNonGETUnknownRoutesAreNotTheAppShell(t *testing.T) {
	h := newHarness(t)
	srv, err := server.New(server.Options{
		Engine: h.app.Engine,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Assets: fs.FS(fstest.MapFS{
			"index.html": &fstest.MapFile{Data: []byte("<html>app shell</html>")},
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := ts.Client().Post(ts.URL+"/whatever", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || strings.Contains(string(body), "app shell") {
		t.Fatalf("POST to an unknown path: %d %q", resp.StatusCode, body)
	}
}
