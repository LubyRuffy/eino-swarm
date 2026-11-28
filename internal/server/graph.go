package server

import (
	"net/http"
	"strconv"

	"github.com/LubyRuffy/eino-swarm/frontend"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/gin-gonic/gin"
)

// graphQuery reads the optional filters shared by both graph routes. A missing
// limit asks for the whole graph, which the store caps and reports with more.
func graphQuery(c *gin.Context) (store.GraphQuery, bool) {
	q := store.GraphQuery{
		ID: c.Query("id"), Status: c.Query("status"), Text: c.Query("text"),
		Todos: c.Query("todos") == "1", Limit: -1,
		Preview: c.Query("preview") == "1",
	}
	q.All = q.ID == "" && !q.Todos
	for name, dst := range map[string]*int{"offset": &q.Offset, "limit": &q.Limit} {
		raw := c.Query(name)
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err != nil || (name == "offset" && n < 0) {
			badRequest(c, "%s must be a number", name)
			return q, false
		}
		*dst = n
	}
	return q, true
}

func (s *Server) extractThreadGraph(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	if s.rejectSignedOut(c) {
		return
	}
	turn, err := s.engine.ExtractGraph(th.ID, c.Query("clear") == "1", c.Query("owner_id"))
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"turn": turn})
}

func (s *Server) getThreadGraph(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	q, ok := graphQuery(c)
	if !ok {
		return
	}
	view, err := s.engine.ReadThreadGraph(c.Request.Context(), th.ID, q)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

func (s *Server) getProjectGraph(c *gin.Context) {
	q, ok := graphQuery(c)
	if !ok {
		return
	}
	view, err := s.engine.ReadProjectGraph(c.Request.Context(), c.Param("id"), q)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}

// getGraphKinds serves the one catalog the panel renders and update_graph
// describes, so a kind added there appears in both.
func (s *Server) getGraphKinds(c *gin.Context) {
	c.Data(http.StatusOK, "application/json; charset=utf-8", frontend.GraphKinds())
}

// graphStream sends an initial graph_updated frame and another whenever the
// owner, revision or preview changes. A preview asks for a refetch of the same
// owner's current revision.
type graphStream struct {
	threadID string
	owner    string
	revision int64
	preview  uint64
}

func (s *Server) startGraphDelivery(c *gin.Context, threadID string) *graphStream {
	if c.Query("graph") != "1" {
		return nil
	}
	d := &graphStream{threadID: threadID}
	s.pollGraphDelivery(c, d)
	return d
}

func (s *Server) pollGraphDelivery(c *gin.Context, d *graphStream) bool {
	if d == nil {
		return false
	}
	owner, rev, preview, err := s.engine.ThreadGraphWatch(c.Request.Context(), d.threadID)
	if err != nil || (owner == d.owner && rev == d.revision && preview == d.preview) {
		return false
	}
	body := gin.H{"thread_id": d.threadID, "owner_id": owner, "revision": rev}
	if owner == d.owner && rev == d.revision && preview != d.preview {
		body["preview"] = true
	}
	d.owner, d.revision, d.preview = owner, rev, preview
	writeSSE(c, "graph_updated", body, 0)
	return true
}
