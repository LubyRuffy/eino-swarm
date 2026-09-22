package server

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

type openURLRequest struct {
	URL string `json:"url"`
}

var errBadExternalURL = errors.New("the URL must be an absolute http(s) address")

// openURL launches the platform browser. The hook is nil unless this process
// is a desktop window or a loopback engine serving one. A browser tab does
// not call this.
func (s *Server) openURL(c *gin.Context) {
	if s.opts.OpenURL == nil {
		c.JSON(http.StatusNotImplemented, gin.H{
			"error": "opening a URL in the system browser is only available in the desktop app",
		})
		return
	}
	var req openURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	target, err := parseExternalURL(req.URL)
	if err != nil {
		badRequest(c, "%s", err.Error())
		return
	}
	if err := s.opts.OpenURL(target); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"opened": target})
}

func parseExternalURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errBadExternalURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Scheme == "" {
		return "", errBadExternalURL
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return u.String(), nil
	default:
		return "", errBadExternalURL
	}
}
