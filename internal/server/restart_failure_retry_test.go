package server_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/app"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/server"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

// Failed checkpointing keeps the frozen engine diagnosable and retryable; it
// cannot authorize another execution of a possibly effective external tool.
func TestRestartStorageFailureKeepsDiagnosticsAndCanRetry(t *testing.T) {
	for _, name := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL"} {
		t.Setenv(name, "")
	}
	t.Setenv("ZWAI_MOCK_WORKER_DELAY_MS", "1000")
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Swarm.AutoTitle = false
	cfg.Memory.Enabled, cfg.Memory.AutoReview = false, false
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	var reject atomic.Bool
	reject.Store(true)
	var rejected atomic.Int32
	workerStored := make(chan struct{})
	var spawned sync.Once
	failure := errors.New("injected checkpoint storage failure")
	// Register once before constructing any runtime or HTTP goroutine. The
	// private store owns this callback until Close; only an atomic flag changes.
	if err := st.DB().Callback().Create().Before("gorm:create").Register("reject_restart_checkpoint", func(tx *gorm.DB) {
		if ev, ok := tx.Statement.Dest.(*store.Event); ok && ev.Kind == engine.KindRestartCheckpoint && reject.Load() {
			rejected.Add(1)
			_ = tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.DB().Callback().Create().After("gorm:create").Register("observe_restart_worker", func(tx *gorm.DB) {
		if ev, ok := tx.Statement.Dest.(*store.Event); ok && ev.Kind == "spawned" && tx.Error == nil {
			spawned.Do(func() { close(workerStored) })
		}
	}); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	eng := engine.New(cfg, st, provider.NewMock(cfg), logger)
	t.Cleanup(eng.Shutdown)
	srv, err := server.New(server.Options{Engine: eng, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(httpServer.Close)
	h := &harness{t: t, app: &app.App{Engine: eng, Store: st}, srv: httpServer}
	thread := h.newThread()
	turn, err := eng.StartTurn(thread, "continue the assigned work")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-workerStored:
	case <-time.After(5 * time.Second):
		t.Fatal("offline turn never persisted a real worker")
	}
	refused := h.json(http.MethodPost, "/api/engine/restart", nil, http.StatusBadRequest)
	if message, ok := refused["error"].(string); !ok || !strings.Contains(message, failure.Error()) {
		t.Fatalf("storage refusal lost its actual cause: %+v", refused)
	}
	if rejected.Load() < 2 {
		t.Fatal("the initial and final checkpoint writes did not reach the injected failure")
	}
	failed := h.json(http.MethodGet, "/api/engine/restart", nil, http.StatusOK)
	if failed["stage"] != "failed" || failed["ready"] == true {
		t.Fatalf("failed handoff hidden from diagnostics: %+v", failed)
	}
	h.json(http.MethodGet, "/api/threads/"+thread, nil, http.StatusOK)
	trace := h.json(http.MethodGet, "/api/trace/"+turn.ID, nil, http.StatusOK)
	if trace["turn"].(map[string]any)["id"] != turn.ID {
		t.Fatalf("failed handoff lost the original trace: %+v", trace)
	}
	denied := h.json(http.MethodPost, "/api/threads/"+thread+"/turns", map[string]any{"text": "another operation"}, http.StatusConflict)
	if denied["code"] != "restarting" {
		t.Fatalf("failed handoff thawed new work: %+v", denied)
	}
	reject.Store(false)
	ready := h.json(http.MethodPost, "/api/engine/restart", nil, http.StatusOK)
	if ready["ready"] != true || ready["stage"] != "ready" {
		t.Fatalf("repaired storage could not retry retained checkpoints: %+v", ready)
	}
	events, err := st.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	checkpoints := 0
	for _, ev := range events {
		if ev.Kind == engine.KindRestartCheckpoint {
			checkpoints++
			if ev.ThreadID != thread || ev.TurnID != turn.ID {
				t.Fatalf("retry changed checkpoint identity: %+v", ev)
			}
		}
	}
	turns, err := st.ListTurns(thread)
	if err != nil || len(turns) != 1 || turns[0].ID != turn.ID || checkpoints != 1 {
		t.Fatalf("retry executed another turn or lost its checkpoint: turns=%+v checkpoints=%d err=%v", turns, checkpoints, err)
	}
}
