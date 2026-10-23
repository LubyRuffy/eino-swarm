package desktop

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type recoveryHost interface {
	Context() context.Context
	Activity() recoveryWindowActivity
	Window() application.Window
	OnShutdown(func())
	OnWindowEvent(events.WindowEventType, func())
	OnApplicationEvent(events.ApplicationEventType, func())
	Probe(string)
	Snapshot(context.Context) ([]byte, error)
	Reload()
	Quit()
	ShowError()
}

type recoveryWindowActivity struct {
	focused   bool
	minimized bool
}

type wailsRecoveryHost struct {
	app    *application.App
	window application.Window
}

func (h wailsRecoveryHost) Context() context.Context { return h.app.Context() }
func (h wailsRecoveryHost) Activity() recoveryWindowActivity {
	return recoveryWindowActivity{focused: h.window.IsFocused(), minimized: h.window.IsMinimised()}
}
func (h wailsRecoveryHost) Window() application.Window { return h.window }
func (h wailsRecoveryHost) OnShutdown(fn func())       { h.app.OnShutdown(fn) }
func (h wailsRecoveryHost) OnWindowEvent(event events.WindowEventType, fn func()) {
	h.window.OnWindowEvent(event, func(*application.WindowEvent) { fn() })
}
func (h wailsRecoveryHost) OnApplicationEvent(event events.ApplicationEventType, fn func()) {
	h.app.Event.OnApplicationEvent(event, func(*application.ApplicationEvent) { fn() })
}
func (h wailsRecoveryHost) Probe(script string) { executeRecoveryProbe(h.window, script) }
func (h wailsRecoveryHost) Snapshot(ctx context.Context) ([]byte, error) {
	return captureRecoveryPixels(ctx, h.window)
}
func (h wailsRecoveryHost) Reload() { h.window.Reload() }
func (h wailsRecoveryHost) Quit()   { application.InvokeAsync(h.app.Quit) }
func (h wailsRecoveryHost) ShowError() {
	h.app.Dialog.Error().SetTitle("界面恢复失败").SetMessage("界面连续恢复失败，已停止自动重启。本次恢复没有停止后台引擎。请关闭此窗口后重新打开，并保留恢复诊断以便排查。").AttachToWindow(h.window).Show()
}
