package server_test

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/app"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/server"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// The SSE acceptance needs a visible stream long enough to cross coalescing.
// Ordinary HTTP cases retain the faster mock and their existing App wiring.
func newStreamingHarness(t *testing.T) *harness {
	t.Helper()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool := provider.NewMockWithStreamDelay(cfg, 18*time.Millisecond)
	eng := engine.New(cfg, st, pool, logger)
	t.Cleanup(eng.Shutdown)
	srv, err := server.New(server.Options{Engine: eng, Logger: logger, Version: "test", Mode: server.ModeWeb})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	a := &app.App{Config: cfg, Store: st, Pool: pool, Engine: eng, Server: srv, Logger: logger}
	return &harness{t: t, app: a, srv: ts}
}
