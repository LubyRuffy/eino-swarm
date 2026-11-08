package engine

import "github.com/LubyRuffy/eino-swarm/internal/skills"

// ProjectExternalSkills returns the independent external package catalog for a project.
func (e *Engine) ProjectExternalSkills(projectID string) (*skills.Store, error) {
	e.memory.mu.Lock()
	defer e.memory.mu.Unlock()
	// Keep the owner check with the cache lookup so a late request cannot
	// repopulate the cache after DeleteProject forgets this store.
	if _, err := e.store.GetProject(projectID); err != nil {
		return nil, err
	}
	if s := e.memory.external[projectID]; s != nil {
		return s, nil
	}
	s := skills.New(e.cfg.ProjectDir(projectID))
	if e.memory.external == nil {
		e.memory.external = make(map[string]*skills.Store)
	}
	e.memory.external[projectID] = s
	return s, nil
}

func (e *Engine) attachExternalSkills(pc *projectContext) {
	s, err := e.ProjectExternalSkills(pc.project.ID)
	if err != nil {
		e.log.Warn("could not access a project's external skills", "project", pc.project.ID, "err", err)
		return
	}
	items, err := s.ListInstalledSkills()
	if err != nil {
		e.log.Warn("could not list a project's external skills", "project", pc.project.ID, "err", err)
		return
	}
	if len(items) == 0 {
		return
	}
	index := skills.Prompt(items)
	pc.sections += index
	pc.workerText += index
	loader := s.Loader()
	pc.tools = append(pc.tools, loader)
	pc.viewTools = append(pc.viewTools, loader)
}
