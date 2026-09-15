package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/gin-gonic/gin"
)

type projectView struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	SystemPrompt string `json:"system_prompt"`
	// Workdir is what the user chose, empty when zwai manages it.
	Workdir string `json:"workdir"`
	// ResolvedWorkdir is where the agents actually work, so the UI can show
	// the managed path without knowing how it is derived.
	ResolvedWorkdir string `json:"resolved_workdir"`
	MemoryEnabled   bool   `json:"memory_enabled"`
	MemoryDir       string `json:"memory_dir"`
	// Skills is the index the sidebar lists under the project: names and
	// one-line descriptions, not the bodies. An unreadable store is an empty
	// list rather than a failed listing — hiding every project because one
	// memory directory is broken would be the worse failure.
	Skills    []memory.SkillInfo `json:"skills"`
	CreatedAt string             `json:"created_at"`
	UpdatedAt string             `json:"updated_at"`
}

func (s *Server) viewProject(p *store.Project) projectView {
	return projectView{
		ID:              p.ID,
		Name:            p.Name,
		SystemPrompt:    p.SystemPrompt,
		Workdir:         p.Workdir,
		ResolvedWorkdir: s.engine.ProjectWorkdir(p),
		MemoryEnabled:   p.MemoryEnabled,
		MemoryDir:       s.engine.ProjectMemory(p.ID).Dir(),
		Skills:          s.projectSkills(p.ID),
		CreatedAt:       p.CreatedAt.Format(timeFormat),
		UpdatedAt:       p.UpdatedAt.Format(timeFormat),
	}
}

func (s *Server) projectSkills(projectID string) []memory.SkillInfo {
	list, err := s.engine.ProjectMemory(projectID).ListSkills()
	if err != nil || list == nil {
		return []memory.SkillInfo{}
	}
	return list
}

func (s *Server) listProjects(c *gin.Context) {
	list, err := s.engine.ListProjects()
	if err != nil {
		s.fail(c, err)
		return
	}
	out := make([]projectView, 0, len(list))
	for i := range list {
		out = append(out, s.viewProject(&list[i]))
	}
	c.JSON(http.StatusOK, gin.H{"projects": out})
}

type createProjectRequest struct {
	Name         string `json:"name"`
	SystemPrompt string `json:"system_prompt"`
	Workdir      string `json:"workdir"`
	// MemoryEnabled is a pointer so a client that does not mention memory
	// gets the configured default rather than silently opting out.
	MemoryEnabled *bool `json:"memory_enabled"`
}

func (s *Server) createProject(c *gin.Context) {
	var req createProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	enabled := s.engine.Config().Memory.Enabled
	if req.MemoryEnabled != nil {
		enabled = *req.MemoryEnabled
	}
	p, err := s.engine.CreateProject(req.Name, req.SystemPrompt, req.Workdir, enabled)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"project": s.viewProject(p)})
}

func (s *Server) getProject(c *gin.Context) {
	p, ok := s.project(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"project": s.viewProject(p)})
}

type patchProjectRequest struct {
	Name          *string `json:"name"`
	SystemPrompt  *string `json:"system_prompt"`
	Workdir       *string `json:"workdir"`
	MemoryEnabled *bool   `json:"memory_enabled"`
}

func (s *Server) patchProject(c *gin.Context) {
	if _, ok := s.project(c); !ok {
		return
	}
	var req patchProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	p, err := s.engine.UpdateProject(c.Param("id"), req.Name, req.SystemPrompt, req.Workdir, req.MemoryEnabled)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"project": s.viewProject(p)})
}

func (s *Server) deleteProject(c *gin.Context) {
	if _, ok := s.project(c); !ok {
		return
	}
	if err := s.engine.DeleteProject(c.Param("id")); err != nil {
		s.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ---------- memory ----------

type memoryView struct {
	Dir     string             `json:"dir"`
	Enabled bool               `json:"enabled"`
	Memory  memory.Snapshot    `json:"memory"`
	Skills  []memory.SkillInfo `json:"skills"`
}

func (s *Server) getMemory(c *gin.Context) {
	p, ok := s.project(c)
	if !ok {
		return
	}
	mem := s.engine.ProjectMemory(p.ID)
	snap, err := mem.Read()
	if err != nil {
		s.fail(c, err)
		return
	}
	skills, err := mem.ListSkills()
	if err != nil {
		s.fail(c, err)
		return
	}
	if skills == nil {
		// The panel iterates this, so it must never arrive as JSON null.
		skills = []memory.SkillInfo{}
	}
	c.JSON(http.StatusOK, gin.H{"memory": memoryView{
		Dir:     mem.Dir(),
		Enabled: s.engine.MemoryEnabled(p),
		Memory:  snap,
		Skills:  skills,
	}})
}

type putMemoryRequest struct {
	Text string `json:"text"`
	// Rev is the snapshot the editor loaded. A write against a stale one is
	// refused rather than applied over a review that landed in between.
	Rev string `json:"rev"`
}

// putMemory is the hand edit from the Memory panel. The character limit still
// applies: a store that no longer fits in a prompt is the same problem
// whoever typed it.
func (s *Server) putMemory(c *gin.Context) {
	p, ok := s.project(c)
	if !ok {
		return
	}
	var req putMemoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	snap, err := s.engine.ProjectMemory(p.ID).OverwriteIf(req.Rev, req.Text)
	var conflict *memory.ConflictError
	if errors.As(err, &conflict) {
		// 409 rather than 400: the request was fine, the world moved. The
		// current notes come back on the same body so the panel can show
		// both without a second round trip that could itself be stale.
		c.JSON(http.StatusConflict, gin.H{
			"error":  conflict.Error(),
			"code":   "conflict",
			"memory": conflict.Current,
		})
		return
	}
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"memory": snap})
}

func (s *Server) getSkill(c *gin.Context) {
	p, ok := s.project(c)
	if !ok {
		return
	}
	skill, err := s.engine.ProjectMemory(p.ID).ReadSkill(c.Param("name"))
	if err != nil {
		s.failSkill(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"skill": skill})
}

func (s *Server) deleteSkill(c *gin.Context) {
	p, ok := s.project(c)
	if !ok {
		return
	}
	if err := s.engine.ProjectMemory(p.ID).DeleteSkill(c.Param("name")); err != nil {
		s.failSkill(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// failSkill maps the store's "nothing matches" to a 404. A skill name arrives
// in the path, so a name nobody wrote is a missing resource rather than a bad
// request — and a name that could never be one is the opposite.
func (s *Server) failSkill(c *gin.Context, err error) {
	switch {
	case errors.Is(err, memory.ErrNoMatch):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, memory.ErrBadName):
		badRequest(c, "%s", err.Error())
	default:
		s.fail(c, err)
	}
}

// ---------- on-demand review ----------

// reviewThread reviews a conversation's most recent completed turn. It answers
// with the turn it is reviewing rather than the outcome: the review is a
// background job and its result arrives on the event stream, the same way a
// turn's answer does.
func (s *Server) reviewThread(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	turn, err := s.engine.ReviewTurn(th.ID)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"turn": turn})
}

// project loads the project named in the path, answering 404 itself so every
// handler does not repeat the check.
func (s *Server) project(c *gin.Context) (*store.Project, bool) {
	p, err := s.engine.GetProject(strings.TrimSpace(c.Param("id")))
	if err != nil {
		s.fail(c, err)
		return nil, false
	}
	return p, true
}
