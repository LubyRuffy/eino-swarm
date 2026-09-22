package main

import (
	"context"
	"flag"
	"fmt"
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

// runTUI attaches the terminal to the data directory's engine. It does not
// open the database or start a second swarm. With --task the process can
// exit when the turn settles; the conversation stays in the engine.
func runTUI(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	opts, err := tuiLaunch(args)
	if err != nil {
		return err
	}
	base, err := ensureEngine(opts.dataDir, "127.0.0.1:0", opts.mock)
	if err != nil {
		return err
	}
	tui.RunClient(ctx, tui.ClientConfig{
		BaseURL:     base,
		Task:        opts.task,
		Goal:        opts.goal,
		Plan:        opts.plan,
		Model:       opts.model,
		Reasoning:   opts.reasoning,
		Interactive: opts.interactive,
		Workspace:   opts.workspace,
	})
	return nil
}

type tuiLaunchOpts struct {
	dataDir     string
	mock        bool
	task        string
	goal        string
	plan        string
	model       string
	reasoning   string
	interactive bool
	workspace   string
}

func tuiLaunch(args []string) (tuiLaunchOpts, error) {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	task := fs.String("task", "", "the task to work on")
	goal := fs.String("goal", "", "standing objective to pursue until complete_goal or block_goal")
	plan := fs.String("plan", "", "explore and write a plan before changing anything")
	modelName := fs.String("model", "", "model name for this session (default: the provider's configured model)")
	reasoning := fs.String("reasoning", "", "thinking level: default, low, medium, high")
	dataDir := fs.String("data-dir", "", "data directory")
	mock := fs.Bool("mock", false, "run on the scripted offline provider")
	workspace := fs.String("workspace", "", "directory the agents may read and write")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"task": true, "goal": true, "plan": true, "model": true, "reasoning": true, "data-dir": true, "workspace": true,
	})); err != nil {
		return tuiLaunchOpts{}, err
	}
	explicit := strings.TrimSpace(*task + " " + strings.Join(fs.Args(), " "))
	text := explicit
	goalText := strings.TrimSpace(*goal)
	planText := strings.TrimSpace(*plan)
	if text == "" && planText != "" && goalText == "" {
		text = "/plan " + planText
	}
	effort, err := tuiReasoningFlag(*reasoning)
	if err != nil {
		return tuiLaunchOpts{}, err
	}
	return tuiLaunchOpts{
		dataDir:     *dataDir,
		mock:        *mock,
		task:        text,
		goal:        goalText,
		plan:        planText,
		model:       strings.TrimSpace(*modelName),
		reasoning:   effort,
		interactive: explicit == "",
		workspace:   *workspace,
	}, nil
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
	plan := fs.String("plan", "", "explore and write a plan before changing anything")
	modelName := fs.String("model", "", "model name for this session (default: the provider's configured model)")
	reasoning := fs.String("reasoning", "", "thinking level: default, low, medium, high")
	dataDir := fs.String("data-dir", "", "data directory")
	mock := fs.Bool("mock", false, "run on the scripted offline provider")
	workspace := fs.String("workspace", "", "directory the agents may read and write (default: a temporary one)")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"task": true, "goal": true, "plan": true, "model": true, "reasoning": true, "data-dir": true, "workspace": true,
	})); err != nil {
		return nil, err
	}
	// --goal with no --task is still work: the objective is the first user
	// message. Asking for both just to start was a footgun.
	explicit := strings.TrimSpace(*task + " " + strings.Join(fs.Args(), " "))
	text := explicit
	if text == "" {
		text = strings.TrimSpace(*goal)
	}
	if text == "" {
		text = strings.TrimSpace(*plan)
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

	effort, err := tuiReasoningFlag(*reasoning)
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(*modelName)
	catalog := prov.Models()
	if model == "" {
		model = strings.TrimSpace(prov.Model)
	} else if got, ok := pickSessionModel(model, catalog); ok {
		model = got
	} else if len(catalog) > 0 {
		return nil, fmt.Errorf("tui: unknown model %q", model)
	}
	if model != "" && !catalogHas(catalog, model) {
		catalog = append(append([]string{}, catalog...), model)
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
	builder, err := pool.ModelBuilder(ctx, prov.ID, model, effort, nil)
	if err != nil {
		cleanup()
		return nil, err
	}

	reg := swarm.NewRegistry()
	reg.ModelBuilder = builder
	reg.SetMaxConcurrent(cfg.Swarm.MaxConcurrent)
	reg.AgentTimeout = cfg.Swarm.AgentTimeout()
	reg.MaxTurns = cfg.Swarm.MaxTurns
	reg.ManagerMaxIterations = cfg.Swarm.ManagerIterations()
	reg.SubAgentTools = toolset.Tools
	reg.WorkerPreamble = engine.HostEnvironmentPrompt()

	host := tui.NewAskHost(stdinIsTTY())
	planState := tui.NewPlanState(cfg, toolset, engine.PersonalityPrompt(cfg.Personality.Instructions), host.Wait)

	sw := &tui.Switcher{
		Model:     model,
		Reasoning: effort,
		Catalog:   catalog,
		Levels:    append([]string{""}, config.ReasoningEfforts()...),
		Rebuild: func(nextModel, nextEffort string) error {
			b, err := pool.ModelBuilder(ctx, prov.ID, nextModel, nextEffort, nil)
			if err != nil {
				return err
			}
			reg.ModelBuilder = b
			return nil
		},
	}

	// Empty explicit task is not an error: the TUI waits at the composer.
	// --task (or leftover words) is the one-shot path that starts immediately
	// and exits. --goal / --plan alone starts immediately but keeps the composer.
	session := tui.Session{
		Registry:    reg,
		Task:        text,
		Interactive: explicit == "",
		Switcher:    sw,
		Ask:         host,
		Plan:        planState,
	}
	session.Extra = engine.PersonalityPrompt(cfg.Personality.Instructions)
	if g := strings.TrimSpace(*goal); g != "" {
		done := &atomic.Bool{}
		session.Extra = engine.JoinPromptSections(session.Extra, engine.GoalPrompt(g, false))
		stop := func(string) (string, error) {
			done.Store(true)
			return `{"ok":true}`, nil
		}
		undo := func(string) (string, error) {
			done.Store(false)
			return `{"ok":true}`, nil
		}
		planState.SetGoal(engine.GoalPrompt(g, false), []tool.BaseTool{
			engine.CompleteGoalTool(stop),
			engine.BlockGoalTool(stop),
			engine.ReopenGoalTool(undo),
		})
		session.ShouldContinue = func() bool { return !done.Load() }
		session.ContinueTask = engine.GoalContinueText()
		session.MaxContinues = cfg.Swarm.GoalAutoTurns()
		session.MaxIterations = cfg.Swarm.GoalSessionIterations()
	}
	if strings.TrimSpace(*plan) != "" {
		planState.SetOn(true)
	}
	session.ManagerTools = planState.ManagerTools()
	session.Instruction = planState.Instruction()

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

func tuiReasoningFlag(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "default") {
		return "", nil
	}
	for _, l := range config.ReasoningEfforts() {
		if strings.EqualFold(s, l) {
			return l, nil
		}
	}
	return "", fmt.Errorf("tui: unknown reasoning level %q", s)
}

func pickSessionModel(name string, catalog []string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	for _, m := range catalog {
		if m == name {
			return m, true
		}
	}
	for _, m := range catalog {
		if strings.EqualFold(m, name) {
			return m, true
		}
	}
	return "", false
}

func catalogHas(catalog []string, name string) bool {
	_, ok := pickSessionModel(name, catalog)
	return ok
}

func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
