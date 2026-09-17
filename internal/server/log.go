package server

import (
	"net/http"
	"strconv"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/gin-gonic/gin"
)

// A page of history is sized for one viewport of tool rows. The client
// picks the limit from the scroller height; these bounds stop a typo
// from dumping an 18-hour log in one JSON body.
const (
	defaultLogLimit = 80
	maxLogLimit     = 200
)

// listLog is the tail of the event log as JSON, not SSE. Opening a
// conversation paints the live edge first; older pages load when the
// reader scrolls up.
func (s *Server) listLog(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	events, hasMore, err := s.engine.Store().ListTailEvents(th.ID, parseBefore(c), parseLogLimit(c))
	if err != nil {
		s.fail(c, err)
		return
	}
	if events == nil {
		events = []store.Event{}
	}
	c.JSON(http.StatusOK, gin.H{
		"events":   events,
		"has_more": hasMore,
	})
}

func parseBefore(c *gin.Context) int64 {
	n, err := strconv.ParseInt(c.Query("before"), 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func parseLogLimit(c *gin.Context) int {
	n, err := strconv.Atoi(c.Query("limit"))
	if err != nil {
		return defaultLogLimit
	}
	return clampLogLimit(n)
}

func clampLogLimit(n int) int {
	if n <= 0 {
		return defaultLogLimit
	}
	if n > maxLogLimit {
		return maxLogLimit
	}
	return n
}
