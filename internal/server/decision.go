package server

import (
	"errors"
	"net/http"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/gin-gonic/gin"
)

func (s *Server) decideThread(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if s.rejectSignedOut(c) {
		return
	}
	text, err := s.engine.Decide(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, engine.ErrDecisionUnavailable) {
			c.JSON(http.StatusNotFound, gin.H{"error": "This conversation has no decision model.", "code": "decision_unavailable"})
			return
		}
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"text": text})
}
