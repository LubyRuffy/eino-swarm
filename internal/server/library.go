package server

import (
	"net/http"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/gin-gonic/gin"
)

// getLibrary is the shared skill library: procedures recorded from
// conversations that belong to no project. Notes are not part of it.
func (s *Server) getLibrary(c *gin.Context) {
	view, err := s.libraryView()
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"memory": view})
}

func (s *Server) libraryView() (memoryView, error) {
	mem := s.engine.LibraryMemory()
	snap, err := mem.Read()
	if err != nil {
		return memoryView{}, err
	}
	skills, err := mem.ListSkills()
	if err != nil {
		return memoryView{}, err
	}
	if skills == nil {
		skills = []memory.SkillInfo{}
	}
	skills = s.annotateOrigins(skills)
	families, err := mem.SkillFamilyNames()
	if err != nil {
		families = nil
	}
	return memoryView{
		Dir:       mem.Dir(),
		Enabled:   s.engine.Config().Memory.Enabled,
		Memory:    snap,
		Skills:    skills,
		NeedsTidy: len(families) > 0,
	}, nil
}

func (s *Server) getLibrarySkill(c *gin.Context) {
	skill, err := s.engine.LibraryMemory().ReadSkill(c.Param("name"))
	if err != nil {
		s.failSkill(c, err)
		return
	}
	annotated := s.annotateOrigins([]memory.SkillInfo{skill.SkillInfo})
	skill.SkillInfo = annotated[0]
	c.JSON(http.StatusOK, gin.H{"skill": skill})
}

func (s *Server) deleteLibrarySkill(c *gin.Context) {
	if err := s.engine.LibraryMemory().DeleteSkill(c.Param("name")); err != nil {
		s.failSkill(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) copyLibrarySkills(c *gin.Context) {
	s.copyCatalog(c, memory.LibraryID, s.engine.LibraryMemory())
}

func (s *Server) tidyLibrary(c *gin.Context) {
	if wantsEventStream(c) {
		s.streamCatalogTidy(c, func(watch func(engine.TidyEvent)) (memory.FoldReport, error) {
			return s.engine.FoldLibrarySkillsWatch(watch)
		}, s.libraryView)
		return
	}
	report, err := s.engine.FoldLibrarySkills()
	if err != nil {
		s.fail(c, err)
		return
	}
	view, err := s.libraryView()
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, tidyPayload(view, report))
}
