package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/LubyRuffy/eino-swarm/internal/tui"
	"github.com/cloudwego/eino/components/tool"
)

// runTUI runs one task in the terminal. It is the same swarm the app runs,
// with the same configuration and the same toolset — only the presentation
// differs — so a task that misbehaves in the UI can be reproduced here.
func runTUI(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	setup, err := assembleTUI(ctx, args)
	if err != nil {
		return err
	}
	defer setup.cleanup()

	tui.RunSession(ctx, setup.session)
	return nil
}

type tuiSetup struct {
	session tui.Session
	cleanup func()
}

// assembleTUI builds the terminal swarm. buildTUISwarm is the older three-value
// wrapper tests already call; this is the one that can carry a --goal.
func assembleTUI(ctx context.Context, args []string) (*tuiSetup, error) {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	task := fs.String("task", "", "the task to work on")
	goal := fs.String("goal", "", "standing objective to pursue until complete_goal or block_goal")
	dataDir := fs.String("data-dir", "", "data directory")
	mock := fs.Bool("mock", false, "run on the scripted offline provider")
	workspace := fs.String("workspace", "", "directory the agents may read and write (default: a temporary one)")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"task": true, "goal": true, "data-dir": true, "workspace": true,
	})); err != nil {
		return nil, err
	}
	text := strings.TrimSpace(*task + " " + strings.Join(fs.Args(), " "))
	if text == "" {
		return nil, errors.New(`tui needs a task: zwai tui --task "..."`)
	}

	cfg, err := config.Load(*dataDir)
	if err != nil {
		return nil, err
	}
	pool := provider.New(cfg)
	if *mock {
		pool = provider.NewMock(cfg)
	}
	prov, err := pool.Resolve("")
	if err != nil {
		return nil, err
	}

	dir := *workspace
	cleanup := func() {}
	if dir == "" {
		// A terminal run is not a saved conversation, so it gets a scratch
		// workspace rather than leaving files in the data directory.
		dir, err = os.MkdirTemp("", "zwai-tui-*")
		if err != nil {
			return nil, err
		}
		cleanup = func() { _ = os.RemoveAll(dir) }
	}
	toolset, err := tools.Build(ctx, cfg, dir)
	if err != nil {
		cleanup()
		return nil, err
	}
	builder, err := pool.ModelBuilder(ctx, prov.ID, "", "", nil)
	if err != nil {
		cleanup()
		return nil, err
	}

	reg := swarm.NewRegistry()
	reg.ModelBuilder = builder
	reg.MaxConcurrent = cfg.Swarm.MaxConcurrent
	reg.AgentTimeout = cfg.Swarm.AgentTimeout()
	reg.MaxTurns = cfg.Swarm.MaxTurns
	reg.ManagerMaxIterations = cfg.Swarm.ManagerIterations()
	reg.SubAgentTools = toolset.Tools
	reg.WorkerPreamble = engine.HostEnvironmentPrompt()

	session := tui.Session{Registry: reg, Task: text}
	if g := strings.TrimSpace(*goal); g != "" {
		done := &atomic.Bool{}
		session.Extra = engine.GoalPrompt(g, false)
		stop := func(string) (string, error) {
			done.Store(true)
			return `{"ok":true}`, nil
		}
		session.ManagerTools = []tool.BaseTool{
			engine.CompleteGoalTool(stop),
			engine.BlockGoalTool(stop),
		}
		session.ShouldContinue = func() bool { return !done.Load() }
		session.ContinueTask = engine.GoalContinueText()
		session.MaxContinues = cfg.Swarm.GoalAutoTurns()
	}

	return &tuiSetup{
		session: session,
		cleanup: func() { reg.Close(); cleanup() },
	}, nil
}

// buildTUISwarm assembles the same swarm the app runs — same configuration,
// same toolset — so a task that misbehaves in the UI can be reproduced in the
// terminal. It is separate from runTUI because everything here can fail and
// be checked, while the bubbletea program cannot run in a test.
func buildTUISwarm(ctx context.Context, args []string) (*swarm.Registry, string, func(), error) {
	setup, err := assembleTUI(ctx, args)
	if err != nil {
		return nil, "", nil, err
	}
	return setup.session.Registry, setup.session.Task, setup.cleanup, nil
}
