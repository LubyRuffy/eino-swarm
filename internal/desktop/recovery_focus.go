package desktop

import (
	"context"
	"log/slog"
	"os"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/events"
)

// A successor's page may finish loading while its parent still owns the
// foreground. Only this uncommitted shell gets one guarded focus request.
func bindRecoverySuccessorFocus(ctx context.Context, host recoveryHost, c *recoveryController, logger *slog.Logger) {
	if c.opts.Ready == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	report := func(stage string) {
		logger.Info("desktop recovery focus", "session_id", c.opts.SessionID, "generation", c.opts.Generation,
			"attempt", c.opts.Attempts, "shell_pid", os.Getpid(), "stage", stage)
	}
	focusCtx, stopFocus := context.WithCancel(ctx)
	ready := c.opts.Ready
	c.opts.Ready = func(ctx context.Context) error {
		report("ready_cancelled")
		stopFocus()
		return ready(ctx)
	}
	var once sync.Once
	host.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, func() {
		once.Do(func() {
			report("navigation")
			requestRecoverySuccessorFocus(focusCtx, c.window, c.opts.ParentDone, report)
		})
	})
}
