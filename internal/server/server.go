// Package server is the HTTP surface both front ends run on. The desktop
// window and the browser load the same origin and use the same endpoints, so
// uploads, downloads and the event stream have exactly one implementation
// rather than one per shell.
package server

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/gin-gonic/gin"
)

// Mode says which shell is in front of this server, which the UI uses to
// decide whether to offer native affordances.
const (
	ModeWeb     = "web"
	ModeDesktop = "desktop"
)

// Options configures a server.
type Options struct {
	Engine  *engine.Engine
	Logger  *slog.Logger
	Version string
	// Mode is ModeWeb or ModeDesktop.
	Mode string
	// Assets serves the built front end. When nil the API still works, which
	// is what the handler tests and `zwai trace` rely on.
	Assets fs.FS
	// Reveal shows a path in the platform file manager. Only the desktop shell
	// supplies it; in a browser the download endpoint is the way to get a file
	// out, so the UI hides the affordance when this is absent.
	Reveal func(path string) error
	// OpenURL opens an http(s) URL in the platform browser. Only the desktop
	// shell supplies it; a browser tab uses window.open instead, so a web
	// server never spawns windows on the host.
	OpenURL func(url string) error
}

// Server owns the router.
type Server struct {
	opts   Options
	engine *engine.Engine
	log    *slog.Logger
	router *gin.Engine
}

// New builds the server and its routes.
func New(opts Options) (*Server, error) {
	if opts.Engine == nil {
		return nil, errors.New("server: an engine is required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Mode == "" {
		opts.Mode = ModeWeb
	}
	gin.SetMode(gin.ReleaseMode)

	s := &Server{opts: opts, engine: opts.Engine, log: opts.Logger}
	r := gin.New()
	r.Use(gin.Recovery(), s.accessLog())
	// A local app has no cross-origin clients to serve, and a permissive CORS
	// policy on a loopback port is how a random web page in another tab gets
	// to read the user's conversations. Same-origin only, no exceptions.
	r.MaxMultipartMemory = maxUploadMemory

	api := r.Group("/api")
	{
		api.GET("/meta", s.getMeta)
		api.POST("/open", s.openURL)
		api.GET("/settings", s.getSettings)
		api.PUT("/settings", s.putSettings)
		api.GET("/models", s.getModels)
		api.POST("/models/discover", s.discoverModels)
		api.GET("/tools", s.getTools)

		api.GET("/projects", s.listProjects)
		api.POST("/projects", s.createProject)
		api.PUT("/projects/reorder", s.reorderProjects)
		api.GET("/projects/:id", s.getProject)
		api.PATCH("/projects/:id", s.patchProject)
		api.DELETE("/projects/:id", s.deleteProject)
		api.GET("/projects/:id/memory", s.getMemory)
		api.PUT("/projects/:id/memory", s.putMemory)
		api.GET("/projects/:id/skills/:name", s.getSkill)
		api.DELETE("/projects/:id/skills/:name", s.deleteSkill)

		api.GET("/threads", s.listThreads)
		api.POST("/threads", s.createThread)
		api.PUT("/threads/reorder", s.reorderThreads)
		api.GET("/threads/:id", s.getThread)
		api.PATCH("/threads/:id", s.patchThread)
		api.DELETE("/threads/:id", s.deleteThread)

		api.GET("/threads/:id/events", s.streamEvents)
		api.POST("/threads/:id/turns", s.startTurn)
		api.POST("/threads/:id/steer", s.steer)
		api.GET("/threads/:id/followups", s.listFollowups)
		api.POST("/threads/:id/followups", s.enqueueFollowup)
		api.DELETE("/threads/:id/followups/:fid", s.deleteFollowup)
		api.POST("/threads/:id/followups/:fid/steer", s.steerFollowup)
		api.POST("/threads/:id/interrupt", s.interrupt)
		api.POST("/threads/:id/continue", s.continueTurn)
		api.POST("/threads/:id/review", s.reviewThread)
		api.POST("/threads/:id/compact", s.compactThread)
		api.GET("/threads/:id/turns", s.listTurns)

		api.GET("/threads/:id/files", s.listFiles)
		api.POST("/threads/:id/files", s.uploadFiles)
		api.GET("/threads/:id/download/*path", s.downloadFile)
		api.DELETE("/threads/:id/download/*path", s.deleteFile)
		api.GET("/threads/:id/input-images/:image_id", s.getInputImage)
		api.POST("/threads/:id/reveal", s.reveal)

		api.GET("/trace/:turn", s.getTrace)
	}

	if opts.Assets != nil {
		s.mountAssets(r, opts.Assets)
	}
	s.router = r
	return s, nil
}

// maxUploadMemory is how much of a multipart upload is buffered in memory
// before gin spills to a temp file.
const maxUploadMemory = 16 << 20

// Handler returns the http.Handler to serve.
func (s *Server) Handler() http.Handler { return s.router }

// accessLog logs one line per request at debug level, skipping the event
// stream because those requests last as long as the tab is open and would
// only ever be logged when they end.
func (s *Server) accessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasSuffix(c.Request.URL.Path, "/events") {
			c.Next()
			return
		}
		start := time.Now()
		c.Next()
		s.log.Debug("http",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"ms", time.Since(start).Milliseconds())
	}
}

// ---------- error plumbing ----------

// fail maps an error to a status code and a JSON body. Engine sentinels get
// the status the UI needs to react correctly: busy is not a server fault, and
// a missing conversation is not a bad request.
func (s *Server) fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, engine.ErrBusy):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "busy"})
	case errors.Is(err, engine.ErrIdle):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "idle"})
	case errors.Is(err, engine.ErrNothingToCompact):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "nothing_to_compact"})
	case errors.Is(err, engine.ErrInvalidWorkdir):
		// Its own code so the project dialog can put the message under the
		// working directory field instead of somewhere the user has to hunt.
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "workdir"})
	case errors.Is(err, store.ErrInvalidReorder):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
}

func badRequest(c *gin.Context, format string, args ...any) {
	c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf(format, args...)})
}

// thread loads the conversation named in the path, answering 404 itself when
// there is none so every handler does not repeat the check.
func (s *Server) thread(c *gin.Context) (*store.Thread, bool) {
	th, err := s.engine.Store().GetThread(c.Param("id"))
	if err != nil {
		s.fail(c, err)
		return nil, false
	}
	return th, true
}
