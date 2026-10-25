package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/LubyRuffy/eino-swarm/internal/app"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// This hook exists only in the test binary; production has no environment
// switch that could delay recovery or bypass the startup admission boundary.
func runRestoreProcessHelper() int {
	fs := flag.NewFlagSet("restore-process", flag.ContinueOnError)
	dir := fs.String("data-dir", "", "")
	addr := fs.String("addr", "127.0.0.1:0", "")
	_ = fs.Bool("mock", false, "")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return 2
	}
	if os.Getenv("ZWAI_RESTORE_OLD") == "1" {
		version += "-previous"
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	delay, _ := time.ParseDuration(os.Getenv("ZWAI_RESTORE_DELAY"))
	err := serveEngine(ctx, engineOpts{DataDir: *dir, Addr: *addr, Restore: func(ctx context.Context, a *app.App) (err error) {
		probe, err := startLargeRestoreProbe(ctx, a)
		if err != nil {
			return err
		}
		if probe != nil {
			defer func() { err = errors.Join(err, probe.finish(err)) }()
		}
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return a.Restore(ctx)
	}})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
