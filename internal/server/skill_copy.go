package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/gin-gonic/gin"
)

type skillCopyHit struct {
	From      string `json:"from"`
	Name      string `json:"name"`
	ProjectID string `json:"project_id"`
}

type skillCopySkip struct {
	Name  string `json:"name"`
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

type copySkillsRequest struct {
	ToProject string   `json:"to_project"`
	Names     []string `json:"names"`
	As        string   `json:"as"`
}

// copySkills writes independent copies into another project. An empty names
// list copies the whole catalog. One named skill fails the request; a batch
// reports the ones that could not be written and still keeps the rest.
func (s *Server) copySkills(c *gin.Context) {
	src, ok := s.project(c)
	if !ok {
		return
	}
	s.copyCatalog(c, src.ID, s.engine.ProjectMemory(src.ID))
}

func (s *Server) copyCatalog(c *gin.Context, srcID string, src *memory.Store) {
	var req copySkillsRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	to := strings.TrimSpace(req.ToProject)
	if to == "" {
		badRequest(c, "choose a project to copy into")
		return
	}
	if to == srcID {
		badRequest(c, "choose a different project")
		return
	}
	dest, err := s.engine.GetProject(to)
	if err != nil {
		s.fail(c, err)
		return
	}
	names, explicit, err := skillNamesToCopy(src, req.Names)
	if err != nil {
		s.fail(c, err)
		return
	}
	if len(names) == 0 {
		if srcID == memory.LibraryID {
			badRequest(c, "the shared library has no skills to copy")
			return
		}
		badRequest(c, "this project has no skills to copy")
		return
	}
	as := strings.TrimSpace(req.As)
	if as != "" && len(names) != 1 {
		badRequest(c, "a new name applies to one skill")
		return
	}
	if as != "" {
		if err := memory.ValidSkillName(as); err != nil {
			s.failSkill(c, err)
			return
		}
	}

	single := explicit && len(names) == 1
	dst := s.engine.ProjectMemory(dest.ID)
	copied := []skillCopyHit{}
	skipped := []skillCopySkip{}
	for _, name := range names {
		target := name
		if as != "" {
			target = as
		}
		hit, err := s.copyOneSkill(srcID, dest.ID, src, dst, name, target)
		if err != nil {
			if single {
				s.failSkill(c, err)
				return
			}
			skipped = append(skipped, skillCopySkipFrom(name, err))
			continue
		}
		copied = append(copied, hit)
	}
	c.JSON(http.StatusOK, gin.H{"copied": copied, "skipped": skipped})
}

func skillNamesToCopy(mem *memory.Store, asked []string) ([]string, bool, error) {
	if len(asked) == 0 {
		list, err := mem.ListSkills()
		if err != nil {
			return nil, false, err
		}
		names := make([]string, 0, len(list))
		for _, skill := range list {
			names = append(names, skill.Name)
		}
		return names, false, nil
	}
	names := make([]string, 0, len(asked))
	seen := map[string]struct{}{}
	for _, name := range asked {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names, true, nil
}

func (s *Server) copyOneSkill(srcID, destID string, src, dst *memory.Store, name, target string) (skillCopyHit, error) {
	if err := memory.ValidSkillName(name); err != nil {
		return skillCopyHit{}, err
	}
	skill, err := src.ReadSkill(name)
	if err != nil {
		return skillCopyHit{}, err
	}
	origin := memory.OriginOfCopy(srcID, skill)
	if !s.originStillThere(origin) || origin.ProjectID == destID {
		// The bytes being copied live here. Pointing at a missing upstream,
		// or at the destination itself, would make the next update read the
		// wrong file.
		origin = memory.SkillOrigin{ProjectID: srcID, Name: skill.Name, Digest: skill.ContentDigest}
	}
	if _, err := dst.ImportSkill(target, skill.Description, skill.Body, origin); err != nil {
		return skillCopyHit{}, err
	}
	return skillCopyHit{From: name, Name: target, ProjectID: destID}, nil
}

func (s *Server) originStillThere(origin memory.SkillOrigin) bool {
	mem, err := s.memoryStore(origin.ProjectID)
	if err != nil {
		return false
	}
	_, err = mem.ReadSkill(origin.Name)
	return err == nil
}

// memoryStore is a project's memory, or the shared library when the origin
// id is the library. A missing project is still a missing project.
func (s *Server) memoryStore(projectID string) (*memory.Store, error) {
	if projectID == memory.LibraryID {
		return s.engine.LibraryMemory(), nil
	}
	if _, err := s.engine.GetProject(projectID); err != nil {
		return nil, err
	}
	return s.engine.ProjectMemory(projectID), nil
}

func skillCopySkipFrom(name string, err error) skillCopySkip {
	skip := skillCopySkip{Name: name, Error: err.Error()}
	switch {
	case errors.Is(err, memory.ErrSkillExists):
		skip.Code = "exists"
	case errors.Is(err, memory.ErrDuplicateSkill):
		skip.Code = "duplicate"
	case errors.Is(err, memory.ErrNoMatch):
		skip.Code = "not_found"
	case errors.Is(err, memory.ErrBadName):
		skip.Code = "bad_name"
	default:
		skip.Code = "rejected"
	}
	return skip
}

type pullSkillRequest struct {
	Force bool `json:"force"`
}

// pullSkill copies the upstream text over this skill. The upstream file is
// not written. A copy with local edits is left alone unless force is set.
func (s *Server) pullSkill(c *gin.Context) {
	p, ok := s.project(c)
	if !ok {
		return
	}
	var req pullSkillRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	name := c.Param("name")
	mem := s.engine.ProjectMemory(p.ID)
	current, err := mem.ReadSkill(name)
	if err != nil {
		s.failSkill(c, err)
		return
	}
	if current.Origin == nil || current.Origin.ProjectID == "" || current.Origin.Name == "" {
		s.failSkill(c, &memory.SkillPullError{Reason: "unlinked"})
		return
	}
	upstream, err := s.memoryStore(current.Origin.ProjectID)
	if err != nil {
		s.fail(c, err)
		return
	}
	source, err := upstream.ReadSkill(current.Origin.Name)
	if err != nil {
		s.failSkill(c, err)
		return
	}
	updated, err := mem.PullSkill(name, source, req.Force)
	if err != nil {
		s.failSkill(c, err)
		return
	}
	annotated := s.annotateOrigins([]memory.SkillInfo{updated.SkillInfo})
	updated.SkillInfo = annotated[0]
	c.JSON(http.StatusOK, gin.H{"skill": updated})
}

// annotateOrigins fills whether each copy is still the text that was copied,
// has an update waiting, or has local edits. The files themselves are not
// rewritten: opening the list is not a sync.
func (s *Server) annotateOrigins(skills []memory.SkillInfo) []memory.SkillInfo {
	if len(skills) == 0 {
		return skills
	}
	type cacheKey struct{ project, name string }
	type cached struct {
		skill memory.Skill
		found bool
	}
	bodies := map[cacheKey]cached{}
	projects := map[string]string{}
	alive := map[string]bool{}
	for i := range skills {
		origin := skills[i].Origin
		if origin == nil {
			continue
		}
		cp := *origin
		if _, ok := alive[cp.ProjectID]; !ok {
			if cp.ProjectID == memory.LibraryID {
				alive[cp.ProjectID] = true
				projects[cp.ProjectID] = ""
			} else if p, err := s.engine.GetProject(cp.ProjectID); err != nil {
				alive[cp.ProjectID] = false
				projects[cp.ProjectID] = ""
			} else {
				alive[cp.ProjectID] = true
				projects[cp.ProjectID] = p.Name
			}
		}
		cp.ProjectName = projects[cp.ProjectID]
		found := false
		sourceDigest := ""
		if alive[cp.ProjectID] && cp.Name != "" {
			key := cacheKey{cp.ProjectID, cp.Name}
			hit, ok := bodies[key]
			if !ok {
				mem, err := s.memoryStore(cp.ProjectID)
				var skill memory.Skill
				if err == nil {
					skill, err = mem.ReadSkill(cp.Name)
				}
				hit = cached{skill: skill, found: err == nil}
				bodies[key] = hit
			}
			found = hit.found
			sourceDigest = hit.skill.ContentDigest
		}
		cp.Status = memory.CompareOrigin(skills[i].ContentDigest, cp.Digest, sourceDigest, found && cp.Digest != "")
		skills[i].Origin = &cp
	}
	return skills
}
