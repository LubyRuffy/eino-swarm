package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/lease"
)

// Nothing is attached and nothing is running, so the process has to exit
// without anyone calling Shutdown from a window.
func TestEngineExitsWhenIdle(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")

	ready := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- serveEngine(context.Background(), engineOpts{
			DataDir: t.TempDir(),
			Addr:    "127.0.0.1:0",
			Mock:    true,
			Watch: lease.Watcher{
				StartGrace: 20 * time.Millisecond,
				IdleGrace:  30 * time.Millisecond,
				Poll:       5 * time.Millisecond,
			},
			Ready: func(url string) { ready <- url },
		})
	}()
	var url string
	select {
	case url = <-ready:
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(30 * time.Second):
		t.Fatal("engine did not listen")
	}
	if _, err := http.Get(url + "/api/meta"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("idle engine stayed up")
	}
}
