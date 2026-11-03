package engine

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

// A move can legitimately finish before fresh occupation, even though the
// admission already prepared a turn from the original project context.
func TestNuwaMoveBeforeFreshClaimRefusesStaleProjectSnapshot(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Swarm.ComposerPredictions = false
	e.cfg.Memory.AutoReview = false
	var requests atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "stale project turn reached a model request", http.StatusBadRequest)
	}))
	t.Cleanup(endpoint.Close)
	e.cfg.Models = config.ModelsConfig{Default: "project-move-fixture", Providers: []config.Provider{
		{ID: "project-move-fixture", BaseURL: endpoint.URL, Model: "fixture/planner"},
	}}
	e.pool = provider.New(e.cfg)
	old, err := e.CreateProject("Original owner", "", t.TempDir(), false, true)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := e.CreateProject("Current owner", "", t.TempDir(), false, true)
	if err != nil {
		t.Fatal(err)
	}
	th, err := e.CreateThread("Fresh admission", "", old.ID)
	if err != nil {
		t.Fatal(err)
	}
	prepared, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	if err := e.store.DB().Callback().Create().Before("gorm:create").Register("fresh-move-before-claim", func(tx *gorm.DB) {
		if turn, ok := tx.Statement.Dest.(*store.Turn); ok && turn.ThreadID == th.ID && turn.ProjectID == old.ID {
			close(prepared)
			<-release
		}
	}); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		turn *store.Turn
		err  error
	}
	started := make(chan outcome, 1)
	go func() {
		turn, err := e.StartTurn(th.ID, "Read the current workspace.")
		started <- outcome{turn, err}
	}()
	select {
	case <-prepared:
	case <-time.After(5 * time.Second):
		unblock()
		t.Fatal("real turn did not capture old project before occupation")
	}
	if err := e.MoveThread(th.ID, destination.ID); err != nil {
		unblock()
		t.Fatal(err)
	}
	unblock()
	var observed outcome
	select {
	case observed = <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("fresh admission did not return after Move")
	}
	current, err := e.store.GetThread(th.ID)
	if err != nil || current.ProjectID != destination.ID {
		t.Fatalf("Move did not persist: %+v %v", current, err)
	}
	if observed.turn != nil {
		waitRestartState(t, func() bool { return requests.Load() > 0 })
		_ = e.Interrupt(th.ID)
		waitSettled(t, e, th.ID)
	}
	if observed.turn != nil || !errors.Is(observed.err, errTurnProjectOwnerChanged) || requests.Load() != 0 {
		t.Fatalf("stale fresh claim: admitted=%v err=%v actual_model_requests=%d",
			observed.turn != nil, observed.err, requests.Load())
	}
}
