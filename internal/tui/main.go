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

	"github.com/LubyRuffy/eino-swarm"
	tea "github.com/charmbracelet/bubbletea"
)

// Run starts the bubbletea TUI for one swarm run.
func Run(ctx context.Context, reg *swarm.Registry, task string) {

	// bridge swarm notifications into bubbletea: the swarm goroutine only
	// sends on a channel; TUI state mutates exclusively inside Update.
	tm := newModel(reg)
	notifCh := make(chan notificationMsg, 512)
	go func() {
		_, runErr := reg.Run(ctx, task, func(n swarm.Notification) {
			notifCh <- notificationMsg{Notification: n}
		})
		if runErr != nil {
			notifCh <- notificationMsg{Notification: swarm.Notification{
				Kind: swarm.NotifyError, AgentID: swarm.DefaultManagerID, Err: runErr}}
		}
		close(notifCh)
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

// finalOf picks which model to print. bubbletea works on copies of the model,
// so a worker spawned mid-run only exists on the one it hands back; printing
// the model we started with would lose every worker.
func finalOf(started swarmTUI, ended tea.Model) swarmTUI {
	if fm, ok := ended.(swarmTUI); ok {
		return fm
	}
	return started
}
