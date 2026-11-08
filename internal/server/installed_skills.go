package server

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/skills"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/gin-gonic/gin"
)

const maxSkillZIPUpload = 16 << 20

func (s *Server) listInstalledSkills(c *gin.Context) {
	catalog := s.externalSkills(c)
	if catalog == nil {
		return
	}
	items, err := catalog.ListInstalledSkills()
	if err != nil {
		s.failInstalledSkill(c, err)
		return
	}
	if items == nil {
		items = []skills.SkillInfo{}
	}
	c.JSON(http.StatusOK, gin.H{"skills": items})
}

func (s *Server) installSkill(c *gin.Context) {
	catalog := s.externalSkills(c)
	if catalog == nil {
		return
	}
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/zip" {
		c.JSON(http.StatusUnsupportedMediaType, gin.H{"error": "upload a skill ZIP with Content-Type application/zip"})
		return
	}

	data, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxSkillZIPUpload))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "skill ZIP exceeds the 16 MiB upload limit"})
		} else {
			badRequest(c, "could not read the skill ZIP")
		}
		return
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		badRequest(c, "could not open the skill ZIP")
		return
	}
	skill, err := catalog.InstallSkillZIP(c.Request.Context(), archive)
	switch {
	case err == nil:
		c.JSON(http.StatusCreated, skill)
	case errors.Is(err, skills.ErrInvalidSkillBundle):
		badRequest(c, "%s", err.Error())
	case errors.Is(err, skills.ErrSkillInstallConflict):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "exists"})
	default:
		s.failInstalledSkill(c, err)
	}
}

func (s *Server) getInstalledSkill(c *gin.Context) {
	catalog := s.externalSkills(c)
	if catalog == nil {
		return
	}
	skill, err := catalog.ReadInstalledSkill(c.Param("name"))
	if err != nil {
		s.failInstalledSkill(c, err)
		return
	}
	c.JSON(http.StatusOK, skill)
}

func (s *Server) removeInstalledSkill(c *gin.Context) {
	catalog := s.externalSkills(c)
	if catalog == nil {
		return
	}
	if err := catalog.RemoveInstalledSkill(c.Param("name")); err != nil {
		s.failInstalledSkill(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) externalSkills(c *gin.Context) *skills.Store {
	catalog, err := s.engine.ProjectExternalSkills(strings.TrimSpace(c.Param("id")))
	if err != nil {
		s.failInstalledSkill(c, err)
		return nil
	}
	return catalog
}

func (s *Server) failInstalledSkill(c *gin.Context, err error) {
	switch {
	case errors.Is(err, skills.ErrProjectDeleted):
		s.fail(c, store.ErrNotFound)
	case errors.Is(err, skills.ErrNoMatch):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, skills.ErrBadName):
		badRequest(c, "%s", err.Error())
	default:
		s.fail(c, err)
	}
}
