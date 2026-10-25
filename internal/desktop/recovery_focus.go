package desktop

import (
	"context"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/events"
)

// A successor's page may finish loading while its parent still owns the
// foreground. Only this uncommitted shell gets one guarded focus request.
func bindRecoverySuccessorFocus(ctx context.Context, host recoveryHost, c *recoveryController) {
	if c.opts.Ready == nil {
		return
	}
	focusCtx, stopFocus := context.WithCancel(ctx)
	ready := c.opts.Ready
	c.opts.Ready = func(ctx context.Context) error {
		stopFocus()
		return ready(ctx)
	}
	var once sync.Once
	host.OnWindowEvent(events.Mac.WebViewDidFinishNavigation, func() {
		once.Do(func() { requestRecoverySuccessorFocus(focusCtx, c.window, c.opts.ParentDone) })
	})
}
