package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/components/tool"
)

// ErrInvalidWorkdir means the working directory a project was given cannot be
// used. It is its own error so the HTTP layer can point the settings field at
// it instead of showing a generic failure next to the wrong control.
var ErrInvalidWorkdir = errors.New("engine: that working directory cannot be used")

// projectMemory caches one memory.Store per project.
//
// Sharing the instance is not an optimization: the store's mutex is what keeps
// a turn's manager and the review of the previous turn from losing each other's
// entries, and two instances over one directory have two mutexes and no
// protection at all.
type projectMemory struct {
	mu     sync.Mutex
	stores map[string]*memory.Store
}

// CreateProject opens a new project and prepares the directories it needs.
func (e *Engine) CreateProject(name, systemPrompt, workdir string, memoryEnabled bool) (*store.Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("engine: a project needs a name")
	}
	workdir, err := normalizeWorkdir(workdir)
	if err != nil {
		return nil, err
	}
	p := &store.Project{
		Name:          name,
		SystemPrompt:  strings.TrimSpace(systemPrompt),
		Workdir:       workdir,
		MemoryEnabled: memoryEnabled,
	}
	if err := e.store.CreateProject(p); err != nil {
		return nil, err
	}
	if err := e.prepareProjectDirs(p); err != nil {
		// A project whose directories could not be created would fail every
		// turn, so it does not get to exist half-made.
		_ = e.store.DeleteProject(p.ID)
		return nil, err
	}
	return p, nil
}

// UpdateProject applies the fields the caller set. Each is a pointer so
// clearing the working directory (back to the managed one) is a different
// request from leaving it alone.
func (e *Engine) UpdateProject(id string, name, systemPrompt, workdir *string, memoryEnabled *bool) (*store.Project, error) {
	if _, err := e.store.GetProject(id); err != nil {
		return nil, err
	}
	fields := map[string]any{}
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed == "" {
			return nil, fmt.Errorf("engine: a project needs a name")
		}
		fields["name"] = trimmed
	}
	if systemPrompt != nil {
		fields["system_prompt"] = strings.TrimSpace(*systemPrompt)
	}
	if workdir != nil {
		normalized, err := normalizeWorkdir(*workdir)
		if err != nil {
			return nil, err
		}
		fields["workdir"] = normalized
	}
	if memoryEnabled != nil {
		fields["memory_enabled"] = *memoryEnabled
	}
	if err := e.store.UpdateProject(id, fields); err != nil {
		return nil, err
	}
	updated, err := e.store.GetProject(id)
	if err != nil {
		return nil, err
	}
	if err := e.prepareProjectDirs(updated); err != nil {
		return nil, err
	}
	// The next turn resolves its workspace from the project, so a changed
	// working directory takes effect without a restart. Nothing to invalidate
	// beyond the memory store, which is keyed by project and not by path.
	return updated, nil
}

// DeleteProject removes a project, its conversations and the directories zwai
// created for it.
//
// A working directory the user chose is left alone. It is their code; a
// feature that can delete it because a project was tidied up is a feature
// nobody can risk using.
func (e *Engine) DeleteProject(id string) error {
	p, err := e.store.GetProject(id)
	if err != nil {
		return err
	}
	threadIDs, err := e.store.ListThreadIDsByProject(id)
	if err != nil {
		return err
	}
	for _, tid := range threadIDs {
		if err := e.Interrupt(tid); err != nil && !errors.Is(err, ErrIdle) && !errors.Is(err, ErrNotFound) {
			return err
		}
		e.closeRuntime(tid)
	}
	if err := e.store.DeleteProject(id); err != nil {
		return err
	}
	for _, tid := range threadIDs {
		e.dropSubscribers(tid)
	}
	e.forgetProjectMemory(id)
	dir := e.cfg.ProjectDir(p.ID)
	if err := os.RemoveAll(dir); err != nil {
		e.log.Warn("could not remove a project directory", "project", p.ID, "path", dir, "err", err)
	}
	return nil
}

// ListProjects returns every project for the sidebar.
func (e *Engine) ListProjects() ([]store.Project, error) { return e.store.ListProjects() }

// GetProject loads one project.
func (e *Engine) GetProject(id string) (*store.Project, error) { return e.store.GetProject(id) }

// MoveThread puts a conversation in a project, or takes it out of one. Its
// files do not move: the workspace it worked in is where its output already
// is, and silently relocating that would lose it.
func (e *Engine) MoveThread(threadID, projectID string) error {
	if _, err := e.store.GetThread(threadID); err != nil {
		return err
	}
	if projectID = strings.TrimSpace(projectID); projectID != "" {
		if _, err := e.store.GetProject(projectID); err != nil {
			return err
		}
	}
	return e.store.SetThreadProject(threadID, projectID)
}

// ProjectWorkdir is where a project's conversations work: the directory the
// user named, or the one zwai manages for it.
func (e *Engine) ProjectWorkdir(p *store.Project) string {
	if dir := strings.TrimSpace(p.Workdir); dir != "" {
		return dir
	}
	return e.cfg.ProjectWorkspaceDir(p.ID)
}

