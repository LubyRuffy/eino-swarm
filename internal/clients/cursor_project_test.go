package clients

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCursorProjectKeepsTheRealDirectoryName(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "src", "github.com", "demo-app")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src", "github.com", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src", "github-com"), 0o755); err != nil {
		t.Fatal(err)
	}
	slug := "src-github-com-demo-app"
	if got, full := decodeCursorProject(root, slug); got != project || !full {
		t.Fatalf("decoded = %s full=%v", got, full)
	}
	dotted := filepath.Join(root, "src", "github.com")
	if got, full := decodeCursorProject(root, "src-github-com"); got != dotted || !full {
		t.Fatalf("dotted directory = %s full=%v", got, full)
	}
	if got, full := decodeCursorProject(root, "src-missing"); got != "" || full {
		t.Fatalf("missing project = %s full=%v", got, full)
	}
	transcript := filepath.Join(root, "projects", slug, "agent-transcripts", "u1", "u1.jsonl")
	if cursorSlug(transcript) != slug {
		t.Fatalf("slug = %s", cursorSlug(transcript))
	}
	if cursorSlug(filepath.Join(root, "elsewhere", "u1.jsonl")) != "" {
		t.Fatal("a file outside agent-transcripts produced a slug")
	}
	if cursorProjectDir("..") != "" || cursorProjectDir("  ") != "" {
		t.Fatal("unsafe slug decoded")
	}
}

func TestCursorProjectReadsAHiddenWorktreeAndFallsBackToItsRepository(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	leaf := filepath.Join(repo, ".worktrees", "feature")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "repo-worktrees"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: ../.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, full := decodeCursorProject(root, "repo-worktrees-feature"); got != leaf || !full {
		t.Fatalf("worktree = %s full=%v", got, full)
	}
	if got, full := decodeCursorProject(root, "repo-worktrees-gone"); got != repo || full {
		t.Fatalf("removed worktree = %s full=%v", got, full)
	}
}

func TestCursorProjectRemembersAResolvedDirectory(t *testing.T) {
	root := t.TempDir()
	slug := "cursor-cache-project"
	project := filepath.Join(root, slug)
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cursorSlugCache.Delete(slug) })
	if got := lookupCursorProject(root, slug); got != project {
		t.Fatalf("resolved = %s", got)
	}
	if err := os.Remove(project); err != nil {
		t.Fatal(err)
	}
	if got := lookupCursorProject(root, slug); got != project {
		t.Fatalf("cached = %s", got)
	}
	transcript := filepath.Join(root, "projects", "cursor-missing-project-label", "agent-transcripts", "u1", "u1.jsonl")
	if cursorCWD(transcript) != "" {
		t.Fatal("missing project produced a directory")
	}
}

func TestCursorProjectIgnoresUnreadableAndDotOnlyNames(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "..."), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "kept.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	kept := filepath.Join(root, "kept")
	if err := os.MkdirAll(kept, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, full := decodeCursorProject(root, "kept"); got != kept || !full {
		t.Fatalf("kept = %s full=%v", got, full)
	}
	blocked := filepath.Join(root, "blocked")
	if err := os.MkdirAll(filepath.Join(blocked, "leaf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })
	if _, err := os.Open(blocked); err == nil {
		t.Fatal("unreadable directory was readable")
	}
	if got, full := decodeCursorProject(root, "blocked-leaf"); got != "" || full {
		t.Fatalf("blocked = %s full=%v", got, full)
	}
	if cursorRepoDir(root, "") != "" || cursorRepoDir(root, ".") != "" || cursorRepoDir(root, "foo") != "" {
		t.Fatal("empty or stalled path became a project")
	}
}

func TestCursorProjectFollowsALinkedDirectory(t *testing.T) {
	root := t.TempDir()
	leaf := filepath.Join(root, "real", "leaf")
	if err := os.MkdirAll(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(filepath.Join(root, "real"), link); err != nil {
		t.Skip(err)
	}
	want := filepath.Join(link, "leaf")
	if got, full := decodeCursorProject(root, "linked-leaf"); got != want || !full {
		t.Fatalf("linked = %s full=%v", got, full)
	}
}
