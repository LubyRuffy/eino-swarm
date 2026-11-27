package server

import (
	"errors"
	"net/http"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/gin-gonic/gin"
)

func (s *Server) getProjectDistillation(c *gin.Context) {
	result, err := s.engine.ProjectDistillationStatus(c.Request.Context(), c.Param("id"))
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) startProjectDistillation(c *gin.Context) {
	if s.rejectSignedOut(c) {
		return
	}
	var input engine.DistillationRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		badRequest(c, "invalid distillation request")
		return
	}
	result, err := s.engine.StartProjectDistillation(c.Request.Context(), c.Param("id"), input)
	switch {
	case errors.Is(err, engine.ErrDistillationScope):
		badRequest(c, "%s", err)
	case errors.Is(err, engine.ErrDistillationTargets):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "distillation_disabled"})
	case errors.Is(err, engine.ErrDistillationPending):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "distillation_pending"})
	case err != nil:
		s.fail(c, err)
	case result.State == "running" || result.State == "pausing":
		c.JSON(http.StatusAccepted, result)
	default:
		c.JSON(http.StatusOK, result)
	}
}
