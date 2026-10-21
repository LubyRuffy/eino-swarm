package server

import (
	"net/http"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/gin-gonic/gin"
)

// A queue row can disappear during an otherwise healthy turn. Keep that
// failed UI action identifiable without logging its text, query or credentials.
func (s *Server) missingRecord(c *gin.Context, code, message string, identity gin.H) {
	requestID := store.NewID("req_")
	body := gin.H{
		"error": message, "code": code, "request_id": requestID,
		"method": c.Request.Method, "route": c.FullPath(),
	}
	attrs := []any{
		"request_id", requestID, "code", code,
		"method", c.Request.Method, "route", c.FullPath(), "status", http.StatusNotFound,
	}
	for key, value := range identity {
		body[key] = value
		attrs = append(attrs, key, value)
	}
	s.log.Warn("HTTP record not found", attrs...)
	c.Header("X-Request-ID", requestID)
	c.JSON(http.StatusNotFound, body)
}
