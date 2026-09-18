package server

import (
	"net/http"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/gin-gonic/gin"
)

type answerRequest struct {
	CallID  string            `json:"call_id"`
	Text    string            `json:"text"`
	Answers engine.AskAnswers `json:"answers"`
}

func (s *Server) answerTurn(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	var req answerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	var err error
	if len(req.Answers) > 0 {
		err = s.engine.AnswerTurn(th.ID, req.CallID, req.Answers)
	} else {
		err = s.engine.AnswerTurnText(th.ID, req.Text)
	}
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"answered": true})
}

func (s *Server) implementPlan(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	turn, err := s.engine.ImplementPlan(th.ID)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"turn": turn})
}
