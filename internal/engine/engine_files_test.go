package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------- workspace paths ----------

// The download endpoint is unauthenticated on loopback; a path that escapes
// the workspace would turn the app into a file server for the whole machine.
func TestResolveWorkspacePathRejectsEscapes(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	root := e.WorkspaceDir(th.ID)

	ok, err := e.ResolveWorkspacePath(th.ID, "notes/report.md")
	if err != nil {
		t.Fatalf("a normal relative path was rejected: %v", err)
	}
	if ok != filepath.Join(root, "notes", "report.md") {
		t.Fatalf("resolved to %q", ok)
	}

	for _, bad := range []string{
		"../../etc/passwd",
		"../" + filepath.Base(root) + "-other/x",
		"notes/../../../../../../etc/passwd",
		"",
		"   ",
		".",
	} {
		if got, err := e.ResolveWorkspacePath(th.ID, bad); err == nil {
			t.Fatalf("path %q escaped the workspace to %q", bad, got)
		}
	}

	// An absolute path is read as workspace-relative rather than rejected, so
	// a client that sends a leading slash gets its own file, never the host's.
	abs, err := e.ResolveWorkspacePath(th.ID, "/etc/passwd")
	if err != nil {
		t.Fatalf("an absolute path should be confined, not rejected: %v", err)
	}
	if abs != filepath.Join(root, "etc", "passwd") {
		t.Fatalf("absolute path resolved outside the workspace: %q", abs)
	}
}

func TestUploadAndDeleteFiles(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")

	write := func(content string) func(string) error {
		return func(dst string) error { return os.WriteFile(dst, []byte(content), 0o600) }
	}
	att, err := e.SaveUpload(th.ID, "input.csv", write("a,b\n1,2\n"))
	if err != nil {
		t.Fatalf("SaveUpload: %v", err)
	}
	if att.RelPath != "uploads/input.csv" {
		t.Fatalf("uploads should be grouped: %q", att.RelPath)
	}
	if att.Size == 0 {
		t.Fatalf("size not recorded: %+v", att)
	}

	// a second upload with the same name must not overwrite the first
	second, err := e.SaveUpload(th.ID, "input.csv", write("different"))
	if err != nil {
		t.Fatal(err)
	}
	if second.RelPath == att.RelPath {
		t.Fatalf("the second upload overwrote the first: %q", second.RelPath)
	}
	first, err := os.ReadFile(filepath.Join(e.WorkspaceDir(th.ID), filepath.FromSlash(att.RelPath)))
	if err != nil || string(first) != "a,b\n1,2\n" {
		t.Fatalf("the first upload was clobbered: %q %v", first, err)
	}

	// a path in the name must not place the file outside uploads/
	escaped, err := e.SaveUpload(th.ID, "../../evil.sh", write("x"))
	if err != nil {
		t.Fatalf("SaveUpload: %v", err)
	}
	if !strings.HasPrefix(escaped.RelPath, "uploads/") || strings.Contains(escaped.RelPath, "..") {
		t.Fatalf("an upload escaped the uploads directory: %q", escaped.RelPath)
	}

	if _, err := e.SaveUpload(th.ID, "  ", write("x")); err == nil {
		t.Fatal("want an error for an upload with no name")
	}
	if _, err := e.SaveUpload("missing", "a.txt", write("x")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	list, err := e.Store().ListAttachments(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("want 3 recorded uploads, got %d", len(list))
	}

	if err := e.DeleteFile(th.ID, att.RelPath); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.WorkspaceDir(th.ID), filepath.FromSlash(att.RelPath))); !os.IsNotExist(err) {
		t.Fatalf("the file survived the delete: %v", err)
	}
	list, _ = e.Store().ListAttachments(th.ID)
	if len(list) != 2 {
		t.Fatalf("the attachment record survived: %+v", list)
	}
	if err := e.DeleteFile(th.ID, "../outside"); err == nil {
		t.Fatal("DeleteFile must refuse a path outside the workspace")
	}
}

func TestListFilesReportsDirsAndSizes(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	root := e.WorkspaceDir(th.ID)
	if err := os.MkdirAll(filepath.Join(root, "notes", "deep"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "deep", "a.md"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}

	files, err := e.ListFiles(th.ID)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	byPath := map[string]FileEntry{}
	for _, f := range files {
		byPath[f.Path] = f
		if strings.Contains(f.Path, string(os.PathSeparator)) && os.PathSeparator != '/' {
			t.Fatalf("paths must be slash-separated for the UI: %q", f.Path)
		}
	}
	if !byPath["notes"].Dir || !byPath["notes/deep"].Dir {
		t.Fatalf("directories not reported: %+v", files)
	}
	f := byPath["notes/deep/a.md"]
	if f.Dir || f.Size != 5 || f.Modified.IsZero() {
		t.Fatalf("file entry wrong: %+v", f)
	}

	// listing a conversation with no workspace yet must not fail
	if _, err := e.ListFiles("never-existed"); err != nil {
		t.Fatalf("ListFiles on a fresh conversation: %v", err)
	}
}
