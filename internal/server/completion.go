package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Server) readThread(c *gin.Context) {
	var req struct {
		CompletedSeq int64 `json:"completed_seq"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.CompletedSeq <= 0 {
		badRequest(c, "completed_seq must be a positive terminal event sequence")
		return
	}
	if err := s.engine.Store().MarkThreadRead(c.Param("id"), req.CompletedSeq); err != nil {
		s.fail(c, err)
		return
	}
	th, err := s.engine.Store().GetThread(c.Param("id"))
	if err != nil {
		s.fail(c, err)
		return
	}
	st := s.engine.Status(th.ID)
	c.JSON(http.StatusOK, gin.H{"thread": viewThread(th, st.Running, st.AwaitingAnswer, st.Waiting)})
}
