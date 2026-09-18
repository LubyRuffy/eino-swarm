package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/gin-gonic/gin"
)

type createScheduleRequest struct {
	Kind           string     `json:"kind"`
	ThreadID       string     `json:"thread_id"`
	OriginThreadID string     `json:"origin_thread_id"`
	ProjectID      string     `json:"project_id"`
	ProviderID     string     `json:"provider_id"`
	Model          string     `json:"model"`
	Title          string     `json:"title"`
	Prompt         string     `json:"prompt"`
	DelayS         int        `json:"delay_s"`
	EveryS         int        `json:"every_s"`
	Cron           string     `json:"cron"`
	MaxRuns        int        `json:"max_runs"`
	Until          *time.Time `json:"until"`
}

type patchScheduleRequest struct {
	Status *string `json:"status"`
	Title  *string `json:"title"`
	Prompt *string `json:"prompt"`
	DelayS *int    `json:"delay_s"`
	EveryS *int    `json:"every_s"`
	Cron   *string `json:"cron"`
}

func (s *Server) listSchedules(c *gin.Context) {
	rows, err := s.engine.ListSchedules()
	if err != nil {
		s.fail(c, err)
		return
	}
	status := strings.TrimSpace(c.Query("status"))
	kind := strings.TrimSpace(c.Query("kind"))
	out := make([]store.Schedule, 0, len(rows))
	for i := range rows {
		if status != "" && rows[i].Status != status {
			continue
		}
		if kind != "" && rows[i].Kind != kind {
			continue
		}
		out = append(out, rows[i])
	}
	unread, err := s.engine.CountUnreadRuns()
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"schedules": out, "unread": unread})
}

func (s *Server) createSchedule(c *gin.Context) {
	var req createScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	row, err := s.engine.CreateSchedule(engine.ScheduleInput{
		Kind:           req.Kind,
		ThreadID:       req.ThreadID,
		OriginThreadID: req.OriginThreadID,
		ProjectID:      req.ProjectID,
		ProviderID:     req.ProviderID,
		Model:          req.Model,
		Title:          req.Title,
		Prompt:         req.Prompt,
		DelayS:         req.DelayS,
		EveryS:         req.EveryS,
		Cron:           req.Cron,
		MaxRuns:        req.MaxRuns,
		UntilAt:        req.Until,
		CreatedBy:      store.ScheduleCreatedHuman,
	})
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"schedule": row})
}

func (s *Server) getSchedule(c *gin.Context) {
	row, err := s.engine.GetSchedule(c.Param("id"))
	if err != nil {
		s.fail(c, err)
		return
	}
	runs, err := s.engine.ListRuns(row.ID)
	if err != nil {
		s.fail(c, err)
		return
	}
	if len(runs) == 0 {
		runs = []store.ScheduleRun{}
	}
	c.JSON(http.StatusOK, gin.H{"schedule": row, "runs": runs})
}

func (s *Server) patchSchedule(c *gin.Context) {
	var req patchScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	row, err := s.engine.PatchScheduleFields(c.Param("id"), engine.ScheduleFields{
		Status: req.Status,
		Title:  req.Title,
		Prompt: req.Prompt,
		DelayS: req.DelayS,
		EveryS: req.EveryS,
		Cron:   req.Cron,
	})
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"schedule": row})
}

func (s *Server) deleteSchedule(c *gin.Context) {
	if err := s.engine.CancelSchedule(c.Param("id")); err != nil {
		s.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) runScheduleNow(c *gin.Context) {
	turn, err := s.engine.RunScheduleNow(c.Param("id"))
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"turn": turn})
}

func (s *Server) markScheduleRunRead(c *gin.Context) {
	if err := s.engine.MarkRunRead(c.Param("rid")); err != nil {
		s.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
