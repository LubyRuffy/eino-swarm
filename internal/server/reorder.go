package server

import (
	"github.com/gin-gonic/gin"
)

type reorderRequest struct {
	IDs []string `json:"ids"`
}

func (s *Server) reorderThreads(c *gin.Context) {
	var req reorderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	if err := s.engine.Store().ReorderThreads(req.IDs); err != nil {
		s.fail(c, err)
		return
	}
	s.listThreads(c)
}

func (s *Server) reorderProjects(c *gin.Context) {
	var req reorderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	if err := s.engine.Store().ReorderProjects(req.IDs); err != nil {
		s.fail(c, err)
		return
	}
	s.listProjects(c)
}
