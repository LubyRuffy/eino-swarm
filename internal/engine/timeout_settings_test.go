package engine

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/cloudwego/eino/schema"
)

func TestTimeoutFirstBytePublicErrorNamesTheActualHTTPBudget(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	e := newTestEngine(t)
	if err := e.cfg.Update(func(c *config.Config) error {
		c.Models.Providers = []config.Provider{{ID: "local", BaseURL: server.URL, Model: "offline-fixture", TimeoutSeconds: 4, FirstByteTimeoutSeconds: 1}}
		c.Models.Default = "local"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	model, err := provider.New(e.cfg).Get(context.Background(), "local", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.Generate(context.Background(), []*schema.Message{schema.UserMessage("check configured timeout")})
	if err == nil {
		t.Fatal("silent real HTTP endpoint must time out")
	}
	if got := publicTurnError(err); !strings.Contains(got, "within 1s") {
		t.Fatalf("wrong actual timeout advice: %s", got)
	}
}

func TestTimeoutUpdateAppliesOnlyAfterPersistenceAndKeepsWatchdogBudget(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	r := swarm.NewRegistry()
	defer r.Close()
	if err := r.PlantFinished(swarm.FinishedWorker{ID: "exhausted", Role: "worker", TimeoutFailures: 3}); err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(th.ID)
	rt.mu.Lock()
	rt.reg = r
	rt.mu.Unlock()
	if err := e.UpdateConfig(func(c *config.Config) error { c.Swarm.AgentTimeoutSeconds = 43; return nil }); err != nil {
		t.Fatal(err)
	}
	if r.AgentTimeout != 43*time.Second {
		t.Fatal("registry not updated")
	}
	if _, err := r.Resume(t.Context(), "exhausted", "continue", nil); err == nil {
		t.Fatal("save reset watchdog cap")
	}
	if err := os.Mkdir(e.cfg.Path()+".tmp", 0700); err != nil {
		t.Fatal(err)
	}
	if err := e.UpdateConfig(func(c *config.Config) error { c.Swarm.AgentTimeoutSeconds = 55; return nil }); err == nil {
		t.Fatal("expected save failure")
	}
	if e.cfg.SwarmSnapshot().AgentTimeoutSeconds != 43 || r.AgentTimeout != 43*time.Second {
		t.Fatal("failed save changed runtime")
	}
	if err := e.UpdateConfig(func(*config.Config) error { return config.ErrTimeoutSettings }); !errors.Is(err, config.ErrTimeoutSettings) {
		t.Fatal(err)
	}
}

func TestTimeoutActualFirstByteBudgetReachesStoredTurnAndTrace(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	e := newTestEngine(t)
	if err := e.cfg.Update(func(c *config.Config) error {
		c.Memory.Enabled = false
		c.Swarm.AutoTitle = false
		c.Models.Default = "local"
		c.Models.Providers = []config.Provider{{ID: "local", BaseURL: server.URL, Model: "offline", TimeoutSeconds: 4, FirstByteTimeoutSeconds: 1}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	e.pool = provider.New(e.cfg)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "report from offline fixture")
	if err != nil {
		t.Fatal(err)
	}
	done := waitForTurn(t, e, turn.ID)
	if !strings.Contains(done.Error, "within 1s") {
		t.Fatalf("wrong stored error: %s", done.Error)
	}
	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == swarm.NotifyError.String() && strings.Contains(ev.Err, "within 1s") {
			return
		}
	}
	t.Fatal("actual first-byte budget missing from persisted Trace")
}

func TestTimeoutContextUsageReadsTheCurrentBudgetDuringSaves(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	goal := "context 内容"
	if err := e.store.UpdateThread(th.ID, map[string]any{"goal": goal}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []int{71, 113} {
		if err := e.UpdateConfig(func(c *config.Config) error { c.Swarm.ContextCharBudget = want; return nil }); err != nil {
			t.Fatal(err)
		}
		chars, budget, err := e.ContextUsage(th.ID)
		if err != nil || chars != len([]rune(goal)) || budget != want {
			t.Fatalf("context usage=(%d,%d,%v), want (%d,%d,nil)", chars, budget, err, len([]rune(goal)), want)
		}
	}
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 12; i++ {
			err := e.UpdateConfig(func(c *config.Config) error {
				n := 43 + i%2
				c.Swarm.ContextCharBudget = []int{71, 113}[i%2]
				return config.ApplyTimeoutPatch(c, config.TimeoutPatch{AgentTimeoutSeconds: &n})
			})
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	defer func() {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	for i := 0; i < 30; i++ {
		chars, budget, err := e.ContextUsage(th.ID)
		if err != nil || chars != len([]rune(goal)) || (budget != 71 && budget != 113) {
			t.Fatalf("concurrent context usage=(%d,%d,%v)", chars, budget, err)
		}
	}
}

func TestTimeoutTidyAnchorKeepsConfiguredFallbackWhenStorageFails(t *testing.T) {
	e := newTestEngine(t)
	timeoutAnchorFallbackDuringSaves(t, e, func() (string, string, string, string, string) {
		return e.tidyReviewAnchor("unavailable-project")
	})
}

func TestTimeoutLibraryAnchorKeepsConfiguredFallbackWhenStorageFails(t *testing.T) {
	e := newTestEngine(t)
	timeoutAnchorFallbackDuringSaves(t, e, e.libraryReviewAnchor)
}

func timeoutAnchorFallbackDuringSaves(t *testing.T, e *Engine, read func() (string, string, string, string, string)) {
	t.Helper()
	if err := e.UpdateConfig(func(c *config.Config) error {
		c.Models.Providers = []config.Provider{{ID: "fallback-a"}, {ID: "fallback-b"}}
		c.Models.Default = "fallback-a"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.store.Close(); err != nil {
		t.Fatal(err)
	}
	check := func(want string) {
		t.Helper()
		thread, turn, providerID, model, effort := read()
		if thread != "" || turn != "" || model != "" || effort != "" || (want != "" && providerID != want) || (providerID != "fallback-a" && providerID != "fallback-b") {
			t.Fatalf("storage-failure fallback=(%q,%q,%q,%q,%q), want provider=%q only", thread, turn, providerID, model, effort, want)
		}
	}
	check("fallback-a")
	if err := e.UpdateConfig(func(c *config.Config) error { c.Models.Default = "fallback-b"; return nil }); err != nil {
		t.Fatal(err)
	}
	check("fallback-b")
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 12; i++ {
			err := e.UpdateConfig(func(c *config.Config) error {
				n := 43 + i%2
				c.Models.Default = []string{"fallback-a", "fallback-b"}[i%2]
				return config.ApplyTimeoutPatch(c, config.TimeoutPatch{AgentTimeoutSeconds: &n})
			})
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	defer func() {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	for i := 0; i < 30; i++ {
		check("")
	}
}
