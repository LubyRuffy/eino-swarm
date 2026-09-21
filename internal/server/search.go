package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/search"
	"github.com/gin-gonic/gin"
)

func (s *Server) getSearch(c *gin.Context) {
	if s.search == nil {
		c.JSON(http.StatusOK, search.Result{Query: strings.TrimSpace(c.Query("q")), Hits: []search.Hit{}})
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	res, err := s.search.Search(c.Request.Context(), c.Query("q"), limit)
	if err != nil {
		s.fail(c, err)
		return
	}
	if res.Hits == nil {
		res.Hits = []search.Hit{}
	}
	c.JSON(http.StatusOK, res)
}
