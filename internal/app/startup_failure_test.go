package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A valid schema can contain unreadable legacy data. New must return the
// actual recovery error and close its assembled resources instead of leaking
// an unreachable app with search/remote owners and an open WAL connection.
func TestNewRejectsFatalRecoveryAndClosesItsDatabase(t *testing.T) {
	for _, name := range []string{"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL"} {
		t.Setenv(name, "")
	}
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	thread := &store.Thread{Title: "legacy record"}
	if err := st.CreateThread(thread); err != nil {
		t.Fatal(err)
	}
	if err := st.DB().Exec("UPDATE threads SET completed_seq = ? WHERE id = ?", "invalid-integer", thread.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	// Establish that migration succeeds and that recovery's ListThreads is the
	// failing boundary, rather than accepting a prior config/open failure.
	checked, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal("fixture failed before recovery: ", err)
	}
	if _, err := checked.ListThreads(true, ""); err == nil || !strings.Contains(err.Error(), "completed_seq") {
		t.Fatalf("fixture did not fail at thread decoding: %v", err)
	}
	if err := checked.Close(); err != nil {
		t.Fatal(err)
	}
	a, err := New(Options{DataDir: dir, Mock: true, NoAssets: true})
	var partial *engine.RestoreFailures
	if a != nil || err == nil || errors.As(err, &partial) || !strings.Contains(err.Error(), "store: list threads:") || !strings.Contains(err.Error(), "completed_seq") {
		t.Fatalf("fatal startup recovery lost its scan-error context: app=%v error=%v", a, err)
	}
	// A second live WAL connection prevents changing the journal mode. This
	// checks actual SQL ownership after failure, not only a private close flag.
	probe, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := probe.Close(); err != nil {
			t.Error(err)
		}
	}()
	var mode string
	if err := probe.DB().Raw("PRAGMA journal_mode=DELETE").Scan(&mode).Error; err != nil || mode != "delete" {
		t.Fatalf("failed app retained a WAL connection: mode=%q err=%v", mode, err)
	}
}
