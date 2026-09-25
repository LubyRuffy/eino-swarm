package server

import (
	"net/http"
	"strconv"
	"time"

	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/clients"
	"github.com/LubyRuffy/eino-swarm/internal/store"
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
	c.JSON(http.StatusOK, clients.View(s.engine.Config().Clients, time.Now(), before))
}

func (s *Server) getClientTask(c *gin.Context) {
	id := strings.TrimSpace(c.Query("id"))
	if id == "" {
		badRequest(c, "id is required")
		return
	}
	doc, ok := clients.Read(s.engine.Config().Clients, id, time.Now())
	if !ok {
		s.fail(c, store.ErrNotFound)
		return
	}
	c.JSON(http.StatusOK, doc)
}
