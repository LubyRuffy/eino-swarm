package engine

import (
	"errors"
	"os"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestTerminalDirFollowsTheProjectNotTheScratchWorkspace(t *testing.T) {
	e := newTestEngine(t)
	shared := t.TempDir()
	p, err := e.CreateProject("P", "", shared, true)
	if err != nil {
		t.Fatal(err)
	}
	th, err := e.CreateThread("one", "", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.TerminalDir(th.ID, "")
	if err != nil {
		t.Fatalf("TerminalDir: %v", err)
	}
	if got != shared {
		t.Fatalf("terminal cwd=%q, want the project directory %q", got, shared)
	}
	// A client that also names a different project must not win: the open
	// conversation is where the agents are working.
	other, err := e.CreateProject("Other", "", t.TempDir(), true)
	if err != nil {
		t.Fatal(err)
	}
	got, err = e.TerminalDir(th.ID, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != shared {
		t.Fatalf("a thread target must ignore a spare project id: %q", got)
	}
}

func TestTerminalDirUsesTheConversationWorkspaceWithoutAProject(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("loose", "", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.TerminalDir(th.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	want := e.Config().WorkspaceDir(th.ID)
	if got != want {
		t.Fatalf("cwd=%q want=%q", got, want)
	}
}

func TestTerminalDirUsesTheSelectedProjectWhenThereIsNoConversation(t *testing.T) {
	e := newTestEngine(t)
	shared := t.TempDir()
	p, err := e.CreateProject("P", "", shared, true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.TerminalDir("", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != shared {
		t.Fatalf("cwd=%q want=%q", got, shared)
	}
}

func TestTerminalDirRefusesNothingAndUnknownIds(t *testing.T) {
	e := newTestEngine(t)
	if _, err := e.TerminalDir("", ""); err == nil {
		t.Fatal("empty target must be refused")
	}
	if _, err := e.TerminalDir("th_missing", ""); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing thread err=%v", err)
	}
	if _, err := e.TerminalDir("", "pj_missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing project err=%v", err)
	}
}

func TestTerminalDirRebuildsAMissingScratchWorkspace(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("loose", "", "")
	if err != nil {
		t.Fatal(err)
	}
	dir := e.WorkspaceDir(th.ID)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	got, err := e.TerminalDir(th.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Fatalf("cwd=%q want=%q", got, dir)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("scratch workspace was not rebuilt: %v", err)
	}
}

func TestTerminalDirDoesNotInventAMissingUserWorkdir(t *testing.T) {
	e := newTestEngine(t)
	shared := t.TempDir()
	p, err := e.CreateProject("P", "", shared, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(shared); err != nil {
		t.Fatal(err)
	}
	if _, err := e.TerminalDir("", p.ID); err == nil {
		t.Fatal("a vanished user workdir must be refused, not recreated")
	}
	if _, err := os.Stat(shared); !os.IsNotExist(err) {
		t.Fatalf("refusing must not mkdir the typed path: %v", err)
	}
}

func TestTerminalDirRejectsAFileWhereADirectoryShouldBe(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("loose", "", "")
	if err != nil {
		t.Fatal(err)
	}
	dir := e.WorkspaceDir(th.ID)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.TerminalDir(th.ID, ""); err == nil {
		t.Fatal("a file must not count as a working directory")
	}
}

func TestTerminalDirManagedProjectWorkspace(t *testing.T) {
	e := newTestEngine(t)
	p, err := e.CreateProject("Managed", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.TerminalDir("", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := e.Config().ProjectWorkspaceDir(p.ID)
	if got != want {
		t.Fatalf("cwd=%q want=%q", got, want)
	}
}
