package server

import (
	"encoding/base64"
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
		})
		return
	}
	c.JSON(http.StatusOK, s.remote.Status())
}

func (s *Server) putRemoteToken(c *gin.Context) {
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
