package server

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (s *Server) preempt(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	if err := s.engine.Preempt(th.ID); err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"preempted": true})
}

func (s *Server) retractSteer(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	seq, err := strconv.ParseInt(c.Param("seq"), 10, 64)
	if err != nil || seq <= 0 {
		badRequest(c, "seq must be a positive integer")
		return
	}
	if err := s.engine.RetractSteer(th.ID, seq); err != nil {
		s.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) reviseSteer(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	seq, err := strconv.ParseInt(c.Param("seq"), 10, 64)
	if err != nil || seq <= 0 {
		badRequest(c, "seq must be a positive integer")
		return
	}
	var req followupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	if err := s.engine.ReviseUnreadSteer(th.ID, seq, req.Text); err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"revised": true})
}
