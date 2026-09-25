package server

import (
	"net/http"

	"github.com/LubyRuffy/eino-swarm/internal/update"
	"github.com/gin-gonic/gin"
)

type updateRequest struct {
	Version string `json:"version"`
}

func (s *Server) getUpdate(c *gin.Context) {
	if s.updater == nil {
		c.JSON(http.StatusOK, update.Result{
			Status:  "unsupported",
			Message: "desktop updates are published for macOS",
		})
		return
	}
	c.JSON(http.StatusOK, s.updater.Check(c.Request.Context(), c.Query("fresh") == "1"))
}

func (s *Server) postUpdate(c *gin.Context) {
	if s.updater == nil {
		c.JSON(http.StatusNotImplemented, gin.H{
			"error": "desktop updates are published for macOS",
		})
		return
	}
	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	pids := s.presence.desktopPIDs()
	if err := s.updater.Apply(c.Request.Context(), req.Version, pids); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "restarting"})
}
