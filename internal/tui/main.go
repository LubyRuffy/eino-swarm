// Package tui provides the bubbletea terminal UI for the swarm: left pane =
// selected agent transcript (collapsible thinking/tool blocks), right pane =
// sub-agent roster with live status. After the run finishes and the TUI
// closes, the full transcript is printed to the normal screen so the output
// survives the altscreen restore.
package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/LubyRuffy/eino-swarm"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Session is one terminal run. The one-shot helper is Run; a standing
// objective uses RunSession so complete_goal can keep the same TUI open
// across auto-continues. Interactive is the Claude/Codex-style composer:
// zwai tui with no --task waits for typed turns instead of exiting.
// --goal with no --task still Interactive, but Task is the objective so
// the first turn starts immediately.
type Session struct {
	Registry *swarm.Registry
	Task     string
	// Instruction is the manager's system prompt. Empty falls back to the
	// host snapshot plus the task, which is only for tests that do not
	// assemble a real toolset.
	Instruction    string
	Extra          string
	ManagerTools   []tool.BaseTool
	ShouldContinue func() bool
	ContinueTask   string
	MaxContinues   int
	MaxIterations  int
	Interactive    bool
	Switcher       *Switcher
	Ask            *AskHost
	Plan           *PlanState
}

// Run starts the bubbletea TUI for one swarm run.
func Run(ctx context.Context, reg *swarm.Registry, task string) {
	RunSession(ctx, Session{Registry: reg, Task: task})
}

// RunSession starts the TUI and, when ShouldContinue says so, feeds the
// previous transcript back for another RunWith instead of exiting. The
// one-shot TUI has no conversation store; this is how a --goal keeps going.
// Interactive mode keeps the altscreen open after a turn and waits for the
// next typed task. Leaving the TUI cancels the session loop so it cannot
// sit blocked on the composer channel.
func RunSession(ctx context.Context, s Session) {
	ctx, stop := context.WithCancel(ctx)
	defer stop()

	tm := newModel(s.Registry)
	tm.askHost = s.Ask
	tm.plan = s.Plan
	notifCh := make(chan notificationMsg, 512)
	var prompts chan string
	if s.Interactive {
		prompts = tm.openComposer(s.Task)
	}
	tm.choice = s.Switcher
	go pumpSession(ctx, s, registryRun(s.Registry), notifCh, prompts)
	tm.notifications = notifCh

	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if s.Interactive {
		tm.ime = &imeAnchor{}
		opts = append(opts, tea.WithOutput(newIMEWriter(os.Stdout, tm.ime)))
	}

	finalModel, progErr := tea.NewProgram(tm, opts...).Run()
	if progErr != nil {
		fmt.Fprintln(os.Stderr, "error:", progErr)
	}
	// altscreen restored: print a durable transcript so the run output
	// survives after exit.
	final := finalOf(tm, finalModel)
	fmt.Println()
	fmt.Println(final.DumpTranscript())
	if final.finErr != nil {
		fmt.Fprintln(os.Stderr, "error:", final.finErr)
		os.Exit(1)
	}
}

// openComposer arms the idle prompt. A non-empty task means this session
// already has work, so mark busy and show that first user line — otherwise
// --goal flashes "Waiting for a task" until the first token.
func (m *swarmTUI) openComposer(task string) chan string {
	prompts := make(chan string)
	m.interactive = true
	m.prompts = prompts
	if text := strings.TrimSpace(task); text != "" {
		m.busy = true
		m.manager.blocks = append(m.manager.blocks, &block{
			kind:    blockUser,
			agentID: m.manager.id,
			answer:  text,
			open:    true,
		})
	}
	return prompts
}

// runConfig is the fallback RunWith payload when Session.Instruction is
// empty. The app and zwai tui set Instruction to engine.ManagerPrompt;
// stuffing the user task into the system prompt is how the manager used
// to think it had no tools.
func runConfig(reg *swarm.Registry, task string) swarm.RunConfig {
	instruction := task
	if reg != nil {
		if p := strings.TrimSpace(reg.WorkerPreamble); p != "" {
			instruction = p + "\n\n" + task
		}
	}
	return swarm.RunConfig{Instruction: instruction, Task: task}
}

func sessionConfig(s Session, task string, messages []adk.Message) swarm.RunConfig {
	tools := s.ManagerTools
	if s.Plan != nil {
		tools = s.Plan.ManagerTools()
	}
	cfg := swarm.RunConfig{Task: task, ManagerTools: tools}
	if s.MaxIterations > 0 {
		cfg.MaxIterations = s.MaxIterations
	}
	if len(messages) > 0 {
		cfg.Messages = messages
	}
	if s.Plan != nil {
		if inst := strings.TrimSpace(s.Plan.Instruction()); inst != "" {
			cfg.Instruction = inst
			return cfg
		}
	}
	if inst := strings.TrimSpace(s.Instruction); inst != "" {
		cfg.Instruction = inst
		return cfg
	}
	cfg.Instruction = runConfig(s.Registry, task).Instruction
	if extra := strings.TrimSpace(s.Extra); extra != "" {
		cfg.Instruction = extra + "\n\n" + cfg.Instruction
	}
	return cfg
}

func sessionHitIterationCap(parent context.Context, runErr error) bool {
	if parent.Err() != nil || runErr == nil {
		return false
	}
	return strings.Contains(strings.ToLower(runErr.Error()), "max iteration")
}

func dropSystem(msgs []adk.Message) []adk.Message {
	out := make([]adk.Message, 0, len(msgs))
	for _, m := range msgs {
		if m == nil || m.Role == schema.System {
			continue
		}
		out = append(out, m)
	}
	return out
}

// finalOf picks which model to print. bubbletea works on copies of the model,
// so a worker spawned mid-run only exists on the one it hands back; printing
// the model we started with would lose every worker.
func finalOf(started swarmTUI, ended tea.Model) swarmTUI {
	if fm, ok := ended.(swarmTUI); ok {
		return fm
	}
	return started
}
