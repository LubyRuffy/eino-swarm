package engine

import (
	"errors"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/skills"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestDeletingAProjectInvalidatesAndForgetsItsExternalCatalog(t *testing.T) {
	e := newTestEngine(t)
	p, err := e.CreateProject("External skill owner", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := e.ProjectExternalSkills(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if shared, err := e.ProjectExternalSkills(p.ID); err != nil || shared != catalog {
		t.Fatalf("project returned a different catalog: %p, %v", shared, err)
	}
	if err := e.DeleteProject(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.ListInstalledSkills(); !errors.Is(err, skills.ErrProjectDeleted) {
		t.Fatalf("previous catalog survived project deletion: %v", err)
	}
	if recreated, err := e.ProjectExternalSkills(p.ID); recreated != nil || !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("late lookup of a deleted catalog = %p, %v", recreated, err)
	}
	e.memory.mu.Lock()
	_, cached := e.memory.external[p.ID]
	e.memory.mu.Unlock()
	if cached {
		t.Fatal("late lookup restored a deleted project's cache entry")
	}
}
