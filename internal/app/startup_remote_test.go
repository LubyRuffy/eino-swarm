package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func TestNewDoesNotWaitForRemoteRegistration(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(hub.Close)
	t.Cleanup(unblock)
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Remote.Enabled = true
	cfg.Remote.HubURL = hub.URL
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	type result struct {
		app *App
		err error
	}
	ready := make(chan result, 1)
	go func() {
		a, err := New(Options{DataDir: cfg.DataDir(), Addr: "127.0.0.1:0", Mock: true, NoAssets: true})
		ready <- result{a, err}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("application never reached the controlled hub")
	}
	var got result
	select {
	case got = <-ready:
	case <-time.After(500 * time.Millisecond):
		t.Error("application startup waited for remote registration")
		unblock()
		got = <-ready
	}
	if got.err != nil {
		t.Fatal(got.err)
	}
	t.Cleanup(func() { got.app.Shutdown(t.Context()) })
	url, err := got.app.Listen()
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- got.app.Serve() }()
	resp, err := http.Get(url + "/api/meta")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("local metadata returned %d", resp.StatusCode)
	}
	got.app.Shutdown(t.Context())
	if err := <-served; err != nil {
		t.Fatal(err)
	}
}

func TestNewReportsLocalStartupPhaseDurations(t *testing.T) {
	logFile, err := os.CreateTemp(t.TempDir(), "startup-*.log")
	if err != nil {
		t.Fatal(err)
	}
	previousStderr := os.Stderr
	os.Stderr = logFile
	t.Cleanup(func() {
		os.Stderr = previousStderr
		_ = logFile.Close()
	})
	a, err := New(Options{DataDir: t.TempDir(), Mock: true, NoAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Shutdown(t.Context()) })
	raw, err := os.ReadFile(logFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"database_ready", "unfinished_runs_restored", "local_handlers_ready"} {
		count := 0
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.Contains(line, "phase="+phase+" ") {
				continue
			}
			count++
			_, value, found := strings.Cut(line, "duration_ms=")
			duration, err := strconv.ParseInt(value, 10, 64)
			if !found || err != nil || duration < 0 {
				t.Errorf("phase %s did not expose a nonnegative duration: %s", phase, line)
			}
		}
		if count != 1 {
			t.Errorf("phase %s appeared %d times", phase, count)
		}
	}
}
