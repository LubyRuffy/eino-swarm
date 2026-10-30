package server

import (
	"encoding/base64"
	"encoding/json"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func remoteOffline(c *gin.Context, msg string) {
	c.JSON(http.StatusConflict, gin.H{"error": msg, "code": "remote_offline"})
}

func (s *Server) getRemoteStatus(c *gin.Context) {
	if s.remote == nil {
		c.JSON(http.StatusOK, gin.H{
			"enabled": false, "hub_url": "", "has_token": false, "online": false,
			"keep_awake": false, "awake": false,
		})
		return
	}
	status := s.remote.Status()
	status.LoginAvailable = s.opts.OpenURL != nil
	c.JSON(http.StatusOK, status)
}

func (s *Server) putRemoteToken(c *gin.Context) {
	if s.engine.Config().Snapshot().Remote.AuthMode == config.RemoteAuthNOSEC {
		c.JSON(http.StatusForbidden, gin.H{"error": "NOSEC login and account approval are required", "code": "remote_login_required"})
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	if err := s.engine.Config().WriteHostToken(strings.TrimSpace(req.Token)); err != nil {
		s.fail(c, err)
		return
	}
	if s.remote != nil {
		s.remote.Reload()
	}
	s.getRemoteStatus(c)
}

func (s *Server) postRemoteOffer(c *gin.Context) {
	if s.remote == nil {
		remoteOffline(c, "remote is not running")
		return
	}
	offer, err := s.remote.Offer(c.Request.Context())
	if err != nil {
		remoteOffline(c, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"uri":        offer.URI,
		"pairing_id": offer.PairingID,
		"expires_at": offer.ExpiresAt,
		"png":        "data:image/png;base64," + base64.StdEncoding.EncodeToString(offer.PNG),
	})
}

func (s *Server) listRemoteBindings(c *gin.Context) {
	if s.remote == nil {
		c.JSON(http.StatusOK, gin.H{"bindings": []any{}})
		return
	}
	list, err := s.remote.ListBindings(c.Request.Context())
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"bindings": list})
}

func (s *Server) revokeRemoteBinding(c *gin.Context) {
	if s.remote == nil {
		remoteOffline(c, "remote is not running")
		return
	}
	if err := s.remote.RevokeBinding(c.Request.Context(), c.Param("id")); err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"revoked": true})
}

func (s *Server) postRemoteLogin(c *gin.Context) {
	if s.opts.OpenURL == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "system browser is unavailable", "code": "remote_login_unavailable"})
		return
	}
	if s.remote == nil {
		remoteOffline(c, "remote is not running")
		return
	}
	var request *struct {
		NewDevice bool `json:"new_device"`
		Confirmed bool `json:"confirmed"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || request == nil {
		badRequest(c, "invalid login request")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF || request.NewDevice && !request.Confirmed {
		badRequest(c, "new device login requires explicit confirmation")
		return
	}
	if err := s.remote.StartLogin(s.opts.OpenURL, request.NewDevice); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "remote_login_unavailable"})
		return
	}
	status := s.remote.Status()
	status.LoginAvailable = true
	c.JSON(http.StatusAccepted, status)
}
func (s *Server) deleteRemoteLogin(c *gin.Context) {
	if s.remote == nil {
		remoteOffline(c, "remote is not running")
		return
	}
	s.remote.CancelLogin()
	s.getRemoteStatus(c)
}
