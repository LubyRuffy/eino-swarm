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

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Options configures the window.
type Options struct {
	// URL is the already-listening local server to load.
	URL string
	// Title is the window title.
	Title string
	// Version appears in the about box.
	Version string
	// Logger receives window lifecycle messages.
	Logger *slog.Logger
	// OnShutdown runs when the user closes the app, before the process ends,
	// so running turns are cancelled and the database is closed cleanly.
	OnShutdown func()
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

	app := application.New(application.Options{
		Name:        opts.Title,
		Description: "A swarm of agents that work together on your tasks",
		Logger:      log,
		LogLevel:    slog.LevelWarn,
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		OnShutdown: opts.OnShutdown,
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
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
			// Hidden inset title bar plus a draggable header in the front end
			// is what makes it look like a native app rather than a browser.
			TitleBar:                application.MacTitleBarHiddenInset,
			InvisibleTitleBarHeight: 48,
			Appearance:              application.DefaultAppearance,
		},
	})

	log.Info("opening the desktop window", "url", opts.URL)
	return app.Run()
}
