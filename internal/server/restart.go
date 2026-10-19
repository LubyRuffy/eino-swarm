package server

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// The launcher reserves the rest of its 30s budget for exiting the old
// process and waiting for the new listener. A client disconnect cannot make
// a partially saved handoff look successful to the launcher.
const restartHandoffBudget = 19 * time.Second

func (s *Server) prepareRestart(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), restartHandoffBudget)
	defer cancel()
	status, err := s.engine.PrepareRestart(ctx)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, status)
}

func (s *Server) restartStatus(c *gin.Context) {
	c.JSON(http.StatusOK, s.engine.GetRestartStatus())
}
