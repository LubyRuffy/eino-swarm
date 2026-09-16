package desktop

import (
	"bytes"
	"image/png"
	"log/slog"
	"testing"
)

func TestStaleWindowWarningsAreTheQuitRace(t *testing.T) {
	// Wails drains NSWindow events after deleting the window from its map.
	// Those lines look like a bug; they are the cost of a fast quit.
	for _, msg := range []string{
		"Window #1 not found",
		"WebviewWindow #1 not found",
	} {
		if !staleWindowWarning(msg) {
			t.Fatalf("%q should be dropped, not shown as a warning", msg)
		}
	}
}

func TestRealWindowWarningsAreStillShown(t *testing.T) {
	for _, msg := range []string{
		"failed to start drag",
		"Window failed to load",
		"file not found",
		"Window #1 crashed",
	} {
		if staleWindowWarning(msg) {
			t.Fatalf("dropped a warning that is not the quit race: %q", msg)
		}
	}
}

func TestDockIconIsARealPNG(t *testing.T) {
	// An empty embed would compile and still leave macOS on the generic
	// Unix-exec glyph. The bytes have to be an image NSImage can decode.
	img, err := png.Decode(bytes.NewReader(appIcon))
	if err != nil {
		t.Fatalf("appicon.png is not a png: %v", err)
	}
	b := img.Bounds()
	if b.Dx() < 128 || b.Dy() < 128 {
		t.Fatalf("dock icon is %dx%d, too small to replace the exec glyph", b.Dx(), b.Dy())
	}
}

func TestAppOptionsGiveWailsTheDockIcon(t *testing.T) {
	// desktop.Run opens a window, so this is the seam that proves the
	// icon actually reaches Wails instead of sitting in the embed unused.
	got := appOptions(Options{})
	if got.Name != "zwai" {
		t.Fatalf("blank title should become zwai, got %q", got.Name)
	}
	if !bytes.Equal(got.Icon, dockIcon()) {
		t.Fatal("Wails never receives the dock icon")
	}
	if got.OnShutdown != nil {
		t.Fatal("nil OnShutdown should stay nil")
	}
}

func TestWindowOptionsCarryTheIconAndHiddenTitleBar(t *testing.T) {
	got := windowOptions(Options{URL: "http://127.0.0.1:9"})
	if got.Title != "zwai" {
		t.Fatalf("blank title should become zwai, got %q", got.Title)
	}
	if got.URL != "http://127.0.0.1:9" {
		t.Fatalf("window url %q", got.URL)
	}
	if !bytes.Equal(got.Linux.Icon, dockIcon()) {
		t.Fatal("Linux taskbar never receives the app icon")
	}
	if got.Mac.InvisibleTitleBarHeight != TitlebarHeight {
		t.Fatalf("title-bar drag strip is %d, want %d", got.Mac.InvisibleTitleBarHeight, TitlebarHeight)
	}
}

func TestAppOptionsKeepShutdownAndDropTheQuitRace(t *testing.T) {
	called := false
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	got := appOptions(Options{
		Title:      "custom",
		Logger:     log,
		OnShutdown: func() { called = true },
	})
	if got.Name != "custom" {
		t.Fatalf("window title %q, want the one the caller set", got.Name)
	}
	if got.OnShutdown == nil {
		t.Fatal("OnShutdown must reach Wails so closing the window closes the db")
	}
	got.OnShutdown()
	if !called {
		t.Fatal("OnShutdown was wrapped away")
	}
	if got.WarningHandler == nil {
		t.Fatal("WarningHandler is how the quit-race lines stay out of the log")
	}
	got.WarningHandler("Window #1 not found")
	if buf.Len() != 0 {
		t.Fatalf("quit-race warning leaked: %s", buf.String())
	}
	got.WarningHandler("failed to start drag")
	if !bytes.Contains(buf.Bytes(), []byte("failed to start drag")) {
		t.Fatalf("a real window warning was dropped: %s", buf.String())
	}
}
