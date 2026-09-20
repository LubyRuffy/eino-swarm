package server

import (
	"errors"
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
//
// The tree is copied into memory first. desktop and web both call
// frontend.Load, which points at frontend/dist on disk; a second `go run`
// (or `make frontend`) runs Vite, which deletes the hashed JS the first
// window's index.html still names. Serving that HTML as the "JS" file is
// how WebKit paints a white window (`text/html` is not a valid JavaScript
// MIME type for a module script). A missing file under /assets/ therefore
// 404s instead of falling back, and the copy means the live directory can
// be rewritten without taking the already-open window with it.
func (s *Server) mountAssets(r *gin.Engine, assets fs.FS) {
	frozen, err := freezeAssets(assets)
	if err != nil {
		s.log.Warn("could not freeze the front end bundle; serving it live", "err", err)
	} else {
		assets = frozen
	}
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
			info, statErr := f.Stat()
			_ = f.Close()
			if statErr != nil || info.IsDir() {
				notFoundAsset(c)
				return
			}
			// Hashed bundle file names make the content immutable, so the
			// desktop shell and the browser can both cache them hard.
			if strings.HasPrefix(name, "assets/") {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(c.Writer, c.Request)
			return
		}
		if bundledFile(name) {
			notFoundAsset(c)
			return
		}
		serveIndex(c)
	})
}

func notFoundAsset(c *gin.Context) {
	c.Data(http.StatusNotFound, "text/plain; charset=utf-8", []byte("not found"))
}

func bundledFile(name string) bool {
	return name == "assets" || strings.HasPrefix(name, "assets/") || path.Ext(name) != ""
}

func freezeAssets(src fs.FS) (fs.FS, error) {
	out := snapFS{}
	err := fs.WalkDir(src, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(src, name)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		cp := make([]byte, len(data))
		copy(cp, data)
		out[name] = cp
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
