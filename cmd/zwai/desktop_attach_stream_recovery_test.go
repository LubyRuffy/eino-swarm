package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/app"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
	"github.com/LubyRuffy/eino-swarm/internal/server"
)

// Real server admission and real HTTP stream: a native process is still alive
// when its previously acknowledged request ends. Only the first stream is
// interrupted; the same engine can immediately accept a replacement hold.
func TestDesktopHoldReconnectsAfterAcknowledgedStreamEOF(t *testing.T) {
	for _, key := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL"} {
		t.Setenv(key, "")
	}
	a, err := app.New(app.Options{DataDir: t.TempDir(), Mock: true, Mode: server.ModeEngine, Version: "stream-eof-readonly-review", NoAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	a.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	drop := make(chan struct{})
	var gets atomic.Int32
	handler := a.Server.Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/presence/") && gets.Add(1) == 1 {
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			go func() {
				select {
				case <-drop:
					cancel()
				case <-ctx.Done():
				}
			}()
			handler.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		srv.Close()
		if a.Search != nil {
			a.Search.Stop()
		}
		a.Engine.Shutdown()
		_ = a.Store.Close()
	})
	held, err := holdDesktopPresence(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer held.release()
	clients := a.Server.Clients()
	if len(clients) != 1 || clients[0].ID != held.id || clients[0].PID != os.Getpid() {
		t.Fatalf("native hold was not genuinely acknowledged: %+v", clients)
	}
	t.Logf("acknowledged native pid=%d id=%s actual_clients=%d", os.Getpid(), held.id, len(clients))
	close(drop)
	// Joining the real cancelled handler proves its original slot was released.
	deadline := time.Now().Add(time.Second)
	for len(a.Server.Clients()) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	// Do not require uninterrupted TCP. Require another acknowledged hold before
	// the actual engine idle grace makes this still-open window eligible to die.
	deadline = time.Now().Add(lease.IdleGrace + 200*time.Millisecond)
	for time.Now().Before(deadline) {
		if got := a.Server.Clients(); len(got) == 1 && got[0].PID == os.Getpid() {
			t.Logf("native recovered stream with %+v", got)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	stopped := a.Server.TryIdleShutdown()
	t.Fatalf("acknowledged stream EOF permanently removed presence while native PID %d remains alive and release was never called: clients=%+v GETs=%d idle_shutdown_committed=%v", os.Getpid(), a.Server.Clients(), gets.Load(), stopped)
}
