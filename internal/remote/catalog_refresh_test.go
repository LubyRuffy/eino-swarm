package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

func catalogEngine(t *testing.T, rows ...config.Provider) *engine.Engine {
	t.Helper()
	base := testEngine(t)
	cfg := base.Config()
	cfg.Models.Providers = rows
	if len(rows) > 0 {
		cfg.Models.Default = rows[0].ID
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	e := engine.New(cfg, base.Store(), provider.New(cfg), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(e.Shutdown)
	return e
}

// The PC's configured catalog is not evidence of what its endpoint serves now.
// Refresh must remove stale defaults without changing the saved default or key.
func TestRefreshCatalogReadsCurrentUpstreamWithoutChangingConfig(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer test-only-key" {
			t.Errorf("wrong discovery request path or authentication")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"new-a"},{"id":"new-b"},{"id":"new-a"},{"id":" "}]}`)
	}))
	defer server.Close()
	e := catalogEngine(t, config.Provider{
		ID: "endpoint", Label: "Endpoint", BaseURL: server.URL + "/v1", APIKey: "test-only-key",
		Model: "removed-default", Catalog: []string{"stale"}, ModelContext: map[string]int{"manual": 42},
	})
	before, err := os.ReadFile(e.Config().Path())
	if err != nil {
		t.Fatal(err)
	}
	cached := Handle(e, config.RemoteConfig{}, Request{ID: "cached", Op: OpCatalog}, "relay", "session")
	if !cached.OK || len(cached.Models) != 2 || requests.Load() != 0 {
		t.Fatalf("ordinary catalog must stay offline: models=%v requests=%d", cached.Models, requests.Load())
	}
	resp := Handle(e, config.RemoteConfig{}, Request{ID: "fresh", Op: "refresh_catalog"}, "relay", "session")
	if !resp.OK {
		t.Fatalf("explicit refresh must discover upstream: code=%s error=%s", resp.Code, resp.Error)
	}
	want := []ModelView{
		{ProviderID: "endpoint", ProviderLabel: "Endpoint", Model: "new-a"},
		{ProviderID: "endpoint", ProviderLabel: "Endpoint", Model: "new-b"},
	}
	if !reflect.DeepEqual(resp.Models, want) || requests.Load() != 1 {
		t.Fatalf("fresh catalog=%v requests=%d", resp.Models, requests.Load())
	}
	if !reflect.DeepEqual(resp.ReasoningLevels, config.ReasoningEfforts()) {
		t.Fatalf("thinking levels=%v", resp.ReasoningLevels)
	}
	after, err := os.ReadFile(e.Config().Path())
	if err != nil || !bytes.Equal(before, after) || e.Config().Models.Providers[0].Model != "removed-default" {
		t.Fatal("read-only discovery changed configuration")
	}
	raw, _ := json.Marshal(resp)
	if strings.Contains(string(raw), "test-only-key") || strings.Contains(string(raw), server.URL) {
		t.Fatal("fresh catalog leaked the PC's endpoint or credential")
	}
}

func TestRefreshCatalogFailureReturnsNoPartialListOrEndpointError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/good/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"fresh"}]}`)
			return
		}
		http.Error(w, "test-only-key", http.StatusUnauthorized)
	}))
	defer server.Close()
	e := catalogEngine(t,
		config.Provider{ID: "good", BaseURL: server.URL + "/good", Model: "old"},
		config.Provider{ID: "bad", BaseURL: server.URL + "/bad", Model: "old", APIKey: "test-only-key"},
	)
	resp := Handle(e, config.RemoteConfig{}, Request{ID: "fresh", Op: "refresh_catalog"}, "relay", "s")
	if resp.OK || resp.Code != "refresh_failed" || len(resp.Models) != 0 {
		t.Fatalf("failed provider must not masquerade as a removed provider: %+v", resp)
	}
	if resp.Error != "could not refresh model list" {
		t.Fatalf("discovery needs a stable safe error: %q", resp.Error)
	}
}

