package server

import (
	"encoding/json"
	"io"
	"net/http"
	"os"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/gin-gonic/gin"
)

func (s *Server) postDesktopRecovery(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var in engine.DesktopRecoveryInput
	if err := decoder.Decode(&in); err != nil {
		s.fail(c, engine.ErrInvalidDesktopRecovery)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		s.fail(c, engine.ErrInvalidDesktopRecovery)
		return
	}
	if !validDraftScopeID(in.SessionID) || in.EnginePID != os.Getpid() || in.ShellPID < 1 || int64(in.ShellPID) > 2147483647 || in.Generation < 0 || in.Attempt < 0 || in.Attempt > 2 || in.DurationMS < 0 || in.At.IsZero() || len(in.Reason) > 256 || !desktopRecoveryPhase(in.Phase) ||
		(in.TurnID != "" && (in.ThreadID == "" || !validDraftScopeID(in.TurnID))) || (in.ThreadID != "" && !validDraftScopeID(in.ThreadID)) {
		s.fail(c, engine.ErrInvalidDesktopRecovery)
		return
	}
	if err := s.engine.RecordDesktopRecovery(c.Request.Context(), in); err != nil {
		s.log.Warn("could not record desktop recovery", "session_id", in.SessionID, "phase", in.Phase, "turn_id", in.TurnID, "err", err)
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"persisted": in.TurnID != ""})
}

func desktopRecoveryPhase(phase string) bool {
	switch phase {
	case "probe_failed", "reloading", "replacing", "ready", "recovered", "exhausted", "replacement_failed", "suspended", "resumed":
		return true
	default:
		return false
	}
}
