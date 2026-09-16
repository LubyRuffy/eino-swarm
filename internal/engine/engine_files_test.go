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

func TestListFilesSkipsDependencyTrees(t *testing.T) {
	// A project pointed at a repository would otherwise spend the 2000-entry
	// cap on .git / node_modules / vendor before the panel lists anything
	// the human recognizes. Agents can still read those paths.
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	root := e.WorkspaceDir(th.ID)
	hidden := []string{
		filepath.Join(root, ".git", "objects", "pack"),
		filepath.Join(root, "node_modules", "left-pad"),
		filepath.Join(root, "vendor", "mod"),
	}
	for _, dir := range hidden {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "blob"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "readme.md"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "a.go"), []byte("package pkg"), 0o600); err != nil {
		t.Fatal(err)
	}

	files, err := e.ListFiles(th.ID)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	byPath := map[string]FileEntry{}
	for _, f := range files {
		byPath[f.Path] = f
	}
	if _, ok := byPath["readme.md"]; !ok {
		t.Fatalf("visible file missing: %+v", files)
	}
	if !byPath["pkg"].Dir {
		t.Fatalf("visible directory missing: %+v", files)
	}
	for _, p := range []string{".git", "node_modules", "vendor"} {
		if _, ok := byPath[p]; ok {
			t.Fatalf("listing included %s: %+v", p, files)
		}
	}
}

func writeUpload(content string) func(string) error {
	return func(dst string) error { return os.WriteFile(dst, []byte(content), 0o600) }
}

func TestStartTurnNamesThisTurnsUploadsNotLeftovers(t *testing.T) {
	// Uploads share one folder for the whole conversation. A later "what is
	// this" has to name the files on THIS send, or the model scavenges an
	// older archive sitting next to them and the human thinks the app
	// opened the wrong file.
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	leftover, err := e.SaveUpload(th.ID, "leftover.bin", writeUpload("old"))
	if err != nil {
		t.Fatal(err)
	}
	current, err := e.SaveUpload(th.ID, "current.csv", writeUpload("a,b\n"))
	if err != nil {
		t.Fatal(err)
	}

	turn, err := e.StartTurnInput(th.ID, UserInput{
		Text:  "what is this",
		Files: []string{current.RelPath, current.RelPath},
	})
	if err != nil {
		t.Fatalf("StartTurnInput: %v", err)
	}
	if !strings.Contains(turn.UserText, current.RelPath) {
		t.Fatalf("this turn's upload must be named on the message: %q", turn.UserText)
	}
	if strings.Count(turn.UserText, current.RelPath) != 1 {
		t.Fatalf("a path listed twice on the wire is still one attachment: %q", turn.UserText)
	}
	if strings.Contains(turn.UserText, leftover.RelPath) {
		t.Fatalf("an older upload must not be presented as this message's file: %q", turn.UserText)
	}
	if !strings.Contains(turn.UserText, "what is this") {
		t.Fatalf("the caption still has to be there: %q", turn.UserText)
	}

	list, err := e.Store().ListAttachments(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]string{}
	for _, a := range list {
		byPath[a.RelPath] = a.TurnID
	}
	if byPath[current.RelPath] != turn.ID {
		t.Fatalf("this turn's upload should be bound to the turn, got %q", byPath[current.RelPath])
	}
	if byPath[leftover.RelPath] != "" {
		t.Fatalf("a leftover upload is not this turn's: %q", byPath[leftover.RelPath])
	}

	finished := waitForTurn(t, e, turn.ID)
	msgs, err := e.Store().ListMessages(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	var user string
	for _, m := range msgs {
		if m.Role == "user" {
			user = m.Content
			break
		}
	}
	if user != finished.UserText {
		t.Fatalf("replay must see the same named files as the timeline: %q vs %q", user, finished.UserText)
	}
}

func TestAFileAloneStillStartsATurn(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	att, err := e.SaveUpload(th.ID, "current.csv", writeUpload("a,b\n"))
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurnInput(th.ID, UserInput{Files: []string{att.RelPath}})
	if err != nil {
		t.Fatalf("a file with no caption still has to start a turn: %v", err)
	}
	if !strings.Contains(turn.UserText, att.RelPath) {
		t.Fatalf("the message has to name the file: %q", turn.UserText)
	}
	waitForTurn(t, e, turn.ID)
}

func TestAttachedFileMustBeAHumanUpload(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	ghost := filepath.Join(e.WorkspaceDir(th.ID), "uploads", "ghost.csv")
	if err := os.MkdirAll(filepath.Dir(ghost), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ghost, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartTurnInput(th.ID, UserInput{
		Text:  "what is this",
		Files: []string{"uploads/ghost.csv"},
	}); err == nil {
		t.Fatal("an agent-written path must not ride in as a human attachment")
	}
	if _, err := e.StartTurnInput(th.ID, UserInput{
		Text:  "what is this",
		Files: []string{"notes/report.md"},
	}); err == nil {
		t.Fatal("only uploads/ can be attached to a message")
	}
	if _, err := e.StartTurnInput(th.ID, UserInput{
		Text:  "what is this",
		Files: []string{"uploads/../notes/x"},
	}); err == nil {
		t.Fatal("a traversal must not attach a file outside uploads/")
	}
}
