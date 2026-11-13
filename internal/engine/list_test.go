package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestListFilesReturnsEveryDirectChild(t *testing.T) {
	// A sibling directory used to spend a global listing cap, so the Files
	// panel showed a directory that disagreed with the file manager.
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	root := e.WorkspaceDir(th.ID)
	kept := filepath.Join(root, "kept")
	noise := filepath.Join(root, "noise")
	if err := os.Mkdir(kept, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(noise, 0o700); err != nil {
		t.Fatal(err)
	}
	const n = 2001
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("f%04d.txt", i)
		if err := os.WriteFile(filepath.Join(kept, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(noise, name), []byte("y"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "root.txt"), []byte("z"), 0o600); err != nil {
		t.Fatal(err)
	}

	rootEntries, err := e.ListFiles(th.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	gotRoot := map[string]FileEntry{}
	for _, entry := range rootEntries {
		gotRoot[entry.Path] = entry
	}
	if len(gotRoot) != 3 || !gotRoot["kept"].Dir || !gotRoot["noise"].Dir || gotRoot["root.txt"].Dir {
		t.Fatalf("root listing = %+v", rootEntries)
	}
	for _, entry := range rootEntries {
		if len(entry.Path) > len("noise/") && entry.Path[:6] == "noise/" {
			t.Fatalf("root listing descended into a sibling: %+v", rootEntries)
		}
	}

	listed, err := e.ListFiles(th.ID, "kept")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != n {
		t.Fatalf("directory listing omitted children: %d of %d", len(listed), n)
	}
	seen := map[string]bool{}
	for _, entry := range listed {
		if entry.Dir || entry.Name == "" || entry.Path != "kept/"+entry.Name || entry.Size != 1 {
			t.Fatalf("child entry = %+v", entry)
		}
		seen[entry.Name] = true
	}
	if !seen["f0000.txt"] || !seen["f2000.txt"] {
		t.Fatalf("endpoints missing: %d names", len(seen))
	}
}

func TestListFilesRejectsEscapeAndNonDirectories(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	root := e.WorkspaceDir(th.ID)
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ListFiles(th.ID, "../outside"); err == nil {
		t.Fatal("listing escaped the workspace")
	}
	if _, err := e.ListFiles(th.ID, "notes.txt"); err == nil {
		t.Fatal("a file was listed as a directory")
	}
	if _, err := e.ListFiles(th.ID, "missing"); err == nil {
		t.Fatal("a missing directory was listed")
	}
	outside := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("no"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ListFiles(th.ID, "linked"); err == nil {
		t.Fatal("a directory symlink was walked")
	}
}

func TestSearchFilesReturnsEveryMatchAndSkipsHiddenTrees(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	root := e.WorkspaceDir(th.ID)
	for _, dir := range []string{
		filepath.Join(root, "notes"),
		filepath.Join(root, "node_modules", "pkg"),
		filepath.Join(root, ".git"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "Match.md"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "other.txt"), []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "pkg", "match.js"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "match"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := e.SearchFiles(context.Background(), th.ID, "match")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "notes/Match.md" || got[0].Dir || got[0].Size != 3 {
		t.Fatalf("search = %+v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled, err := e.SearchFiles(ctx, th.ID, "match")
	if err == nil || cancelled != nil {
		t.Fatalf("cancelled search returned %+v, %v", cancelled, err)
	}
	blank, err := e.SearchFiles(context.Background(), th.ID, " ")
	if err == nil || blank != nil {
		t.Fatalf("blank search returned %+v, %v", blank, err)
	}
}

func TestWorkspaceListingFailsClosedWhenItCannotBeRead(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	root := e.WorkspaceDir(th.ID)
	blocked := filepath.Join(root, "blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "secret.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o700) })
	unreadable, err := e.SearchFiles(context.Background(), th.ID, "secret")
	if err == nil || unreadable != nil {
		t.Fatalf("unreadable directory returned %+v, %v", unreadable, err)
	}

	if err := os.Chmod(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ListFiles(th.ID, ""); err == nil {
		t.Fatal("a file was used as the workspace root")
	}
	children, err := e.ListFiles(th.ID, "notes")
	if err == nil || children != nil {
		t.Fatalf("listing under an unreadable root returned %+v, %v", children, err)
	}
	found, err := e.SearchFiles(context.Background(), th.ID, "notes")
	if err == nil || found != nil {
		t.Fatalf("search of an unreadable root returned %+v, %v", found, err)
	}
}