func TestRefreshCatalogUsesOneConcurrentBudgetAndKeepsProviderOrder(t *testing.T) {
	entered := make(chan string, 2)
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- r.URL.Path
		select {
		case <-release:
			_, _ = io.WriteString(w, `{"data":[{"id":"current"}]}`)
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	e := catalogEngine(t,
		config.Provider{ID: "first", BaseURL: server.URL + "/first", Model: "current"},
		config.Provider{ID: "second", BaseURL: server.URL + "/second", Model: "old"},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan Response, 1)
	go func() { got <- handleCatalogRefresh(ctx, e, Request{ID: "fresh"}, "relay", "s") }()
	for range 2 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("providers must be discovered concurrently, not one timeout after another")
		}
	}
	once.Do(func() { close(release) })
	resp := <-got
	if !resp.OK || len(resp.Models) != 2 || resp.Models[0].ProviderID != "first" || resp.Models[1].ProviderID != "second" || !resp.Models[0].Default || resp.Models[1].Default {
		t.Fatalf("provider order/default changed: %+v", resp)
	}
}

func TestRefreshCatalogIgnoresDisabledAndUnconfiguredEndpoints(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		_, _ = io.WriteString(w, `{"data":[{"id":"fresh"}]}`)
	}))
	defer server.Close()
	off := false
	e := catalogEngine(t,
		config.Provider{ID: "hidden", BaseURL: server.URL, Model: "old", Enabled: &off},
		config.Provider{ID: "unconfigured", Model: "old"},
	)
	resp := Handle(e, config.RemoteConfig{}, Request{Op: OpRefreshCatalog}, "relay", "s")
	if !resp.OK || len(resp.Models) != 0 || count.Load() != 0 {
		t.Fatalf("hidden or unconfigured endpoint was queried: %+v requests=%d", resp, count.Load())
	}
}

func TestRefreshCatalogRejectsUpstreamFailureAndMalformedLists(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"empty", `{"data":[]}`},
		{"HTML", `<html>upstream unavailable</html>`},
		{"wrong type", `{"data":"not-a-model-list"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			e := catalogEngine(t, config.Provider{ID: "endpoint", BaseURL: server.URL, Model: "old"})
			resp := Handle(e, config.RemoteConfig{}, Request{Op: OpRefreshCatalog}, "relay", "s")
			if resp.OK || resp.Code != "refresh_failed" || len(resp.Models) != 0 {
				t.Fatalf("invalid upstream must not become an empty successful refresh: %+v", resp)
			}
		})
	}
}

func TestRefreshCatalogCancellationAndFrameBudgetFailClosed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resp := handleCatalogRefresh(ctx, testEngine(t), Request{ID: "cancelled"}, "relay", "s")
	if resp.OK || resp.Code != "refresh_failed" {
		t.Fatalf("cancelled refresh=%+v", resp)
	}
	resp = handleCatalogRefresh(context.Background(), nil, Request{}, "relay", "s")
	if resp.OK || resp.Code != "refresh_failed" {
		t.Fatalf("unavailable engine=%+v", resp)
	}
	e := testEngine(t)
	resp = Handle(e, config.RemoteConfig{}, Request{Op: OpRefreshCatalog}, "relay", "s")
	if !resp.OK || len(resp.Models) == 0 || resp.Models[0].Model != provider.MockModelName {
		t.Fatalf("offline discovery must stay offline: %+v", resp)
	}
	e.Config().Models.Providers[0].Catalog = []string{strings.Repeat("x", maxCatalogFrame)}
	resp = Handle(e, config.RemoteConfig{}, Request{Op: OpRefreshCatalog}, "relay", "s")
	if resp.OK || resp.Code != "too_large" || len(resp.Models) != 0 {
		t.Fatalf("oversized catalog=%+v", resp)
	}
}

func TestRefreshCatalogDeadlineCancelsAnUnresponsiveUpstream(t *testing.T) {
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
	}))
	defer server.Close()
	e := catalogEngine(t, config.Provider{ID: "endpoint", BaseURL: server.URL, Model: "old"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got := make(chan Response, 1)
	go func() { got <- handleCatalogRefresh(ctx, e, Request{ID: "deadline"}, "relay", "s") }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("offline discovery did not start before its deadline")
	}
	select {
	case resp := <-got:
		if resp.OK || resp.Code != "refresh_failed" || len(resp.Models) != 0 || ctx.Err() != context.DeadlineExceeded {
			t.Fatalf("deadline did not fail the whole refresh: %+v err=%v", resp, ctx.Err())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("upstream outlived the shared deadline")
	}
}
