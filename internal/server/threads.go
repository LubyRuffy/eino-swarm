package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/gin-gonic/gin"
)

type createThreadRequest struct {
	Title      string `json:"title"`
	ProviderID string `json:"provider_id"`
}

type threadView struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	ProviderID   string `json:"provider_id"`
	Archived     bool   `json:"archived"`
	CreatedAt    string `json:"created_at"`
	LastActiveAt string `json:"last_active_at"`
	Running      bool   `json:"running"`
}

func (s *Server) listThreads(c *gin.Context) {
	threads, err := s.engine.Store().ListThreads(c.Query("archived") == "1")
	if err != nil {
		s.fail(c, err)
		return
	}
	// One pass over the live runtimes instead of a status lookup per row: the
	// sidebar renders every conversation, and most of them are idle.
	running := map[string]bool{}
	for _, id := range s.engine.Running() {
		running[id] = true
	}
	out := make([]threadView, 0, len(threads))
	for _, th := range threads {
		out = append(out, threadView{
			ID:           th.ID,
			Title:        th.Title,
			ProviderID:   th.ProviderID,
			Archived:     th.Archived,
			CreatedAt:    th.CreatedAt.Format(timeFormat),
			LastActiveAt: th.LastActiveAt.Format(timeFormat),
			Running:      running[th.ID],
		})
	}
	c.JSON(http.StatusOK, gin.H{"threads": out})
}

const timeFormat = "2006-01-02T15:04:05.000Z07:00"

func (s *Server) createThread(c *gin.Context) {
	var req createThreadRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	th, err := s.engine.CreateThread(req.Title, req.ProviderID)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"thread": threadView{
		ID:           th.ID,
		Title:        th.Title,
		ProviderID:   th.ProviderID,
		CreatedAt:    th.CreatedAt.Format(timeFormat),
		LastActiveAt: th.LastActiveAt.Format(timeFormat),
	}})
}

func (s *Server) getThread(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	status := s.engine.Status(th.ID)
	c.JSON(http.StatusOK, gin.H{
		"thread": threadView{
			ID:           th.ID,
			Title:        th.Title,
			ProviderID:   th.ProviderID,
			Archived:     th.Archived,
			CreatedAt:    th.CreatedAt.Format(timeFormat),
			LastActiveAt: th.LastActiveAt.Format(timeFormat),
			Running:      status.Running,
		},
		"status": status,
	})
}

type patchThreadRequest struct {
	Title      *string `json:"title"`
	ProviderID *string `json:"provider_id"`
	Archived   *bool   `json:"archived"`
}

func (s *Server) patchThread(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	var req patchThreadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	if req.Title != nil {
		if strings.TrimSpace(*req.Title) == "" {
			badRequest(c, "a conversation title cannot be empty")
			return
		}
		if err := s.engine.RenameThread(th.ID, *req.Title); err != nil {
			s.fail(c, err)
			return
		}
	}
	if req.ProviderID != nil {
		if err := s.engine.SetThreadProvider(th.ID, *req.ProviderID); err != nil {
			s.fail(c, err)
			return
		}
	}
	if req.Archived != nil {
		if err := s.engine.SetThreadArchived(th.ID, *req.Archived); err != nil {
			s.fail(c, err)
			return
		}
	}
	s.getThread(c)
}

func (s *Server) deleteThread(c *gin.Context) {
	if _, ok := s.thread(c); !ok {
		return
	}
	if err := s.engine.DeleteThread(c.Param("id")); err != nil {
		s.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ---------- turns ----------

type turnRequest struct {
	Text string `json:"text"`
}

func (s *Server) startTurn(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	var req turnRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	turn, err := s.engine.StartTurn(th.ID, req.Text)
	if err != nil {
		s.fail(c, err)
		return
	}
	// 202: the turn is accepted and running. Its answer arrives on the event
	// stream, because a turn routinely outlives the request that started it.
	c.JSON(http.StatusAccepted, gin.H{"turn": turn})
}

// steer delivers guidance to a running turn. When nothing is running it starts
// a turn instead: from the user's side both are "I pressed Enter", and making
// the browser decide would race with the turn finishing.
func (s *Server) steer(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	var req turnRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	err := s.engine.Steer(th.ID, req.Text)
	if err == nil {
		c.JSON(http.StatusAccepted, gin.H{"steered": true})
		return
	}
	if !isIdle(err) {
		s.fail(c, err)
		return
	}
	turn, startErr := s.engine.StartTurn(th.ID, req.Text)
	if startErr != nil {
		s.fail(c, startErr)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"steered": false, "turn": turn})
}

func isIdle(err error) bool {
	return err != nil && errors.Is(err, engine.ErrIdle)
}

func (s *Server) interrupt(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	if err := s.engine.Interrupt(th.ID); err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"interrupted": true})
}

func (s *Server) listTurns(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	turns, err := s.engine.Store().ListTurns(th.ID)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"turns": turns})
}
