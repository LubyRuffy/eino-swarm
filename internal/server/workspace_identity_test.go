package server_test

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A workspace may hold symlinks an agent made on purpose; agents have full
// access. The HTTP request naming a path is still untrusted input (C-004): a
// link to a directory outside the workspace must not turn the loopback API
// into a reader or deleter for that directory.
func TestWorkspaceSymlinkCannotCarryHTTPFileRequestsOutside(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	root := h.app.Engine.WorkspaceDir(id)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("outside bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "secret-link.txt")); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{"linked/secret.txt", "secret-link.txt"} {
		resp := h.do(http.MethodGet, "/api/threads/"+id+"/download/"+rel, nil)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if strings.Contains(string(body), "outside bytes") {
			t.Fatalf("download of %s served a file outside the workspace (status %d)", rel, resp.StatusCode)
		}
	}

	del := h.do(http.MethodDelete, "/api/threads/"+id+"/download/linked/secret.txt", nil)
	del.Body.Close()
	if _, err := os.Stat(secret); err != nil {
		t.Fatalf("delete through a workspace symlink removed a file outside the workspace (status %d): %v", del.StatusCode, err)
	}
	if del.StatusCode == http.StatusNoContent {
		t.Fatal("delete through a workspace symlink reported success")
	}

	// Removing the link itself is a workspace change and stays allowed; the
	// target it pointed at is untouched.
	del = h.do(http.MethodDelete, "/api/threads/"+id+"/download/linked", nil)
	del.Body.Close()
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("deleting the link itself: status %d", del.StatusCode)
	}
	if _, err := os.Lstat(filepath.Join(root, "linked")); !os.IsNotExist(err) {
		t.Fatalf("the link is still there: %v", err)
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatalf("deleting the link removed its target: %v", err)
	}
}

// File names are identities. A file whose name ends in a space is a different
// file from its trimmed sibling; resolving one to the other made Delete remove
// the wrong file and Download serve it.
func TestWorkspaceFileRequestsKeepTheExactName(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	root := h.app.Engine.WorkspaceDir(id)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "report.md"), []byte("trimmed sibling"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "report.md "), []byte("spaced name"), 0o600); err != nil {
		t.Fatal(err)
	}
	escaped := url.PathEscape("report.md ")

	dl := h.do(http.MethodGet, "/api/threads/"+id+"/download/"+escaped, nil)
	body, _ := io.ReadAll(dl.Body)
	dl.Body.Close()
	if dl.StatusCode != http.StatusOK || string(body) != "spaced name" {
		t.Fatalf("download of %q: status %d body %q", "report.md ", dl.StatusCode, body)
	}

	del := h.do(http.MethodDelete, "/api/threads/"+id+"/download/"+escaped, nil)
	del.Body.Close()
	if del.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status %d", del.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(root, "report.md")); err != nil {
		t.Fatalf("deleting %q removed its trimmed sibling: %v", "report.md ", err)
	}
	if _, err := os.Stat(filepath.Join(root, "report.md ")); !os.IsNotExist(err) {
		t.Fatalf("the requested file is still there: %v", err)
	}
}
