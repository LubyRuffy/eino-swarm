package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/LubyRuffy/eino-swarm/internal/remote"
	"github.com/gin-gonic/gin"
)

func (s *Server) postRemoteLogout(c *gin.Context) {
	var request *struct{}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request == nil {
		badRequest(c, "invalid logout request")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		badRequest(c, "invalid logout request")
		return
	}
	if s.remote == nil {
		remoteOffline(c, "remote is not running")
		return
	}
	if err := s.remote.LogoutContext(c.Request.Context()); err != nil {
		if errors.Is(err, remote.ErrAccountLogoutUnavailable) {
			c.JSON(http.StatusConflict, gin.H{"error": "account logout requires account mode", "code": "remote_logout_unavailable"})
			return
		}
		if errors.Is(err, remote.ErrAccountLogoutInProgress) {
			c.JSON(http.StatusConflict, gin.H{"error": "another account operation is in progress", "code": "remote_logout_in_progress"})
			return
		}
		if errors.Is(err, remote.ErrGatewayLogoutUnsupported) {
			c.JSON(http.StatusConflict, gin.H{"error": "upgrade the Gateway to support device logout, then retry; the private credential was retained", "code": "remote_logout_unsupported"})
			return
		}
		// The local path and private record contents never enter the response.
		s.log.Warn("remote account logout failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "private account state could not be removed; retry logout", "code": "remote_logout_failed"})
		return
	}
	s.getRemoteStatus(c)
}
