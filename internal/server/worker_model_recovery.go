package server

import (
	"errors"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (s *Server) recoverWorkerModels(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	var req struct {
		ProviderID string `json:"provider_id"`
		Model      string `json:"model"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	count, err := s.engine.RecoverWorkerModels(c.Request.Context(), th.ID, req.ProviderID, req.Model)
	if err != nil {
		if errors.Is(err, engine.ErrInvalidWorkerRecovery) || errors.Is(err, provider.ErrUnknownProvider) {
			badRequest(c, "%v", err)
		} else {
			s.fail(c, err)
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"configurations": count})
}
