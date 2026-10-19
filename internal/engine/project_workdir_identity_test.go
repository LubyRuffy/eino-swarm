package engine

import (
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"testing"
)

// A picker returns a filesystem identity, not prose. A similarly named sibling
// must never become the workspace just because the selected name ends in space.
func TestProjectWorkingDirectoryPreservesTheSelectedIdentity(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("Windows does not support these trailing-whitespace directory names")
	}
	for _, siblingExists := range []bool{false, true} {
		name := "selected directory only"
		if siblingExists {
			name = "different trimmed sibling also exists"
		}
		t.Run(name, func(t *testing.T) {
			e := newTestEngine(t)
			base := filepath.Join(t.TempDir(), "project")
			selected := base + " \t"
			if err := os.Mkdir(selected, 0o700); err != nil {
				t.Fatal(err)
			}
			if siblingExists {
				if err := os.Mkdir(base, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			p, err := e.CreateProject("Selected", "", selected, false)
			if err != nil {
				t.Fatalf("a selected existing directory was refused: %v", err)
			}
			if p.Workdir != selected || e.ProjectWorkdir(p) != selected {
				t.Fatalf("selected %q became saved=%q resolved=%q", selected, p.Workdir, e.ProjectWorkdir(p))
			}
			th, err := e.CreateThread("Selected workspace", "", p.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got := e.WorkspaceDir(th.ID); got != selected {
				t.Fatalf("conversation works in %q instead of selected %q", got, selected)
			}
			managed, err := e.CreateProject("Managed", "", "", false)
			if err != nil {
				t.Fatal(err)
			}
			updated, err := e.UpdateProject(managed.ID, nil, nil, &selected, nil)
			if err != nil {
				t.Fatalf("editing to the selected directory failed: %v", err)
			}
			stored, err := e.GetProject(updated.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Workdir != selected || e.ProjectWorkdir(stored) != selected {
				t.Fatalf("edit lost the directory identity: saved=%q resolved=%q", stored.Workdir, e.ProjectWorkdir(stored))
			}
		})
	}
}

func TestMissingSelectedDirectoryNeverFallsBackToItsTrimmedSibling(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("Windows aliases trailing spaces instead of exposing a distinct sibling")
	}
	e := newTestEngine(t)
	sibling := t.TempDir()
	missing := sibling + " "
	if _, err := e.CreateProject("Missing", "", missing, false); !errors.Is(err, ErrInvalidWorkdir) {
		t.Fatalf("missing selection silently became its sibling: %v", err)
	}
	p, err := e.CreateProject("Unchanged", "", sibling, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.UpdateProject(p.ID, nil, nil, &missing, nil); !errors.Is(err, ErrInvalidWorkdir) {
		t.Fatalf("missing edit silently became its sibling: %v", err)
	}
	stored, err := e.GetProject(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Workdir != sibling {
		t.Fatalf("a refused edit changed the prior directory: %q", stored.Workdir)
	}
}

func TestBlankWorkingDirectoryStillUsesTheManagedWorkspace(t *testing.T) {
	e := newTestEngine(t)
	p, err := e.CreateProject("Managed", "", " \t\n", false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Workdir != "" || e.ProjectWorkdir(p) != e.Config().ProjectWorkspaceDir(p.ID) {
		t.Fatalf("blank directory no longer resolves to the managed workspace: %+v", p)
	}
	// Legacy all-whitespace stored values also retain their managed semantics.
	p.Workdir = " \t"
	if e.ProjectWorkdir(p) != e.Config().ProjectWorkspaceDir(p.ID) {
		t.Fatal("legacy blank workspace stopped using the managed directory")
	}
	if _, err := normalizeWorkdir(" " + t.TempDir()); !errors.Is(err, ErrInvalidWorkdir) {
		t.Fatalf("non-absolute input was guessed into a different path: %v", err)
	}
}
