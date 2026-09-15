package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/server"
	"github.com/LubyRuffy/eino-swarm/internal/store"
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

// Serve and Shutdown are the process lifecycle: a request has to be answered
// while it runs, and the database has to be closed when it stops.
func TestServeAnswersRequestsThenShutsDown(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	a, err := New(Options{DataDir: t.TempDir(), Addr: "127.0.0.1:0",
		Mock: true, NoAssets: true, Mode: server.ModeDesktop, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	url, err := a.Listen()
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- a.Serve() }()

	res, err := http.Get(url + "/api/meta")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), server.ModeDesktop) {
		t.Fatalf("meta says %d %s", res.StatusCode, body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.Shutdown(ctx)

	select {
	case err := <-served:
		// a clean shutdown is not an error the caller has to handle
		if err != nil {
			t.Fatalf("Serve returned %v after a clean shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after Shutdown")
	}
	// shutting down twice must not panic on an already closed database
	a.Shutdown(ctx)
}

// Serve with no prior Listen binds on demand, which is the shape `zwai web`
// uses when it does not need the URL up front.
func TestServeBindsOnDemand(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	a, err := New(Options{DataDir: t.TempDir(), Addr: "127.0.0.1:0", Mock: true, NoAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	if a.URL() != "" {
		t.Fatal("an unbound app must not claim a URL")
	}
	done := make(chan error, 1)
	go func() { done <- a.Serve() }()
	deadline := time.Now().Add(5 * time.Second)
	for a.URL() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if a.URL() == "" {
		t.Fatal("Serve never bound a port")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.Shutdown(ctx)
	if err := <-done; err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

// A crash leaves turns marked running. The next start has to close them, or
// the sidebar shows a conversation working forever.
func TestNewClosesTurnsLeftRunningByAPreviousProcess(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	dir := t.TempDir()

	first, err := New(Options{DataDir: dir, Addr: "127.0.0.1:0", Mock: true, NoAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	th, err := first.Engine.CreateThread("left over", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Store.CreateTurn(&store.Turn{ThreadID: th.ID, UserText: "in flight"}); err != nil {
		t.Fatal(err)
	}
	_ = first.Store.Close()

	second, err := New(Options{DataDir: dir, Addr: "127.0.0.1:0", Mock: true, NoAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { second.Engine.Shutdown(); _ = second.Store.Close() }()
	turns, err := second.Store.ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || turns[0].Status != store.TurnCancelled {
		t.Fatalf("stale turn not closed: %+v", turns)
	}
}

// The default build serves the embedded bundle, and a URL bound to every
// interface is reported as a loopback one a browser can actually open.
func TestNewServesTheEmbeddedBundleAndReportsALoopbackURL(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	a, err := New(Options{DataDir: t.TempDir(), Addr: "0.0.0.0:0", Mock: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { a.Engine.Shutdown(); _ = a.Store.Close() }()
	url, err := a.Listen()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Fatalf("url=%q: a wildcard bind must be reported as loopback", url)
	}
}

// A database that cannot be opened has to stop startup, not surface later as a
// broken conversation list.
func TestNewReportsAnUnusableDatabase(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "zwai.db"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{DataDir: dir, Mock: true, NoAssets: true}); err == nil {
		t.Fatal("a directory where the database belongs should fail at startup")
	}
}
