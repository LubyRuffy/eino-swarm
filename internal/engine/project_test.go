package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestCreatingAProjectPreparesItsDirectories(t *testing.T) {
	e := newTestEngine(t)
	p, err := e.CreateProject("Work", "the project's own instruction", "", true)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.ID == "" || p.Name != "Work" {
		t.Fatalf("project=%+v", p)
	}
	// A project with no working directory of its own gets one zwai manages, so
	// it is usable before anyone has a path in mind.
	workdir := e.ProjectWorkdir(p)
	if workdir != e.Config().ProjectWorkspaceDir(p.ID) {
		t.Fatalf("workdir=%q", workdir)
	}
	for _, dir := range []string{workdir, e.Config().ProjectMemoryDir(p.ID)} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("%q was not prepared: %v", dir, err)
		}
	}
	if !e.MemoryEnabled(p) {
		t.Fatal("a project created with memory on must have it on")
	}

	if _, err := e.CreateProject("  ", "", "", true); err == nil {
		t.Fatal("a project needs a name")
	}
}

// A working directory the user typed has to exist. Creating it would be worse
// than refusing: a typo is how an agent ends up working in an empty directory
// that looks like the right one, ten minutes into a turn.
func TestAWorkingDirectoryMustBeAnExistingAbsolutePath(t *testing.T) {
	e := newTestEngine(t)
	real := t.TempDir()
	file := filepath.Join(real, "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, dir := range map[string]string{
		"relative":     "relative/path",
		"not there":    filepath.Join(real, "does-not-exist"),
		"not a folder": file,
	} {
		if _, err := e.CreateProject("P", "", dir, true); !errors.Is(err, ErrInvalidWorkdir) {
			t.Fatalf("%s: err=%v, want ErrInvalidWorkdir", name, err)
		}
	}

	p, err := e.CreateProject("P", "", real+string(filepath.Separator), true)
	if err != nil {
		t.Fatalf("an existing absolute path must be accepted: %v", err)
	}
	if p.Workdir != real {
		t.Fatalf("workdir not cleaned: %q want %q", p.Workdir, real)
	}
	// zwai did not create this directory, so it must not have made a workspace
	// inside the project's own directory either.
	if _, err := os.Stat(e.Config().ProjectWorkspaceDir(p.ID)); !os.IsNotExist(err) {
		t.Fatalf("a managed workspace was created for a project that named its own: %v", err)
	}
}

func TestUpdatingAProjectChangesOnlyWhatWasSent(t *testing.T) {
	e := newTestEngine(t)
	p, err := e.CreateProject("Before", "first instruction", "", true)
	if err != nil {
		t.Fatal(err)
	}
	name, prompt := "After", "second instruction"
	off := false
	updated, err := e.UpdateProject(p.ID, &name, &prompt, nil, &off)
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if updated.Name != "After" || updated.SystemPrompt != "second instruction" || updated.MemoryEnabled {
		t.Fatalf("update=%+v", updated)
	}
	if updated.Workdir != "" {
		t.Fatalf("a field nobody sent was changed: %+v", updated)
	}

	// Naming a real directory moves the project's work there.
	real := t.TempDir()
	if updated, err = e.UpdateProject(p.ID, nil, nil, &real, nil); err != nil {
		t.Fatalf("UpdateProject(workdir): %v", err)
	}
	if e.ProjectWorkdir(updated) != real {
		t.Fatalf("workdir=%q", e.ProjectWorkdir(updated))
	}
	// And clearing it puts the work back in the managed one.
	blank := ""
	if updated, err = e.UpdateProject(p.ID, nil, nil, &blank, nil); err != nil {
		t.Fatalf("UpdateProject(clear workdir): %v", err)
	}
	if e.ProjectWorkdir(updated) != e.Config().ProjectWorkspaceDir(p.ID) {
		t.Fatalf("workdir=%q", e.ProjectWorkdir(updated))
	}
	if _, err := os.Stat(e.Config().ProjectWorkspaceDir(p.ID)); err != nil {
		t.Fatalf("clearing the working directory must prepare the managed one: %v", err)
	}

	empty := "  "
	if _, err := e.UpdateProject(p.ID, &empty, nil, nil, nil); err == nil {
		t.Fatal("a project needs a name")
	}
	bad := "not/absolute"
	if _, err := e.UpdateProject(p.ID, nil, nil, &bad, nil); !errors.Is(err, ErrInvalidWorkdir) {
		t.Fatalf("UpdateProject(bad workdir) err=%v", err)
	}
	if _, err := e.UpdateProject("pj_missing", &name, nil, nil, nil); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("UpdateProject(unknown) err=%v", err)
	}
}

