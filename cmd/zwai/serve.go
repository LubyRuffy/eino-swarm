package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/LubyRuffy/eino-swarm/frontend"
	"github.com/LubyRuffy/eino-swarm/internal/app"
	"github.com/LubyRuffy/eino-swarm/internal/desktop"
	"github.com/LubyRuffy/eino-swarm/internal/server"
)

// shutdownGrace is how long in-flight requests get to finish before the
// process stops waiting for them.
const shutdownGrace = 5 * time.Second

func runDesktop(args []string) error {
	// The app is shut down by opts.OnShutdown when the window closes.
	opts, _, err := startDesktopServer(args)
	if err != nil {
		return err
	}
	return desktop.Run(opts)
}

// startDesktopServer does everything the desktop command does except open the
// window: load the config, start the local server, and describe the window to
// open. Splitting it out keeps the part that can fail testable, because the
// part that cannot be tested is a native window.
func startDesktopServer(args []string) (desktop.Options, *app.App, error) {
	fs := flag.NewFlagSet("desktop", flag.ExitOnError)
	dataDir := fs.String("data-dir", "", "data directory")
	mock := fs.Bool("mock", false, "run on the scripted offline provider")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"data-dir": true})); err != nil {
		return desktop.Options{}, nil, err
	}

	a, err := assembleApp(app.Options{
		DataDir: *dataDir,
		// A random loopback port: a fixed one collides with whatever else the
		// user is running, and the window is told the URL anyway.
		Addr:    "127.0.0.1:0",
		Mock:    *mock,
		Mode:    server.ModeDesktop,
		Version: version,
	})
	if err != nil {
		return desktop.Options{}, nil, err
	}
	url, err := a.Listen()
	if err != nil {
		return desktop.Options{}, nil, err
	}
	go func() {
		if err := a.Serve(); err != nil {
			a.Logger.Error("the local server stopped", "err", err)
		}
	}()

	return desktop.Options{
		URL:     url,
		Title:   "zwai",
		Version: version,
		Logger:  a.Logger,
		OnShutdown: func() {
			ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
			defer cancel()
			a.Shutdown(ctx)
		},
	}, a, nil
}

func runWeb(args []string) error {
	// Ctrl-C has to stop in-memory runs and close the database. Unfinished
	// turns stay marked running so the next start continues them.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(stop)
	return serveWeb(args, stop, nil)
}

// serveWeb is runWeb with its two environmental dependencies injected: the
// stop signal and a hook fired once the server is listening. Tests drive both
// instead of sending themselves signals.
func serveWeb(args []string, stop <-chan os.Signal, ready func(url string)) error {
	fs := flag.NewFlagSet("web", flag.ExitOnError)
	addr := fs.String("addr", "", "listen address (default: the configured one)")
	dataDir := fs.String("data-dir", "", "data directory")
	mock := fs.Bool("mock", false, "run on the scripted offline provider")
	noOpen := fs.Bool("no-open", false, "do not open a browser")
	if err := fs.Parse(args); err != nil {
		return err
	}

	a, err := assembleApp(app.Options{
		DataDir: *dataDir,
		Addr:    *addr,
		Mock:    *mock,
		Mode:    server.ModeWeb,
		Version: version,
	})
	if err != nil {
		return err
	}
	url, err := a.Listen()
	if err != nil {
		return err
	}
	fmt.Println("zwai is running at", url)
	if !*noOpen && a.Config.Server.OpenBrowser {
		if err := a.OpenBrowser(); err != nil {
			a.Logger.Warn("could not open a browser", "err", err)
		}
	}

	errs := make(chan error, 1)
	go func() { errs <- a.Serve() }()
	if ready != nil {
		ready(url)
	}

	select {
	case err := <-errs:
		return err
	case <-stop:
		fmt.Println("\nstopping…")
		ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		a.Shutdown(ctx)
		return nil
	}
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
