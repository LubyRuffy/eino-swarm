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
// across auto-continues.
type Session struct {
	Registry       *swarm.Registry
	Task           string
	Extra          string
	ManagerTools   []tool.BaseTool
	ShouldContinue func() bool
	ContinueTask   string
	MaxContinues   int
}

// Run starts the bubbletea TUI for one swarm run.
func Run(ctx context.Context, reg *swarm.Registry, task string) {
	RunSession(ctx, Session{Registry: reg, Task: task})
}

// RunSession starts the TUI and, when ShouldContinue says so, feeds the
// previous transcript back for another RunWith instead of exiting. The
// one-shot TUI has no conversation store; this is how a --goal keeps going.
func RunSession(ctx context.Context, s Session) {
	tm := newModel(s.Registry)
	notifCh := make(chan notificationMsg, 512)
	go func() {
		defer close(notifCh)
		task := s.Task
		var messages []adk.Message
		extraRuns := 0
		for {
			cfg := sessionConfig(s, task, messages)
			res, runErr := s.Registry.RunWith(ctx, cfg, func(n swarm.Notification) {
				notifCh <- notificationMsg{Notification: n}
			})
			if runErr != nil {
				notifCh <- notificationMsg{Notification: swarm.Notification{
					Kind: swarm.NotifyError, AgentID: swarm.DefaultManagerID, Err: runErr}}
				return
			}
			if s.ShouldContinue == nil || !s.ShouldContinue() {
				return
			}
			if extraRuns >= s.MaxContinues {
				return
			}
			extraRuns++
			messages = dropSystem(res.Transcript)
			if text := strings.TrimSpace(s.ContinueTask); text != "" {
				messages = append(messages, schema.UserMessage(text))
				task = text
			}
		}
	}()
	tm.notifications = notifCh

	finalModel, progErr := tea.NewProgram(tm, tea.WithAltScreen()).Run()
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

// runConfig is the one-shot RunWith payload. The TUI uses the task as the
// manager instruction; WorkerPreamble has to ride in front or the manager
// would not know which OS it is on either.
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
	cfg := runConfig(s.Registry, task)
	if extra := strings.TrimSpace(s.Extra); extra != "" {
		cfg.Instruction = extra + "\n\n" + cfg.Instruction
	}
	cfg.ManagerTools = s.ManagerTools
	if len(messages) > 0 {
		cfg.Messages = messages
	}
	return cfg
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
