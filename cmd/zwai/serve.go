package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/LubyRuffy/eino-swarm/frontend"
	"github.com/LubyRuffy/eino-swarm/internal/app"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/desktop"
)

// shutdownGrace is how long in-flight requests get to finish before the
// process stops waiting for them.
const shutdownGrace = 5 * time.Second

func runDesktop(args []string) error {
	// macOS Local Network privacy keys off a bundle id. `go run` is a naked
	// binary (identifier a.out); LAN model endpoints then fail with
	// "no route to host" while Terminal curl works. Re-exec into a cached
	// .app before we bind the local server.
	if err := desktop.ReexecIfUnbundled(); err != nil {
		return err
	}
	// Closing the window leaves the engine running. Later shells are attached
	// to that process, not to this one.
	opts, err := startDesktopServer(args)
	if err != nil {
		return err
	}
	return desktop.Run(opts)
}

// startDesktopServer resolves the engine URL and describes the window. It does
// not own the engine: OnShutdown must not shut it down.
func startDesktopServer(args []string) (desktop.Options, error) {
	fs := flag.NewFlagSet("desktop", flag.ExitOnError)
	dataDir := fs.String("data-dir", "", "data directory")
	mock := fs.Bool("mock", false, "run on the scripted offline provider")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"data-dir": true})); err != nil {
		return desktop.Options{}, err
	}
	raw, err := ensureEngine(*dataDir, "127.0.0.1:0", *mock)
	if err != nil {
		return desktop.Options{}, err
	}
	return desktop.Options{
		URL:     shellURL(raw, "desktop"),
		Title:   "zwai",
		Version: version,
		OnShutdown: func() {
			// The window is a client. The engine exits on its own when no
			// shell, phone, or turn is left.
		},
	}, nil
}

func shellURL(base, shell string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	q := u.Query()
	q.Set("shell", shell)
	u.RawQuery = q.Encode()
	return u.String()
}

func runWeb(args []string) error {
	// Ctrl-C stops this waiter. It does not stop the engine while another
	// shell, a phone, or a turn is still using it.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	return serveWeb(args, stop, nil)
}

// serveWeb attaches to the engine (starting one if needed), prints the URL,
// and blocks until stop. The engine process is not shut down here.
func serveWeb(args []string, stop <-chan os.Signal, ready func(url string)) error {
	fs := flag.NewFlagSet("web", flag.ExitOnError)
	addr := fs.String("addr", "", "listen address (default: the configured one)")
	dataDir := fs.String("data-dir", "", "data directory")
	mock := fs.Bool("mock", false, "run on the scripted offline provider")
	noOpen := fs.Bool("no-open", false, "do not open a browser")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*dataDir)
	if err != nil {
		return err
	}
	listen, err := ensureEngine(*dataDir, *addr, *mock)
	if err != nil {
		return err
	}
	fmt.Println("zwai is running at", listen)
	if !*noOpen && cfg.Server.OpenBrowser {
		if err := app.OpenURL(listen); err != nil {
			fmt.Fprintln(os.Stderr, "zwai: could not open a browser:", err)
		}
	}
	if ready != nil {
		ready(listen)
	}
	<-stop
	fmt.Println("\nstopping…")
	return nil
}

// assembleApp rebuilds the UI from a checkout when the sources changed,
// then wires the process. `go:embed` is a compile-time snapshot: a clone
// that has only dist/.gitkeep would otherwise serve a 404 until the next
// `go build`. Under `go test`, Load returns the embed and does not spawn npm.
func assembleApp(opts app.Options) (*app.App, error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	assets, err := frontend.Load(ctx)
	if err != nil {
		return nil, err
	}
	opts.Assets = assets
	return app.New(opts)
}
