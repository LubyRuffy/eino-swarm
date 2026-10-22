package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/lease"
	"github.com/LubyRuffy/eino-swarm/internal/server"
)

// Reopening an idle same-build engine must keep it alive before the WebView
// loads any JavaScript. A successful meta probe alone is not an attachment.
func TestDesktopReopenHoldsTheEngineBeforeTheFrontendLoads(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan string, 1)
	done := make(chan error, 1)
	var advance atomic.Bool
	var afterAttachSamples atomic.Int32
	at := time.Now().Add(time.Hour)
	go func() {
		done <- serveEngine(ctx, engineOpts{
			DataDir: dir, Addr: "127.0.0.1:0", Mock: true,
			Ready: func(base string) { ready <- base },
			Watch: lease.Watcher{
				StartGrace: time.Millisecond, IdleGrace: time.Millisecond, Poll: time.Millisecond,
				Now: func() time.Time {
					if advance.Load() {
						n := afterAttachSamples.Add(1)
						return at.Add(time.Hour + time.Duration(n)*time.Millisecond)
					}
					return at
				},
			},
		})
	}()
	var base string
	select {
	case base = <-ready:
	case err := <-done:
		t.Fatal(err)
	case <-time.After(30 * time.Second):
		t.Fatal("engine did not listen")
	}
	opts, err := startDesktopServer([]string{"--mock", "--data-dir", dir})
	if err != nil {
		t.Fatal(err)
	}
	defer opts.OnShutdown()
	advance.Store(true)
	deadline := time.Now().Add(time.Second)
	for afterAttachSamples.Load() < 3 && time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("same-build engine exited before frontend presence: %v", err)
		default:
			time.Sleep(time.Millisecond)
		}
	}
	resp, err := http.Get(base + "/api/meta")
	if err != nil {
		t.Fatalf("the returned desktop URL became unreachable before frontend boot: %v", err)
	}
	defer resp.Body.Close()
	var meta struct {
		Clients []server.PresenceClient `json:"clients"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(opts.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Clients) != 1 || meta.Clients[0].Surface != "desktop" || meta.Clients[0].ID != u.Query().Get("presence") {
		t.Fatalf("native launch must already hold exactly its own slot: clients=%+v URL=%s", meta.Clients, opts.URL)
	}
	opts.OnShutdown()
	opts.OnShutdown()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("closing the native launch leaked its engine presence")
	}
}

func TestDesktopCannotOpenAWindowWithoutAUsableDataDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, []byte("occupied"), 0600); err != nil {
		t.Fatal(err)
	}
	opts, err := startDesktopServer([]string{"--mock", "--data-dir", path})
	if err == nil || opts.URL != "" || opts.OnShutdown != nil {
		t.Fatalf("failed startup returned usable native options: options=%+v err=%v", opts, err)
	}
}