// The point of a project is that its conversations share a directory: a second
// conversation has to be able to read what the first one wrote.
func TestConversationsInAProjectShareItsDirectory(t *testing.T) {
	e := newTestEngine(t)
	shared := t.TempDir()
	p, err := e.CreateProject("P", "", shared, true)
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.CreateThread("one", "", p.ID)
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	second, err := e.CreateThread("two", "", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	loose, err := e.CreateThread("loose", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if e.WorkspaceDir(first.ID) != shared || e.WorkspaceDir(second.ID) != shared {
		t.Fatalf("project conversations do not share a directory: %q / %q",
			e.WorkspaceDir(first.ID), e.WorkspaceDir(second.ID))
	}
	if e.WorkspaceDir(loose.ID) != e.Config().WorkspaceDir(loose.ID) {
		t.Fatalf("a conversation in no project must keep its own workspace: %q", e.WorkspaceDir(loose.ID))
	}
	if _, err := e.CreateThread("x", "", "pj_missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("CreateThread in an unknown project err=%v", err)
	}
}

// The one that must never regress: a project's working directory is often the
// user's own repository, shared with every other conversation in the project.
// Deleting one conversation must not take it.
func TestDeletingAProjectConversationLeavesTheDirectoryAlone(t *testing.T) {
	e := newTestEngine(t)
	repo := t.TempDir()
	marker := filepath.Join(repo, "the-users-file")
	if err := os.WriteFile(marker, []byte("precious"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := e.CreateProject("P", "", repo, true)
	if err != nil {
		t.Fatal(err)
	}
	th, err := e.CreateThread("one", "", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.DeleteThread(th.ID); err != nil {
		t.Fatalf("DeleteThread: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("deleting a conversation deleted the user's files: %v", err)
	}

	// The same for a project whose directory zwai manages: it is still shared.
	managed, err := e.CreateProject("Managed", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	other, err := e.CreateThread("two", "", managed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.DeleteThread(other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(e.Config().ProjectWorkspaceDir(managed.ID)); err != nil {
		t.Fatalf("deleting a conversation removed the project's shared directory: %v", err)
	}
}

// Deleting the project itself does take what zwai created — and still not what
// the user pointed at.
func TestDeletingAProjectRemovesWhatZwaiCreatedAndNothingElse(t *testing.T) {
	e := newTestEngine(t)
	repo := t.TempDir()
	marker := filepath.Join(repo, "the-users-file")
	if err := os.WriteFile(marker, []byte("precious"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := e.CreateProject("P", "", repo, true)
	if err != nil {
		t.Fatal(err)
	}
	th, err := e.CreateThread("one", "", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.ProjectMemory(p.ID).Add("a note"); err != nil {
		t.Fatal(err)
	}

	if err := e.DeleteProject(p.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if _, err := e.Store().GetThread(th.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the project's conversation survived: %v", err)
	}
	if _, err := os.Stat(e.Config().ProjectDir(p.ID)); !os.IsNotExist(err) {
		t.Fatalf("the project's own directory survived: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("deleting a project deleted the user's files: %v", err)
	}
	// The cached memory store must go too, or a new project reusing the id
	// would inherit the old one's lock and limit.
	if snap, _ := e.ProjectMemory(p.ID).Read(); len(snap.Entries) != 0 {
		t.Fatalf("a deleted project's notes came back: %+v", snap.Entries)
	}
	if err := e.DeleteProject("pj_missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("DeleteProject(unknown) err=%v", err)
	}
}

func TestMovingAConversationBetweenProjects(t *testing.T) {
	e := newTestEngine(t)
	p, err := e.CreateProject("P", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th, err := e.CreateThread("t", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.MoveThread(th.ID, p.ID); err != nil {
		t.Fatalf("MoveThread: %v", err)
	}
	if got := e.WorkspaceDir(th.ID); got != e.Config().ProjectWorkspaceDir(p.ID) {
		t.Fatalf("a moved conversation must work in its new project: %q", got)
	}
	if err := e.MoveThread(th.ID, ""); err != nil {
		t.Fatalf("MoveThread(out): %v", err)
	}
	if got := e.WorkspaceDir(th.ID); got != e.Config().WorkspaceDir(th.ID) {
		t.Fatalf("a conversation taken out of a project must go back to its own workspace: %q", got)
	}
	if err := e.MoveThread(th.ID, "pj_missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("MoveThread(unknown project) err=%v", err)
	}
	if err := e.MoveThread("th_missing", p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("MoveThread(unknown conversation) err=%v", err)
	}
}

// A conversation whose project has gone must still run: falling back to its own
// workspace is a worse answer than the project's directory and a much better
// one than anchoring the tools at the process's working directory.
func TestAConversationOutlivesAMissingProject(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("t", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// A row left pointing at a project that is gone: the state a database
	// restored from a partial backup, or an interrupted delete, leaves behind.
	if err := e.Store().SetThreadProject(th.ID, "pj_vanished"); err != nil {
		t.Fatal(err)
	}
	if got := e.WorkspaceDir(th.ID); got != e.Config().WorkspaceDir(th.ID) {
		t.Fatalf("workspace=%q", got)
	}
	reloaded, err := e.Store().GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := e.projectContextFor(reloaded)
	if err != nil {
		t.Fatalf("projectContextFor: %v", err)
	}
	if pc != nil {
		t.Fatalf("a missing project must contribute nothing to the prompt: %+v", pc)
	}
}

// The project's instruction and its notes have to reach the model, and the
// memory tools have to reach the manager — but not the sub-agents, which see
// one task and are in no position to judge what is worth remembering.
func TestProjectPromptAndMemoryReachTheManagerOnly(t *testing.T) {
	e := newTestEngine(t)
	p, err := e.CreateProject("P", "the project's own instruction", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.ProjectMemory(p.ID).Add("a note from an earlier conversation"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ProjectMemory(p.ID).WriteSkill("a-procedure", "when it applies", "steps"); err != nil {
		t.Fatal(err)
	}
	th, err := e.CreateThread("t", "", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := e.projectContextFor(th)
	if err != nil {
		t.Fatalf("projectContextFor: %v", err)
	}
	for _, want := range []string{
		"the project's own instruction",
		"a note from an earlier conversation",
		"a-procedure",
		"when it applies",
	} {
		if !strings.Contains(pc.promptSections(), want) {
			t.Fatalf("%q missing from the project's prompt sections:\n%s", want, pc.promptSections())
		}
	}

	toolset := buildTestToolset(t, e, th.ID)
	managerTools := pc.managerTools(toolset)
	if len(managerTools) != len(toolset.Tools)+len(memory.Names()) {
		t.Fatalf("the manager got %d tools, want the workspace toolset plus %d memory tools",
			len(managerTools), len(memory.Names()))
	}
	// The sub-agents' slice must not have grown: it shares a backing array
	// with the manager's, so appending in place would hand them the memory
	// tools too.
	if len(toolset.Tools) == len(managerTools) {
		t.Fatal("the sub-agent toolset was modified in place")
	}

	// With memory off, the instruction survives and the tools do not: a prompt
	// that advertises a tool the agent does not have produces failed calls.
	off := false
	if _, err := e.UpdateProject(p.ID, nil, nil, nil, &off); err != nil {
		t.Fatal(err)
	}
	pc, err = e.projectContextFor(th)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pc.promptSections(), "the project's own instruction") {
		t.Fatalf("the instruction is not part of memory and must survive:\n%s", pc.promptSections())
	}
	if strings.Contains(pc.promptSections(), "a note from an earlier conversation") {
		t.Fatalf("memory is off but its notes are still in the prompt:\n%s", pc.promptSections())
	}
	if pc.memoryLive() || len(pc.managerTools(toolset)) != len(toolset.Tools) {
		t.Fatal("memory is off but the manager still got the memory tools")
	}
}

// A conversation in no project contributes nothing, which is what keeps every
// existing conversation working exactly as before.
func TestAConversationInNoProjectAddsNothingToThePrompt(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("t", "", "")
	if err != nil {
		t.Fatal(err)
	}
	pc, err := e.projectContextFor(th)
	if err != nil {
		t.Fatalf("projectContextFor: %v", err)
	}
	if pc != nil || pc.promptSections() != "" || pc.memoryLive() {
		t.Fatalf("pc=%+v", pc)
	}
	toolset := buildTestToolset(t, e, th.ID)
	if len(pc.managerTools(toolset)) != len(toolset.Tools) {
		t.Fatal("a conversation in no project must get exactly the workspace toolset")
	}
}

// The store instance is shared per project on purpose: its lock is what keeps
// a turn's manager and the previous turn's review from losing each other's
// notes, and two instances over one directory have two locks and no protection.
func TestProjectMemoryIsOneSharedStore(t *testing.T) {
	e := newTestEngine(t)
	p, err := e.CreateProject("P", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if e.ProjectMemory(p.ID) != e.ProjectMemory(p.ID) {
		t.Fatal("two callers got two stores over the same directory")
	}
	if e.ProjectMemory(p.ID).Limit() != e.Config().Memory.Limit() {
		t.Fatalf("limit=%d", e.ProjectMemory(p.ID).Limit())
	}
	if e.ProjectMemory(p.ID).Dir() != e.Config().ProjectMemoryDir(p.ID) {
		t.Fatalf("dir=%q", e.ProjectMemory(p.ID).Dir())
	}
}

func TestProjectsAreListedForTheSidebar(t *testing.T) {
	e := newTestEngine(t)
	for _, name := range []string{"first", "second"} {
		if _, err := e.CreateProject(name, "", "", true); err != nil {
			t.Fatal(err)
		}
	}
	list, err := e.ListProjects()
	if err != nil || len(list) != 2 {
		t.Fatalf("ListProjects=%d err=%v", len(list), err)
	}
	got, err := e.GetProject(list[0].ID)
	if err != nil || got.ID != list[0].ID {
		t.Fatalf("GetProject=%+v err=%v", got, err)
	}
	if _, err := e.GetProject("pj_missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetProject(unknown) err=%v", err)
	}
}

// A project whose directories could not be created would fail every turn, so
// it must not be left half-made in the database.
func TestAProjectThatCannotBePreparedIsNotCreated(t *testing.T) {
	e := newTestEngine(t)
	// A regular file where the projects directory belongs: the readable
	// stand-in for a data directory the process cannot write to.
	if err := os.RemoveAll(e.Config().ProjectsDir()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.Config().ProjectsDir(), []byte("in the way"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.CreateProject("P", "", "", true); err == nil {
		t.Fatal("want an error when a project's directories cannot be created")
	}
	list, err := e.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("a half-made project was left behind: %+v", list)
	}
}

// Memory the process cannot read must not cost the user a turn: run without
// it rather than refusing to answer.
func TestAnUnreadableMemoryStoreStillLetsTheTurnRun(t *testing.T) {
	e := newTestEngine(t)
	p, err := e.CreateProject("P", "an instruction", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th, err := e.CreateThread("t", "", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(e.Config().ProjectMemoryDir(p.ID), memory.MemoryFile), 0o700); err != nil {
		t.Fatal(err)
	}
	pc, err := e.projectContextFor(th)
	if err != nil {
		t.Fatalf("projectContextFor: %v", err)
	}
	if !strings.Contains(pc.promptSections(), "an instruction") {
		t.Fatalf("the project's instruction must survive unreadable memory:\n%s", pc.promptSections())
	}
	if pc.memoryLive() {
		t.Fatal("memory that cannot be read must not be offered as a tool")
	}
}

// A skills directory that cannot be listed costs the index, not the turn.
func TestAnUnreadableSkillsDirectoryStillLetsTheTurnRun(t *testing.T) {
	e := newTestEngine(t)
	p, err := e.CreateProject("P", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	th, err := e.CreateThread("t", "", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.Config().ProjectMemoryDir(p.ID), memory.SkillsDir),
		[]byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	pc, err := e.projectContextFor(th)
	if err != nil {
		t.Fatalf("projectContextFor: %v", err)
	}
	if !pc.memoryLive() {
		t.Fatal("unreadable skills must not take the notes with them")
	}
	if !strings.Contains(pc.promptSections(), "No skills recorded yet") {
		t.Fatalf("sections:\n%s", pc.promptSections())
	}
}

// The global switch is the operator's and the project's is the user's; memory
// runs only when both agree.
func TestMemoryNeedsBothSwitchesOn(t *testing.T) {
	e := newTestEngine(t)
	on := &store.Project{MemoryEnabled: true}
	if !e.MemoryEnabled(on) {
		t.Fatal("both switches on must mean memory is on")
	}
	if e.MemoryEnabled(&store.Project{MemoryEnabled: false}) {
		t.Fatal("a project that opted out must stay out")
	}
	if e.MemoryEnabled(nil) {
		t.Fatal("no project, no memory")
	}
	e.Config().Memory.Enabled = false
	if e.MemoryEnabled(on) {
		t.Fatal("the global switch must be able to turn memory off everywhere")
	}
}
