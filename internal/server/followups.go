package server

import (
	"net/http"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/gin-gonic/gin"
)

type followupRequest struct {
	Text string `json:"text"`
}

func (s *Server) listFollowups(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	list, err := s.engine.ListFollowups(th.ID)
	if err != nil {
		s.fail(c, err)
		return
	}
	if list == nil {
		list = []store.Followup{}
	}
	c.JSON(http.StatusOK, gin.H{"followups": list})
}

func (s *Server) enqueueFollowup(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	var req followupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	f, err := s.engine.EnqueueFollowup(th.ID, req.Text)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"followup": f})
}

func (s *Server) deleteFollowup(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	if err := s.engine.DeleteFollowup(th.ID, c.Param("fid")); err != nil {
		s.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) requeueFollowup(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	var req followupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	f, err := s.engine.RequeueFollowup(th.ID, c.Param("fid"), req.Text)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"followup": f})
}

func (s *Server) steerFollowup(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	if err := s.engine.SteerFollowup(th.ID, c.Param("fid")); err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"steered": true})
}
