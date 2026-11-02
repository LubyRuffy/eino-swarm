package server

import (
	"net/http"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/gin-gonic/gin"
)

func (s *Server) getNuwa(c *gin.Context) {
	if _, ok := s.project(c); !ok {
		return
	}
	snapshot, err := s.engine.ProjectNuwaSnapshot(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, snapshot)
}

// Project revisions invalidate snapshots; they are not chat sequence numbers.
// Sampling the durable revision also repairs missed notifications after restart,
// reconnect or a slow reader without maintaining another in-memory event log.
func (s *Server) streamNuwa(c *gin.Context) {
	project, ok := s.project(c)
	if !ok {
		return
	}
	w := c.Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	writeSSE(c, "nuwa_updated", gin.H{"project_id": project.ID, "revision": project.NuwaRevision}, project.NuwaRevision)
	w.Flush()
	last := project.NuwaRevision
	ticker := time.NewTicker(lagCheckInterval)
	defer ticker.Stop()
	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()
	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var current store.Project
			if err := s.engine.Store().DB().WithContext(ctx).Select("nuwa_revision").First(&current, "id = ?", project.ID).Error; err != nil {
				return
			}
			if current.NuwaRevision != last {
				writeSSE(c, "nuwa_updated", gin.H{"project_id": project.ID, "revision": current.NuwaRevision}, current.NuwaRevision)
				w.Flush()
				last = current.NuwaRevision
			}
		case <-heartbeat.C:
			writeSSE(c, "heartbeat", gin.H{"project_id": project.ID}, 0)
			w.Flush()
		}
	}
}
