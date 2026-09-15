package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/LubyRuffy/eino-swarm/internal/tui"
)

// runTUI runs one task in the terminal. It is the same swarm the app runs,
// with the same configuration and the same toolset — only the presentation
// differs — so a task that misbehaves in the UI can be reproduced here.
func runTUI(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	reg, text, cleanup, err := buildTUISwarm(ctx, args)
	if err != nil {
		return err
	}
	defer cleanup()

	tui.Run(ctx, reg, text)
	return nil
}

// buildTUISwarm assembles the same swarm the app runs — same configuration,
// same toolset — so a task that misbehaves in the UI can be reproduced in the
// terminal. It is separate from runTUI because everything here can fail and
// be checked, while the bubbletea program cannot run in a test.
func buildTUISwarm(ctx context.Context, args []string) (*swarm.Registry, string, func(), error) {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	task := fs.String("task", "", "the task to work on")
	dataDir := fs.String("data-dir", "", "data directory")
	mock := fs.Bool("mock", false, "run on the scripted offline provider")
	workspace := fs.String("workspace", "", "directory the agents may read and write (default: a temporary one)")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"task": true, "data-dir": true, "workspace": true,
	})); err != nil {
		return nil, "", nil, err
	}
	text := strings.TrimSpace(*task + " " + strings.Join(fs.Args(), " "))
	if text == "" {
		return nil, "", nil, errors.New(`tui needs a task: zwai tui --task "..."`)
	}

	cfg, err := config.Load(*dataDir)
	if err != nil {
		return nil, "", nil, err
	}
	pool := provider.New(cfg)
	if *mock {
		pool = provider.NewMock(cfg)
	}
	prov, err := pool.Resolve("")
	if err != nil {
		return nil, "", nil, err
	}

	dir := *workspace
	cleanup := func() {}
	if dir == "" {
		// A terminal run is not a saved conversation, so it gets a scratch
		// workspace rather than leaving files in the data directory.
		dir, err = os.MkdirTemp("", "zwai-tui-*")
		if err != nil {
			return nil, "", nil, err
		}
		cleanup = func() { _ = os.RemoveAll(dir) }
	}
	toolset, err := tools.Build(ctx, cfg, dir)
	if err != nil {
		cleanup()
		return nil, "", nil, err
	}
	builder, err := pool.ModelBuilder(ctx, prov.ID, nil)
	if err != nil {
		cleanup()
		return nil, "", nil, err
	}

	reg := swarm.NewRegistry()
	reg.ModelBuilder = builder
	reg.MaxConcurrent = cfg.Swarm.MaxConcurrent
	reg.AgentTimeout = cfg.Swarm.AgentTimeout()
	reg.MaxTurns = cfg.Swarm.MaxTurns
	reg.SubAgentTools = toolset.Tools

	return reg, text, func() { reg.Close(); cleanup() }, nil
}
