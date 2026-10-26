// Package desktop is the native window around the same HTTP server the
// browser uses.
//
// The window loads http://127.0.0.1:<port> rather than a wails:// asset
// protocol on purpose: the event stream, multipart uploads and downloads then
// run on exactly one code path in both shells, instead of a webview-specific
// copy that quietly diverges.
package desktop

import (
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Options configures the window.
type Options struct {
	// URL is the already-listening local server to load.
	URL string
	// Title is the window title.
	Title string
	// Version appears in the about box.
	Version string
	// Logger receives window lifecycle messages and terminal connection causes.
	Logger *slog.Logger
	// OnShutdown releases this native client's presence when the app closes.
	// Running turns and other connected clients keep the shared engine alive.
	OnShutdown func()
	// Recovery is present only when the launcher can safely reattach a successor.
	Recovery          *RecoveryOptions
	rawMessageHandler func(application.Window, string, *application.OriginInfo)
	// Fatal reports a terminal loss of this native client's engine connection.
	// Run records the cause before native Quit; returning platforms preserve it.
	Fatal <-chan error
}

// Run opens the window and blocks until the user quits.
func Run(opts Options) error {
	if opts.Title == "" {
		opts.Title = "zwai"
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	var recovery atomic.Pointer[recoveryController]
	opts.rawMessageHandler = func(window application.Window, message string, origin *application.OriginInfo) {
		if controller := recovery.Load(); controller != nil {
			controller.raw(window, message, origin)
		}
	}
	app := application.New(appOptions(opts))

	win := app.Window.NewWithOptions(windowOptions(opts))

	keepTrafficLightsAligned(win)
	controller := startRendererRecovery(app, win, opts)
	recovery.Store(controller)
	if controller != nil {
		defer controller.shutdown()
	}
	log.Info("opening the desktop window", "url", opts.URL)
	started := make(chan struct{})
	var once sync.Once
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		once.Do(func() { close(started) })
	})
	// Quit is synchronous; enqueue it so a concurrently exiting main loop
	// cannot strand the watcher waiting for a native callback.
	return runWithFatal(app.Run, nativeFatalQuit(log, func() { application.InvokeAsync(app.Quit) }), started, opts.Fatal)
}

// appOptions is everything Run feeds Wails except the window itself, so
// tests can see the Dock icon is wired without opening a native window.
func appOptions(opts Options) application.Options {
	if opts.Title == "" {
		opts.Title = "zwai"
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return application.Options{
		Name:        opts.Title,
		Description: "A swarm of agents that work together on your tasks",
		Icon:        dockIcon(),
		Logger:      log,
		LogLevel:    slog.LevelWarn,
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		OnShutdown: opts.OnShutdown,
		RawMessageHandler: func(window application.Window, message string, origin *application.OriginInfo) {
			directoryMessageHandler(opts.URL, chooseNativeDirectory)(window, message, origin)
			if opts.Recovery != nil {
				recoveryCoverMessageHandler(opts.URL, window, message, origin, updateRecoveryCover)
			}
			if opts.rawMessageHandler != nil {
				opts.rawMessageHandler(window, message, origin)
			}
		},
		WarningHandler: func(msg string) {
			if staleWindowWarning(msg) {
				return
			}
			log.Warn(msg)
		},
	}
}

// windowOptions is the native window Run opens. Split out so the Linux
// taskbar icon and the hidden title bar are testable without a display.
func windowOptions(opts Options) application.WebviewWindowOptions {
	if opts.Title == "" {
		opts.Title = "zwai"
	}
	return application.WebviewWindowOptions{
		Name:   "main",
		Title:  opts.Title,
		URL:    opts.URL,
		Width:  1280,
		Height: 860,
		// Below this the three-pane layout stops being usable, so the window
		// refuses to get smaller rather than rendering something broken.
		MinWidth:  980,
		MinHeight: 640,
		// The front end paints its own background; leaving this transparent
		// would flash white on a dark theme while the page loads.
		BackgroundType:   application.BackgroundTypeSolid,
		BackgroundColour: application.NewRGB(10, 10, 10),
		Mac: application.MacWindow{
			// Hidden title bar, no NSToolbar: we place the traffic lights
			// ourselves so they share the HTML header's baseline. HiddenInset
			// would add an empty toolbar that fights that geometry.
			// InvisibleTitleBarHeight is the native drag strip; a double-click
			// is left to the front end (wails:drag:doubleclick) so AppKit can
			// restore the previous frame.
			TitleBar:                application.MacTitleBarHidden,
			InvisibleTitleBarHeight: TitlebarHeight,
			Appearance:              application.DefaultAppearance,
		},
		Linux: application.LinuxWindow{
			Icon: dockIcon(),
		},
	}
}

// staleWindowWarning is the Wails message fired when NSWindow delegate
// events land after the Go window map has already been cleared on quit.
// Harmless, and not a signal anything in this process went wrong.
func staleWindowWarning(msg string) bool {
	if !strings.HasSuffix(msg, " not found") {
		return false
	}
	return strings.HasPrefix(msg, "Window #") || strings.HasPrefix(msg, "WebviewWindow #")
}