// ProjectMemory returns the shared memory store for a project, whether or not
// that project has memory switched on — the Memory panel still reads it, so a
// project that was switched off does not look like it lost its notes.
func (e *Engine) ProjectMemory(projectID string) *memory.Store {
	e.memory.mu.Lock()
	defer e.memory.mu.Unlock()
	if s, ok := e.memory.stores[projectID]; ok {
		return s
	}
	s := memory.New(e.cfg.ProjectMemoryDir(projectID), e.cfg.Memory.Limit())
	e.memory.stores[projectID] = s
	return s
}

func (e *Engine) forgetProjectMemory(projectID string) {
	e.memory.mu.Lock()
	delete(e.memory.stores, projectID)
	e.memory.mu.Unlock()
}

// MemoryEnabled reports whether a project's memory is live. Both switches have
// to be on: the global one is the operator's, the project's is the user's.
func (e *Engine) MemoryEnabled(p *store.Project) bool {
	return p != nil && p.MemoryEnabled && e.cfg.Memory.Enabled
}

// projectContext is what a project contributes to one turn: the extra prompt
// sections and, when memory is on, the tools that maintain them.
//
// It is built once at the start of a turn. The memory block is therefore a
// snapshot: writes during the turn land on disk and show up in tool results,
// but the prompt's prefix stays stable so the provider's cache keeps hitting.
type projectContext struct {
	project  *store.Project
	memory   *memory.Store
	sections string
	tools    []tool.BaseTool
	changes  func(memory.Change)
}

// promptSections is what this project adds to the manager's prompt. A nil
// context adds nothing, which is what a conversation in no project gets.
func (pc *projectContext) promptSections() string {
	if pc == nil {
		return ""
	}
	return pc.sections
}

// managerTools gives the manager the memory tools on top of the workspace
// toolset.
//
// Only the manager: a sub-agent sees one task and none of the conversation, so
// it is in no position to judge what is worth remembering — and five workers
// curating one bounded store at once is how it fills with near-duplicates.
func (pc *projectContext) managerTools(set *tools.Set) []tool.BaseTool {
	if pc == nil || len(pc.tools) == 0 {
		return set.Tools
	}
	// A fresh slice: the same backing array is registered on the sub-agents as
	// Registry.SubAgentTools, and appending in place would hand them the
	// memory tools too.
	out := make([]tool.BaseTool, 0, len(set.Tools)+len(pc.tools))
	out = append(out, set.Tools...)
	return append(out, pc.tools...)
}

// memoryLive reports whether this turn's project has a usable memory store.
func (pc *projectContext) memoryLive() bool {
	return pc != nil && pc.memory != nil
}

// projectContextFor assembles a turn's project contribution, or nil when the
// conversation belongs to no project.
func (e *Engine) projectContextFor(th *store.Thread) (*projectContext, error) {
	if strings.TrimSpace(th.ProjectID) == "" {
		return nil, nil
	}
	p, err := e.store.GetProject(th.ProjectID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// The workspace already fell back to the per-conversation one; a
			// turn is more use than a refusal.
			e.log.Warn("conversation points at a project that is not there",
				"thread", th.ID, "project", th.ProjectID)
			return nil, nil
		}
		return nil, err
	}
	pc := &projectContext{project: p}
	var snap memory.Snapshot
	var skills []memory.SkillInfo
	if e.MemoryEnabled(p) {
		mem := e.ProjectMemory(p.ID)
		if snap, err = mem.Read(); err != nil {
			// Memory the process cannot read must not cost the user a turn:
			// run without it and say so in the log. pc.memory stays nil, so
			// nothing downstream offers a tool that would fail on every call.
			e.log.Warn("could not read a project's memory", "project", p.ID, "err", err)
		} else {
			pc.memory = mem
			pc.tools = memory.Tools(mem, nil)
			// An unreadable skills directory costs the index, not the notes.
			if skills, err = mem.ListSkills(); err != nil {
				e.log.Warn("could not list a project's skills", "project", p.ID, "err", err)
			}
		}
	}
	pc.sections = memory.PromptSections(p.SystemPrompt, snap, skills, e.cfg.Memory.IndexMax(), pc.memory != nil)
	return pc, nil
}

// prepareProjectDirs creates what a project needs before a turn runs in it.
// The tools refuse to start against a missing base directory, and a failure
// here is far easier to read than one per tool call.
func (e *Engine) prepareProjectDirs(p *store.Project) error {
	dirs := []string{e.cfg.ProjectMemoryDir(p.ID)}
	if strings.TrimSpace(p.Workdir) == "" {
		dirs = append(dirs, e.cfg.ProjectWorkspaceDir(p.ID))
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("engine: create %s: %w", d, err)
		}
	}
	return nil
}

// normalizeWorkdir checks a user-supplied working directory.
//
// It must be absolute and it must already exist. Creating it would be worse
// than refusing: a typo in a path is how an agent ends up working in an empty
// directory that looks like the right one, and finding that out ten minutes
// into a turn.
func normalizeWorkdir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", nil
	}
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("%w: %q is not an absolute path", ErrInvalidWorkdir, dir)
	}
	clean := filepath.Clean(dir)
	info, err := os.Stat(clean)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrInvalidWorkdir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: %q is not a directory", ErrInvalidWorkdir, clean)
	}
	return clean, nil
}
