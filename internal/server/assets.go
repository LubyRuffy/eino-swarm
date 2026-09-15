package server

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

// mountAssets serves the built front end with a single-page-app fallback:
// anything that is not a real file and not under /api is answered with
// index.html, so a deep link and a browser reload land on the app instead of a
// 404.
func (s *Server) mountAssets(r *gin.Engine, assets fs.FS) {
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		s.log.Warn("no front end bundle is embedded; only the API is available", "err", err)
		return
	}
	files := http.FileServer(http.FS(assets))

	serveIndex := func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	}

	r.GET("/", serveIndex)
	r.NoRoute(func(c *gin.Context) {
		reqPath := c.Request.URL.Path
		if strings.HasPrefix(reqPath, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"error": "no such endpoint"})
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.JSON(http.StatusNotFound, gin.H{"error": "no such endpoint"})
			return
		}
		name := strings.TrimPrefix(path.Clean(reqPath), "/")
		if name == "" || name == "." {
			serveIndex(c)
			return
		}
		if f, err := assets.Open(name); err == nil {
			_ = f.Close()
			// Hashed bundle file names make the content immutable, so the
			// desktop shell and the browser can both cache them hard.
			if strings.HasPrefix(name, "assets/") {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(c.Writer, c.Request)
			return
		}
		serveIndex(c)
	})
}
