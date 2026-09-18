package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/components/tool"
)

const tuiPlanThread = "tui"

// PlanState is the terminal session's planning flag and PLAN.md. The app
// keeps those on the conversation; this shell has no store.
type PlanState struct {
	mu        sync.Mutex
	on        bool
	markdown  string
	path      string
	cfg       *config.Config
	toolset   *tools.Set
	extraBase string
	goalExtra string
	goalTools []tool.BaseTool
	goalHold  bool
	waitAsk   func(context.Context, []engine.AskQuestion) (engine.AskAnswers, error)
}

func NewPlanState(cfg *config.Config, set *tools.Set, extra string, waitAsk func(context.Context, []engine.AskQuestion) (engine.AskAnswers, error)) *PlanState {
	path := ""
	if cfg != nil {
		path = cfg.ThreadPlanFile(tuiPlanThread)
	}
	return &PlanState{
		cfg:       cfg,
		toolset:   set,
		extraBase: extra,
		path:      path,
		waitAsk:   waitAsk,
	}
}

func (p *PlanState) On() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.on
}

func (p *PlanState) GoalHeld() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.goalHold
}

func (p *PlanState) Markdown() string {
	if p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.markdown
}

func (p *PlanState) SetGoal(extra string, tools []tool.BaseTool) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.goalExtra = extra
	p.goalTools = tools
}

func (p *PlanState) SetOn(on bool) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.on = on
	if on {
		p.goalHold = true
	}
}

func (p *PlanState) SetMarkdown(markdown string) error {
	if p == nil {
		return nil
	}
	markdown = strings.TrimSpace(markdown)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.markdown = markdown
	if p.path == "" || markdown == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p.path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p.path, []byte(markdown), 0o600)
}

func (p *PlanState) ManagerTools() []tool.BaseTool {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	set := p.toolset
	if p.on {
		set = tools.ExploreOnly(set)
	}
	var ts []tool.BaseTool
	if set != nil {
		ts = append(ts, set.Tools...)
	}
	if p.on {
		ts = dropPlanMutating(ts)
		ts = append(ts, engine.ProposePlanTool(func(md string) (string, error) {
			if err := p.storeMarkdown(md); err != nil {
				return "", err
			}
			return `{"ok":true}`, nil
		}))
	} else {
		ts = append(ts, p.goalTools...)
	}
	ts = append(ts, engine.AskUserTool(p.waitAsk))
	return ts
}

func (p *PlanState) Instruction() string {
	if p == nil {
		return ""
	}
	p.mu.Lock()
	on, md, extra, goal, set := p.on, p.markdown, p.extraBase, p.goalExtra, p.toolset
	p.mu.Unlock()
	if on {
		set = tools.ExploreOnly(set)
	}
	joined := engine.JoinPromptSections(extra, goal, engine.PlanPrompt(on, md))
	return engine.ManagerPrompt(set, p.cfg, joined)
}

func (p *PlanState) storeMarkdown(markdown string) error {
	// propose_plan runs without the SetOn lock; SetMarkdown takes it.
	return p.SetMarkdown(markdown)
}

func dropPlanMutating(ts []tool.BaseTool) []tool.BaseTool {
	drop := map[string]bool{
		memory.ToolMemory:      true,
		memory.ToolSkillManage: true,
	}
	for _, name := range tools.MutatingCatalogNames() {
		drop[name] = true
	}
	var out []tool.BaseTool
	for _, t := range ts {
		if t == nil {
			continue
		}
		info, err := t.Info(context.Background())
		if err != nil || info == nil || drop[info.Name] {
			continue
		}
		out = append(out, t)
	}
	return out
}
