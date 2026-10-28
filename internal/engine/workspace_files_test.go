package engine

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func hasDotDotSegment(rel string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

var workspacePathSeeds = []string{
	"notes.md", "notes.md ", " notes.md", "\tnotes.md", "a//b", "./a", "a/./b", "/etc/passwd",
	"..", "../x", "a/../b", "a/..", "...", "..a", "a/", "/", "//", ".", "", "   ", "\x00",
	"linked", "linked/secret.txt", "linked/sub/x", "inner/linked/secret.txt", "file.txt/x",
	"inside/inner-link/x", "uploads/报告 v1.md",
}

// The HTTP and phone file entries hand untrusted paths to this function. A
// path it accepts is the file the client named: the same name up to "./" and
// repeated slashes, never trimmed, and never reached through ".." (C-004).
func FuzzWorkspaceRelNameKeepsTheNamedFile(f *testing.F) {
	for _, s := range workspacePathSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, rel string) {
		name, err := workspaceRelName(rel)
		clean := path.Clean("/" + filepath.ToSlash(rel))
		rejectable := strings.TrimSpace(rel) == "" || hasDotDotSegment(rel) || clean == "/"
		if err != nil {
			if !rejectable {
				t.Fatalf("workspaceRelName(%q) refused a plain workspace path: %v", rel, err)
			}
			return
		}
		if rejectable {
			t.Fatalf("workspaceRelName(%q) = %q, want a refusal", rel, name)
		}
		if "/"+filepath.ToSlash(name) != clean {
			t.Fatalf("workspaceRelName(%q) = %q names a different file than %q", rel, name, clean)
		}
		if filepath.IsAbs(name) || hasDotDotSegment(name) {
			t.Fatalf("workspaceRelName(%q) = %q leaves the workspace", rel, name)
		}
	})
}

// Attaching an upload must name that upload. Trimming its name either refused
// a real upload or attached its trimmed sibling to the message instead.
func TestAttachedUploadKeepsItsExactName(t *testing.T) {
	e := newTestEngine(t)
	th := &store.Thread{Title: "attach"}
	if err := e.store.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"plan.md": "trimmed", "plan.md ": "spaced"} {
		if _, err := e.SaveUpload(th.ID, name, func(dst string) error {
			return os.WriteFile(dst, []byte(body), 0o600)
		}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := e.resolveAttachedFiles(th.ID, []string{"uploads/plan.md "})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].RelPath != "uploads/plan.md " {
		t.Fatalf("attached %+v, want only uploads/plan.md with its trailing space", got)
	}
	if _, err := e.resolveAttachedFiles(th.ID, []string{"  "}); err == nil {
		t.Fatal("a blank attachment path was accepted")
	}
}

// newSymlinkWorkspace gives one conversation a workspace whose links point
// both outside it and inside it.
func newSymlinkWorkspace(t testing.TB, e *Engine) (threadID, root, outside string) {
	th := &store.Thread{Title: "files"}
	if err := e.store.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	root = e.WorkspaceDir(th.ID)
	outside = filepath.Join(filepath.Dir(filepath.Dir(root)), "outside")
	for _, dir := range []string{filepath.Join(root, "inside"), filepath.Join(outside, "sub")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{
		filepath.Join(root, "file.txt"):        "inside",
		filepath.Join(outside, "secret.txt"):   "outside",
		filepath.Join(root, "inside", "x"):     "inside x",
		filepath.Join(outside, "sub", "x"):     "outside x",
		filepath.Join(root, "notes.md "):       "spaced",
		filepath.Join(root, "notes.md"):        "trimmed",
		filepath.Join(root, "inside", "y.txt"): "y",
	} {
		if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	links := map[string]string{
		filepath.Join(root, "linked"):                outside,
		filepath.Join(root, "inside", "inner-link"):  filepath.Join(root, "inside"),
		filepath.Join(root, "secret-link.txt"):       filepath.Join(outside, "secret.txt"),
		filepath.Join(root, "inside", "y-link.txt"):  filepath.Join(root, "inside", "y.txt"),
		filepath.Join(root, "inside", "up-and-out"):  "../../outside",
		filepath.Join(root, "inside", "up-and-back"): "../inside",
	}
	for link, target := range links {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	return th.ID, root, outside
}

// existingParentInside reports whether the deepest existing directory above
// abs, with every symlink resolved, is inside root.
func existingParentInside(root, abs string) bool {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	dir := filepath.Dir(abs)
	for {
		real, err := filepath.EvalSymlinks(dir)
		if err == nil {
			return real == rootReal || strings.HasPrefix(real, rootReal+string(os.PathSeparator))
		}
		if dir == root || len(dir) <= len(root) {
			return true
		}
		dir = filepath.Dir(dir)
	}
}

// Agents may leave links to anywhere in a workspace; the client naming a path
// may not use them to reach outside it. Every accepted path keeps its name and
// resolves its parent inside the workspace; the last component may itself be a
// link, which the reading and deleting callers handle through os.Root.
func FuzzResolveWorkspacePathStaysInside(f *testing.F) {
	for _, s := range workspacePathSeeds {
		f.Add(s)
	}
	for _, s := range []string{"inside/up-and-out/secret.txt", "inside/up-and-back/x", "inside/inner-link/inner-link/x"} {
		f.Add(s)
	}
	e := newTestEngineAt(f, f.TempDir())
	threadID, root, _ := newSymlinkWorkspace(f, e)
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, rel string) {
		abs, err := e.ResolveWorkspacePath(threadID, rel)
		if err != nil {
			return
		}
		if !strings.HasPrefix(abs, rootAbs+string(os.PathSeparator)) {
			t.Fatalf("ResolveWorkspacePath(%q) = %q, outside %q", rel, abs, rootAbs)
		}
		name, _ := workspaceRelName(rel)
		if abs != filepath.Join(rootAbs, name) {
			t.Fatalf("ResolveWorkspacePath(%q) = %q renamed the file", rel, abs)
		}
		if !existingParentInside(rootAbs, abs) {
			t.Fatalf("ResolveWorkspacePath(%q) = %q walks through a link out of the workspace", rel, abs)
		}
	})
}
