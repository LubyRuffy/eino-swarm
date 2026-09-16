package server

import (
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

// getInputImage serves one pasted image so the transcript can render a
// thumbnail. Inline, not attachment: the <img> tag has to display it. The
// bytes were sniffed as a real image on the way in, so this is not the
// workspace-download path that forces download to keep agent HTML from
// executing as same-origin script.
func (s *Server) getInputImage(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	data, mime, err := s.engine.ReadInputImage(th.ID, c.Param("image_id"))
	if err != nil {
		s.fail(c, err)
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "private, max-age=31536000, immutable")
	c.Header("Content-Disposition",
		fmt.Sprintf("inline; filename*=UTF-8''%s", escapePath(filepath.Base(c.Param("image_id")))))
	c.Data(http.StatusOK, mime, data)
}
