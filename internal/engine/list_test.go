package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestListWorkspaceEmptyWhenCapIsZero(t *testing.T) {
	if got := listWorkspace(t.TempDir(), 0); got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
}

func TestListWorkspaceEmptyWhenRootIsMissing(t *testing.T) {
	got := listWorkspace(filepath.Join(t.TempDir(), "missing"), 8)
	if len(got) != 0 {
		t.Fatalf("want empty, got %+v", got)
	}
}

func TestListWorkspaceTakesRootSiblingsBeforeDescending(t *testing.T) {
	// Directories at a depth are recorded before files, and both beat
	// descending. Otherwise a fat folder that sorts early spends the cap
	// and the Files panel disagrees with the file manager at the same path.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "aaa"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "aaa", "deep.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "zzz"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "root.txt"), []byte("y"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := listWorkspace(dir, 3)
	want := []string{"aaa", "zzz", "root.txt"}
	if len(got) != len(want) {
		t.Fatalf("got %d entries: %+v", len(got), got)
	}
	for i, p := range want {
		if got[i].Path != p {
			t.Fatalf("entry %d: got %q want %q (%+v)", i, got[i].Path, p, got)
		}
	}
}

func TestListWorkspaceDoesNotHideSiblingsBehindAFatDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "aaa"), 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		name := fmt.Sprintf("f%02d", i)
		if err := os.WriteFile(filepath.Join(dir, "aaa", name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "zzz"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "zzz", "n.md"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "root.txt"), []byte("y"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := listWorkspace(dir, 20)
	seen := map[string]bool{}
	for _, f := range got {
		seen[f.Path] = true
	}
	for _, p := range []string{"aaa", "zzz", "zzz/n.md", "root.txt"} {
		if !seen[p] {
			t.Fatalf("fat directory hid %s; listing %+v", p, got)
		}
	}
	if len(got) != 20 {
		t.Fatalf("want cap 20, got %d", len(got))
	}
}

func TestListWorkspaceStopsAtTheCap(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	got := listWorkspace(dir, 2)
	if len(got) != 2 {
		t.Fatalf("want 2, got %d %+v", len(got), got)
	}
}

func TestListFilesDoesNotHideSiblingsBehindAFatDirectory(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	root := e.WorkspaceDir(th.ID)

	if err := os.Mkdir(filepath.Join(root, "aaa"), 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		name := fmt.Sprintf("f%02d", i)
		if err := os.WriteFile(filepath.Join(root, "aaa", name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "zzz"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "zzz", "n.md"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "root.txt"), []byte("y"), 0o600); err != nil {
		t.Fatal(err)
	}

	files, err := e.ListFiles(th.ID)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	seen := map[string]bool{}
	for _, f := range files {
		seen[f.Path] = true
	}
	for _, p := range []string{"aaa", "zzz", "zzz/n.md", "root.txt"} {
		if !seen[p] {
			t.Fatalf("ListFiles hid %s behind a fat directory: %+v", p, files)
		}
	}
}
