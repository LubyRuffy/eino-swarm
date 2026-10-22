package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/frontend"
	"github.com/LubyRuffy/eino-swarm/internal/app"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
	"github.com/LubyRuffy/eino-swarm/internal/server"
)

// activeEngineStop cancels the in-process engine tests start. Production
// shells spawn a child and must not call this: closing a window is not
// shutting the engine down.
var activeEngineStop func()

// Tests replace the process launcher with the same engine running in-process.
var launchEngine = spawnEngineContext

var loadEngineFrontend = frontend.Load

func runEngine(args []string) error {
	fs := flag.NewFlagSet("engine", flag.ExitOnError)
	dataDir := fs.String("data-dir", "", "data directory")
	addr := fs.String("addr", "", "listen address")
	mock := fs.Bool("mock", false, "run on the scripted offline provider")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"data-dir": true, "addr": true})); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serveEngine(ctx, engineOpts{
		DataDir: *dataDir,
		Addr:    *addr,
		Mock:    *mock,
	})
}

type engineOpts struct {
	DataDir string
	Addr    string
	Mock    bool
	Watch   lease.Watcher
	Ready   func(url string)
}

// serveEngine is the long-lived process. It holds the data-dir lock, serves
// the same App the window used to own, and returns when the idle rule says
// nobody is left or when ctx is cancelled.
func serveEngine(ctx context.Context, opts engineOpts) error {
	cfg, err := config.Load(opts.DataDir)
	if err != nil {
		return err
	}
	dir := cfg.DataDir()
	hold, err := lease.Acquire(dir)
	if err != nil {
		return err
	}
	defer hold.Release()

	addr := opts.Addr
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	a, err := assembleApp(app.Options{
		DataDir: dir,
		Addr:    addr,
		Mock:    opts.Mock,
		Mode:    server.ModeEngine,
		Version: version,
	})
	if err != nil {
		return err
	}
	url, err := a.Listen()
	if err != nil {
		shut(a)
		return err
	}
	build := lease.ThisBuild(version)
	if err := lease.Publish(dir, lease.Record{
		PID: os.Getpid(), URL: url, Mock: opts.Mock, StartedAt: time.Now().UTC(),
		Version: build.Version, Dev: build.Dev, Ino: build.Ino,
	}); err != nil {
		shut(a)
		return err
	}
	errCh := make(chan error, 1)
	go func() { errCh <- a.Serve() }()

	if opts.Ready != nil {
		opts.Ready(url)
	}
	// Tests stop the engine by cancelling ctx. The real idle grace would
	// race a slow test that has not opened a presence connection yet.
	watch := opts.Watch
	if testing.Testing() && watch.StartGrace == 0 && watch.IdleGrace == 0 && watch.Poll == 0 && watch.Now == nil {
		watch.StartGrace = time.Hour
		watch.IdleGrace = time.Hour
	}
	started := time.Now()
	watch.StopIfIdle = a.Server.TryIdleShutdown
	watch.Run(ctx, started, func() (int, bool, int) {
		phone := a.Remote != nil && a.Remote.Status().Online
		return len(a.Server.Clients()), phone, a.Engine.ActiveWork()
	})
	shut(a)
	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	case <-time.After(shutdownGrace):
	}
	return nil
}

func shut(a *app.App) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	a.Shutdown(ctx)
}

// ensureEngine attaches to a live engine or starts one. A mock mismatch or a
// process that will not answer is an error: starting a second engine on the
// same database is the bug this exists to prevent.
func ensureEngine(dataDir, addr string, mock bool) (string, error) {
	rec, err := ensureEngineRecord(dataDir, addr, mock)
	return rec.URL, err
}

func ensureEngineRecord(dataDir, addr string, mock bool) (lease.Record, error) {
	cfg, err := config.Load(dataDir)
	if err != nil {
		return lease.Record{}, err
	}
	dir := cfg.DataDir()
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	// Prepare checkout assets while the old engine can still serve its clients.
	// Package installation must not consume its bounded handoff/recovery window.
	if _, err := loadEngineFrontend(ctx); err != nil {
		return lease.Record{}, fmt.Errorf("prepare frontend before engine startup: %w", err)
	}
	started := time.Now()
	restarting := false
	for {
		if err := ctx.Err(); err != nil {
			return lease.Record{}, fmt.Errorf("engine launch stopped: %w", err)
		}
		rec, err := lease.Discover(dir, mock)
		if ctx.Err() != nil {
			continue
		}
		// The lock is taken before engine.json exists. Wait for that
		// record, then apply the same build and active-turn checks as an
		// engine that was already listening when this shell opened.
		if err != nil && !errors.Is(err, lease.ErrMockMismatch) && !errors.Is(err, lease.ErrUnreachable) {
			if started, held, waitErr := discoverIfStartingContext(ctx, dir, mock); held {
				if waitErr != nil {
					return lease.Record{}, waitErr
				}
				rec, err = started, nil
			}
		}
		switch {
		case err == nil:
			if lease.SameEngine(rec, lease.ThisBuild(version)) {
				if restarting {
					log.Printf("engine restarted in %s; continuing tasks from saved progress", time.Since(started).Round(time.Millisecond))
				}
				return rec, nil
			}
			if !restarting {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, started.Add(restartBudget))
				defer cancel()
				restarting = true
			}
			if err := replaceEngine(ctx, rec); err != nil {
				if _, discErr := lease.Discover(dir, mock); !errors.Is(discErr, lease.ErrNotRunning) {
					return lease.Record{}, err
				}
			}
			continue
		case errors.Is(err, lease.ErrMockMismatch), errors.Is(err, lease.ErrUnreachable):
			return lease.Record{}, err
		}
		if _, err := launchEngine(ctx, dir, addr, mock); err != nil {
			return lease.Record{}, err
		}
		// A competing holder may have won while our child started. Its
		// published identity must pass the checks above before attaching.
	}
}

func engineBusy(client *http.Client, base string) (bool, error) {
	resp, err := client.Get(base + "/api/threads")
	if err != nil {
		return false, fmt.Errorf("engine: list conversations: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false, err
	}
	// An engine that does not list conversations cannot be shown to be
	// mid-call. Holding the replacement forever would leave the old binary
	// in place, which is the failure this wait exists to avoid.
	if resp.StatusCode != http.StatusOK {
		return false, nil
	}
	var page struct {
		Threads []struct {
			Running        bool `json:"running"`
			AwaitingAnswer bool `json:"awaiting_answer"`
		} `json:"threads"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return false, fmt.Errorf("engine: list conversations: %w", err)
	}
	for _, th := range page.Threads {
		if th.Running && !th.AwaitingAnswer {
			return true, nil
		}
	}
	return false, nil
}

func startInProcessEngineContext(parent context.Context, dir, addr string, mock bool) (string, error) {
	if err := parent.Err(); err != nil {
		return "", err
	}
	boot, stopBoot := context.WithTimeout(parent, engineBootWait)
	defer stopBoot()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- serveEngine(ctx, engineOpts{
			DataDir: dir, Addr: addr, Mock: mock,
			Ready: func(url string) { ready <- url },
		})
	}()
	select {
	case url := <-ready:
		activeEngineStop = cancel
		return url, nil
	case err := <-errCh:
		cancel()
		if err == nil {
			err = errors.New("engine stopped before it was listening")
		}
		return "", err
	case <-boot.Done():
		cancel()
		return "", fmt.Errorf("engine did not start: %w", boot.Err())
	}
}
