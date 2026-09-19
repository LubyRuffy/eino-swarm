// Package app wires the process together: config, database, model pool,
// engine and HTTP server. Both shells (`zwai web` and `zwai desktop`) build
// the same App, which is what keeps them functionally identical.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/frontend"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/remote"
	"github.com/LubyRuffy/eino-swarm/internal/server"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Options configures the process.
type Options struct {
	// DataDir holds the config file, the database and the workspaces. Empty
	// means the default (~/.zwai-swarm, or ZWAI_HOME).
	DataDir string
	// Addr is the listen address. Empty means the configured one; ":0" or
	// "127.0.0.1:0" picks a free port, which is what desktop mode does.
	Addr string
	// Mock runs on the scripted offline provider.
	Mock bool
	// Mode is server.ModeWeb or server.ModeDesktop.
	Mode string
	// Version is reported by /api/meta.
	Version string
	// Assets overrides the embedded front end; nil uses the embedded bundle.
	Assets fs.FS
	// NoAssets serves the API only, which is what the handler tests want.
	NoAssets bool
}

// App is an assembled, not-yet-listening process.
type App struct {
	Config *config.Config
	Store  *store.Store
	Pool   *provider.Pool
	Engine *engine.Engine
	Server *server.Server
	Remote *remote.Host
	Logger *slog.Logger

	addr string

	// Serve can bind lazily while another goroutine asks for the URL or shuts
	// the process down, so the listener and the server are behind a lock.
	mu       sync.Mutex
	listener net.Listener
	http     *http.Server
	// httpStop cancels every in-flight request, including the event stream.
	// Without it, Shutdown waits out the whole deadline for a connection that
	// is designed never to end — Cmd+Q freezes the window, then Wails warns
	// that the window it just closed is already gone.
	httpStop context.CancelFunc
}

// New assembles everything. The caller owns Close.
func New(opts Options) (*App, error) {
	cfg, err := config.Load(opts.DataDir)
	if err != nil {
		return nil, err
	}
	logger := newLogger(cfg)

	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return nil, err
	}

	pool := provider.New(cfg)
	if opts.Mock {
		pool = provider.NewMock(cfg)
		logger.Info("running on the scripted offline provider; no model will be called")
	}
	eng := engine.New(cfg, st, pool, logger)
	// A previous process that was killed (or quit) leaves turns marked
	// running. Continue them here: a crash is not a user Stop.
	if n, err := eng.ResumeOrphanedTurns(); err != nil {
		logger.Warn("could not resume turns left over from a previous run", "err", err)
	} else if n > 0 {
		logger.Info("resumed turns left over from a previous run", "count", n)
	}
	eng.StartScheduler()

	assets := opts.Assets
	if assets == nil && !opts.NoAssets {
		assets = frontend.Assets()
	}
	srv, err := server.New(server.Options{
		Engine:  eng,
		Logger:  logger,
		Version: opts.Version,
		Mode:    opts.Mode,
		Assets:  assets,
		Reveal:  revealFor(opts.Mode),
		OpenURL: openURLFor(opts.Mode),
	})
	if err != nil {
		eng.Shutdown()
		_ = st.Close()
		return nil, err
	}

	addr := opts.Addr
	if addr == "" {
		addr = cfg.Server.Addr
	}
	host := remote.New(eng, cfg, logger)
	host.Start()
	srv.SetRemote(host)
	return &App{
		Config: cfg, Store: st, Pool: pool, Engine: eng, Server: srv,
		Remote: host, Logger: logger, addr: addr,
	}, nil
}

func newLogger(cfg *config.Config) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.Level(cfg.SlogLevel()),
	}))
}

// Listen binds the address without serving yet, so the caller can learn the
// real URL before anything tries to load it. Desktop mode needs that: the
// window cannot open until the port is known.
func (a *App) Listen() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.listen()
}

// listen is Listen with the lock already held.
func (a *App) listen() (string, error) {
	ln, err := net.Listen("tcp", a.addr)
	if err != nil {
		return "", fmt.Errorf("app: listen on %s: %w", a.addr, err)
	}
	a.listener = ln
	reqCtx, stop := context.WithCancel(context.Background())
	a.httpStop = stop
	a.http = &http.Server{
		Handler: a.Server.Handler(),
		// No write timeout: the event stream is a response that lasts as long
		// as the tab is open.
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return reqCtx },
	}
	return a.url(), nil
}

// URL is the base URL of the listening server.
func (a *App) URL() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.url()
}

func (a *App) url() string {
	if a.listener == nil {
		return ""
	}
	host := a.listener.Addr().String()
	if tcp, ok := a.listener.Addr().(*net.TCPAddr); ok && tcp.IP.IsUnspecified() {
		host = fmt.Sprintf("127.0.0.1:%d", tcp.Port)
	}
	return "http://" + host
}

// Serve blocks until the server stops.
func (a *App) Serve() error {
	a.mu.Lock()
	if a.listener == nil {
		if _, err := a.listen(); err != nil {
			a.mu.Unlock()
			return err
		}
	}
	srv, ln := a.http, a.listener
	a.mu.Unlock()

	err := srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Shutdown stops accepting requests, abandons in-memory runs (the turns stay
// unfinished so the next start can continue them) and closes the database.
func (a *App) Shutdown(ctx context.Context) {
	a.mu.Lock()
	srv := a.http
	stop := a.httpStop
	a.httpStop = nil
	a.mu.Unlock()
	if stop != nil {
		stop()
	}
	if srv != nil {
		if err := srv.Shutdown(ctx); err != nil {
			_ = srv.Close()
		}
	}
	if a.Remote != nil {
		a.Remote.Stop()
	}
	a.Engine.Shutdown()
	if err := a.Store.Close(); err != nil {
		a.Logger.Warn("could not close the database", "err", err)
	}
}

// OpenBrowser points the user's browser at the running server.
func (a *App) OpenBrowser() error {
	url := a.URL()
	if url == "" {
		return errors.New("app: the server is not listening yet")
	}
	return openURL(url)
}

// launch starts a detached OS command. It is a variable so tests can check
// which command would run without actually opening a browser or a Finder
// window on the machine running them.
var launch = func(name string, args ...string) error {
	return exec.Command(name, args...).Start()
}

func openURL(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return launch("open", url)
	case "windows":
		return launch("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return launch("xdg-open", url)
	}
}

// revealFor returns the file-manager hook, but only for the desktop shell:
// spawning a file manager on the server's machine is a reasonable thing for a
// desktop app to do and an unreasonable one for a web server.
func revealFor(mode string) func(string) error {
	if mode != server.ModeDesktop {
		return nil
	}
	return revealPath
}

// openURLFor is the desktop-only browser hook. A web server must not spawn
// windows on the host; the tab already has a browser.
func openURLFor(mode string) func(string) error {
	if mode != server.ModeDesktop {
		return nil
	}
	return openURL
}

func revealPath(path string) error {
	var err error
	switch runtime.GOOS {
	case "darwin":
		err = launch("open", "-R", path)
	case "windows":
		err = launch("explorer", "/select,"+path)
	default:
		// No portable "select the file" on Linux; opening the containing
		// directory is the closest equivalent every file manager supports.
		err = launch("xdg-open", parentDir(path))
	}
	if err != nil {
		return fmt.Errorf("app: could not open the file manager: %w", err)
	}
	return nil
}

func parentDir(path string) string {
	info, err := os.Stat(path)
	if err == nil && info.IsDir() {
		return path
	}
	return filepath.Dir(path)
}
