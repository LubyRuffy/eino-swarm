package app

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/server"
)

func swapLaunch(t *testing.T) *[]string {
	t.Helper()
	var got []string
	prev := launch
	launch = func(name string, args ...string) error {
		got = append([]string{name}, args...)
		return nil
	}
	t.Cleanup(func() { launch = prev })
	return &got
}

func TestOpenBrowserNeedsAListeningServer(t *testing.T) {
	a := &App{}
	if err := a.OpenBrowser(); err == nil {
		t.Fatal("opening a browser at nothing should fail")
	}
}

func TestOpenBrowserLaunchesTheURL(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	a, err := New(Options{DataDir: t.TempDir(), Addr: "127.0.0.1:0", Mock: true, NoAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { a.Engine.Shutdown(); _ = a.Store.Close() }()
	url, err := a.Listen()
	if err != nil {
		t.Fatal(err)
	}
	got := swapLaunch(t)
	if err := a.OpenBrowser(); err != nil {
		t.Fatal(err)
	}
	if len(*got) == 0 || !strings.Contains(strings.Join(*got, " "), url) {
		t.Fatalf("the browser was not pointed at the server: %v", *got)
	}
}

// The reveal hook is what the desktop app gives the server; in a browser it
// must be absent so the API can say the feature is unavailable.
func TestRevealIsWiredOnlyForDesktop(t *testing.T) {
	if revealFor(server.ModeWeb) != nil {
		t.Fatal("a web server must not spawn a file manager on the host")
	}
	if revealFor(server.ModeDesktop) == nil {
		t.Fatal("the desktop shell needs the reveal hook")
	}

	dir := t.TempDir()
	file := filepath.Join(dir, "result.md")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := swapLaunch(t)
	if err := revealPath(file); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(*got, " ")
	// Whatever the platform command is, it has to name something that leads
	// to the file: either the file itself or the directory holding it.
	if !strings.Contains(joined, file) && !strings.Contains(joined, dir) {
		t.Fatalf("reveal did not point at the file: %q", joined)
	}

	launch = func(name string, args ...string) error { return errors.New("no file manager") }
	if err := revealPath(file); err == nil {
		t.Fatal("a failing file manager should be reported, not swallowed")
	}
}

func TestParentDirOfAFileAndADirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := parentDir(file); got != dir {
		t.Fatalf("parentDir(file)=%q want %q", got, dir)
	}
	if got := parentDir(dir); got != dir {
		t.Fatalf("parentDir(dir)=%q want %q", got, dir)
	}
	// A path that does not exist still resolves to something sensible rather
	// than erroring: revealing a just-deleted file should open its folder.
	if got := parentDir(filepath.Join(dir, "gone.txt")); got != dir {
		t.Fatalf("parentDir(missing)=%q want %q", got, dir)
	}
}

func TestNewRejectsAnUnusableDataDir(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{DataDir: blocker, Mock: true, NoAssets: true}); err == nil {
		t.Fatal("a data directory that is a file should fail loudly at startup")
	}
}

func TestListenReportsABadAddress(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	a, err := New(Options{DataDir: t.TempDir(), Addr: "127.0.0.1:not-a-port",
		Mock: true, NoAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { a.Engine.Shutdown(); _ = a.Store.Close() }()
	if _, err := a.Listen(); err == nil {
		t.Fatal("want an error for an unusable address")
	}
	if err := a.Serve(); err == nil {
		t.Fatal("Serve should surface the bind error rather than block")
	}
}

func TestOpenURLPicksAPlatformCommand(t *testing.T) {
	got := swapLaunch(t)
	if err := openURL("http://127.0.0.1:1234"); err != nil {
		t.Fatal(err)
	}
	if len(*got) == 0 {
		t.Fatal("no command was launched")
	}
	want := map[string]string{"darwin": "open", "windows": "rundll32"}[runtime.GOOS]
	if want == "" {
		want = "xdg-open"
	}
	if (*got)[0] != want {
		t.Fatalf("on %s the browser opens with %q, want %q", runtime.GOOS, (*got)[0], want)
	}
}
