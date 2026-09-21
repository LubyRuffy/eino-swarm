package server_test

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LubyRuffy/eino-swarm/internal/app"
	"github.com/LubyRuffy/eino-swarm/internal/server"
)

func writeUI(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func serveUI(t *testing.T, assets fs.FS) (*httptest.Server, func()) {
	t.Helper()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	a, err := app.New(app.Options{
		DataDir: t.TempDir(), Mock: true, Mode: server.ModeWeb,
		Assets: assets,
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(a.Server.Handler())
	stop := func() {
		ts.Close()
		a.Search.Stop()
		a.Engine.Shutdown()
		_ = a.Store.Close()
	}
	return ts, stop
}

func TestAssetsServeSPAWithFallback(t *testing.T) {
	dir := t.TempDir()
	writeUI(t, dir, map[string]string{
		"index.html":     "<html>app shell</html>",
		"assets/main.js": "console.log(1)",
	})
	ts, stop := serveUI(t, os.DirFS(dir))
	defer stop()

	for _, tc := range []struct {
		path, wantBody, wantCache string
	}{
		{"/", "app shell", "no-store"},
		{"/assets/main.js", "console.log(1)", "public, max-age=31536000, immutable"},
		// a deep link and a reload must land on the app, not a 404
		{"/threads/th_abc", "app shell", "no-store"},
	} {
		resp, err := ts.Client().Get(ts.URL + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), tc.wantBody) {
			t.Fatalf("GET %s: %d %q", tc.path, resp.StatusCode, body)
		}
		if got := resp.Header.Get("Cache-Control"); got != tc.wantCache {
			t.Fatalf("GET %s cache-control=%q want %q", tc.path, got, tc.wantCache)
		}
	}

	// an unknown API path is an error, never the app shell: a fetch that gets
	// HTML back fails in a way that is very hard to debug
	resp, err := ts.Client().Get(ts.URL + "/api/does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || strings.Contains(string(body), "app shell") {
		t.Fatalf("unknown api path: %d %q", resp.StatusCode, body)
	}
}

func TestMissingHashedAssetsAreNotTheAppShell(t *testing.T) {
	dir := t.TempDir()
	writeUI(t, dir, map[string]string{
		"index.html": "<html>app shell</html>",
	})
	ts, stop := serveUI(t, os.DirFS(dir))
	defer stop()

	for _, path := range []string{"/assets", "/assets/", "/assets/index-old.js", "/gone.css"} {
		resp, err := ts.Client().Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		ct := resp.Header.Get("Content-Type")
		if resp.StatusCode != http.StatusNotFound || strings.Contains(string(body), "app shell") {
			t.Fatalf("GET %s: %d %q", path, resp.StatusCode, body)
		}
		if strings.Contains(ct, "text/html") {
			t.Fatalf("GET %s content-type=%q; WebKit will not run HTML as a module", path, ct)
		}
	}
}

func TestAssetsKeepTheBundleWhenTheDistDirectoryIsRebuilt(t *testing.T) {
	dir := t.TempDir()
	writeUI(t, dir, map[string]string{
		"index.html":          `<html><script type="module" src="/assets/index-old.js"></script>app shell</html>`,
		"assets/index-old.js": "console.log('old')",
	})
	ts, stop := serveUI(t, os.DirFS(dir))
	defer stop()

	// Vite empties dist/assets and writes new hashes. A second `go run`
	// must not take the already-open window with it.
	if err := os.Remove(filepath.Join(dir, "assets", "index-old.js")); err != nil {
		t.Fatal(err)
	}
	writeUI(t, dir, map[string]string{
		"index.html":          `<html><script type="module" src="/assets/index-new.js"></script>new shell</html>`,
		"assets/index-new.js": "console.log('new')",
	})

	resp, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "index-old.js") || strings.Contains(string(body), "new shell") {
		t.Fatalf("running window lost its shell: %s", body)
	}

	resp, err = ts.Client().Get(ts.URL + "/assets/index-old.js")
	if err != nil {
		t.Fatal(err)
	}
	js, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(js), "old") {
		t.Fatalf("hashed JS vanished from under the window: %d %q", resp.StatusCode, js)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("hashed JS content-type=%q", ct)
	}

	resp, err = ts.Client().Get(ts.URL + "/assets/index-new.js")
	if err != nil {
		t.Fatal(err)
	}
	newer, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || strings.Contains(string(newer), "shell") {
		t.Fatalf("a rebuild's files leaked into the running window: %d %q", resp.StatusCode, newer)
	}
}

func TestAssetsStayUpWhenTheBundleCannotBeFrozen(t *testing.T) {
	index := []byte("<html>app shell</html>")
	ts, stop := serveUI(t, walkFailsFS{index: index})
	defer stop()

	resp, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "app shell") {
		t.Fatalf("live fallback: %d %q", resp.StatusCode, body)
	}

	resp, err = ts.Client().Get(ts.URL + "/assets/index-old.js")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || strings.Contains(string(body), "app shell") {
		t.Fatalf("missing JS on a live tree: %d %q", resp.StatusCode, body)
	}
}

func TestALiveAssetDirectoryIsNotListed(t *testing.T) {
	ts, stop := serveUI(t, liveDirFS{index: []byte("<html>app shell</html>")})
	defer stop()

	resp, err := ts.Client().Get(ts.URL + "/assets")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || strings.Contains(string(body), "app shell") || strings.Contains(string(body), "x.js") {
		t.Fatalf("GET /assets listed the tree: %d %q", resp.StatusCode, body)
	}
}

// walkFailsFS has index.html but cannot be walked, which is the freeze
// failure the live-directory fallback has to survive.
type walkFailsFS struct {
	index []byte
}

func (f walkFailsFS) Open(name string) (fs.File, error) {
	if name == "index.html" {
		return fstest.MapFS{"index.html": {Data: f.index}}.Open("index.html")
	}
	return nil, errors.New("cannot walk")
}

// liveDirFS freezes fail (WalkDir cannot start) but the live tree still has
// an assets directory, which must 404 rather than list hashed names.
type liveDirFS struct {
	index []byte
}

func (f liveDirFS) Open(name string) (fs.File, error) {
	files := fstest.MapFS{
		"index.html":  {Data: f.index},
		"assets/x.js": {Data: []byte("console.log(1)")},
	}
	if name == "." {
		return nil, errors.New("cannot walk")
	}
	return files.Open(name)
}
