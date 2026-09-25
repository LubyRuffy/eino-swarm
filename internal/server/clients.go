package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/clients"
	"github.com/gin-gonic/gin"
)

func (s *Server) getClients(c *gin.Context) {
	var before int64
	if raw := c.Query("before"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			badRequest(c, "before must be a unix millisecond")
			return
		}
		before = n
	}
	c.JSON(http.StatusOK, clients.List(s.engine.Config().Clients, time.Now(), before))
}
