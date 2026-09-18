package server

import (
	"net/http"
	"strconv"
	"strings"

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
// reader scrolls up. The live-edge payload also carries spawned /
// finished / cleanup rows that have fallen out of that viewport, so
// the Agents tab still has a roster.
func (s *Server) listLog(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	before := parseBefore(c)
	events, hasMore, err := s.engine.Store().ListTailEvents(th.ID, before, parseLogLimit(c))
	if err != nil {
		s.fail(c, err)
		return
	}
	if events == nil {
		events = []store.Event{}
	}
	body := gin.H{
		"events":   events,
		"has_more": hasMore,
	}
	// Only the live-edge page carries the roster. Older pages must not
	// move the history cursor; the client already has these rows.
	if before == 0 {
		roster, err := s.engine.Store().ListRosterEvents(th.ID)
		if err != nil {
			s.fail(c, err)
			return
		}
		body["roster"] = rosterOutsidePage(roster, events)
	}
	c.JSON(http.StatusOK, body)
}

func rosterOutsidePage(roster, page []store.Event) []store.Event {
	have := make(map[int64]struct{}, len(page))
	for _, ev := range page {
		have[ev.Seq] = struct{}{}
	}
	out := make([]store.Event, 0, len(roster))
	for _, ev := range roster {
		if _, ok := have[ev.Seq]; ok {
			continue
		}
		out = append(out, ev)
	}
	return out
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

// listAgentLog is one worker's stored rows. Opening the Agents tab used to
// show an empty body for anyone whose tools had fallen out of the live-edge
// viewport; this is that body, without walking the rest of the conversation.
func (s *Server) listAgentLog(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	agent := strings.TrimSpace(c.Param("agent"))
	if agent == "" || len(agent) > 64 {
		badRequest(c, "missing agent")
		return
	}
	events, err := s.engine.Store().ListAgentEvents(th.ID, agent)
	if err != nil {
		s.fail(c, err)
		return
	}
	if events == nil {
		events = []store.Event{}
	}
	c.JSON(http.StatusOK, gin.H{"events": events})
}
