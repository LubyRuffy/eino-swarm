package server

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Recovery inputs cannot change after their snapshot. Reading and durable
// drafts stay available; Stop is ordered against recovery in the engine.
func (s *Server) requireStartupReady() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.FullPath()
		allowed := (c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead) && !strings.HasSuffix(path, "/terminal") ||
			strings.HasPrefix(path, "/api/composer/drafts/") || path == "/api/presence" ||
			path == "/api/desktop/recovery" || path == "/api/threads/:id/interrupt" ||
			path == "/api/threads/:id/read" || path == "/api/schedules/runs/:rid/read"
		if !allowed {
			if err := s.engine.RequireReady(); err != nil {
				s.fail(c, err)
				c.Abort()
				return
			}
		}
		c.Next()
	}
}
