package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func TestParseModelCatalogKeepsOrderAndDropsJunk(t *testing.T) {
	raw := []byte(`{"object":"list","data":[
		{"id":"alpha","object":"model"},
		{"id":"","object":"model"},
		{"id":"beta"},
		{"id":"alpha"}
	]}`)
	got, err := parseModelCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Names) != 2 || got.Names[0] != "alpha" || got.Names[1] != "beta" {
		t.Fatalf("got %v", got.Names)
	}
	if len(got.Windows) != 0 {
		t.Fatalf("a listing with no window keys must not invent one: %v", got.Windows)
	}
	if _, err := parseModelCatalog([]byte("{")); err == nil {
		t.Fatal("garbage JSON must fail")
	}
}

func TestParseModelCatalogReadsContextWindowsWithoutInventingThem(t *testing.T) {
	raw := []byte(`{"data":[
		{"id":"alpha","context_length":128000},
		{"id":"beta","max_model_len":8192},
		{"id":"gamma","context_window":"nope"},
		{"id":"delta"}
	]}`)
	got, err := parseModelCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Windows["alpha"] != 128000 || got.Windows["beta"] != 8192 {
		t.Fatalf("windows=%v", got.Windows)
	}
	if _, ok := got.Windows["gamma"]; ok {
		t.Fatalf("a non-number window must be ignored: %v", got.Windows)
	}
	if _, ok := got.Windows["delta"]; ok {
		t.Fatalf("a missing window must stay missing: %v", got.Windows)
	}
}

func TestParseModelCatalogReadsNestedWindowsAndIgnoresOutputCaps(t *testing.T) {
	raw := []byte(`{"data":[
		{"id":"or","top_provider":{"context_length":200000,"max_completion_tokens":8192}},
		{"id":"llama","meta":{"n_ctx":32768}},
		{"id":"out","max_completion_tokens":4096,"max_tokens":2048}
	]}`)
	got, err := parseModelCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Windows["or"] != 200000 {
		t.Fatalf("nested OpenRouter-style window=%v", got.Windows["or"])
	}
	if got.Windows["llama"] != 32768 {
		t.Fatalf("nested n_ctx=%v", got.Windows["llama"])
	}
	if _, ok := got.Windows["out"]; ok {
		t.Fatalf("an output cap must not become the context window: %v", got.Windows)
	}
}

func TestListFlattensAProviderCatalog(t *testing.T) {
	cfg := configFor(t, true)
	cfg.Models.Providers[0].Label = "Endpoint"
	cfg.Models.Providers[0].Model = "alpha"
	cfg.Models.Providers[0].Catalog = []string{"beta", "alpha", "gamma"}
	list := New(cfg).List()
	if len(list) != 3 {
		t.Fatalf("want 3 selectable models, got %d: %+v", len(list), list)
	}
	if list[0].Model != "beta" || list[1].Model != "alpha" || list[2].Model != "gamma" {
		t.Fatalf("catalog order lost: %+v", list)
	}
	if list[0].ProviderID != cfg.Models.Default || list[0].ProviderLabel != "Endpoint" {
		t.Fatalf("provider grouping fields: %+v", list[0])
	}
	if !list[1].Default || list[0].Default || list[2].Default {
		t.Fatalf("only the configured default is marked: %+v", list)
	}
	if list[1].ID != ChoiceID(cfg.Models.Default, "alpha") {
		t.Fatalf("id=%q", list[1].ID)
	}
}

func TestListReportsAConfiguredWindowAndTheMockFallback(t *testing.T) {
	cfg := configFor(t, true)
	cfg.Models.Providers[0].ContextWindow = 8000
	cfg.Models.Providers[0].ModelContext = map[string]int{"some-model": 32000}
	list := New(cfg).List()
	if len(list) != 1 || list[0].ContextWindow != 32000 {
		t.Fatalf("real pool should prefer the per-name window: %+v", list)
	}

	mock := NewMock(configFor(t, false)).List()
	if len(mock) != 1 || mock[0].ContextWindow != MockContextWindow {
		t.Fatalf("scripted pool needs a simulated window: %+v", mock)
	}
	if New(configFor(t, true)).WindowFor("", "some-model") != 0 {
		t.Fatal("a real endpoint with no window configured must stay at 0")
	}
}

func TestListGroupsByProviderNameNotTheDefaultModel(t *testing.T) {
	cfg := configFor(t, true)
	cfg.Models.Providers[0].Label = ""
	cfg.Models.Providers[0].Model = "alpha"
	cfg.Models.Providers[0].Catalog = []string{"alpha", "beta"}
	list := New(cfg).List()
	if len(list) != 2 {
		t.Fatalf("got %d", len(list))
	}
	if list[0].ProviderLabel != cfg.Models.Providers[0].ID {
		t.Fatalf("composer groups by provider, not the default model: %+v", list[0])
	}
}

func TestDiscoverReadsAnOpenAICompatibleList(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path=%s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		sawAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "alpha"}, {"id": "beta"}},
		})
	}))
	t.Cleanup(srv.Close)

	cfg := configFor(t, true)
	cfg.Models.Providers[0].BaseURL = srv.URL + "/v1"
	cfg.Models.Providers[0].APIKey = "secret-token"
	p := New(cfg)
	cat, err := p.Discover(context.Background(), cfg.Models.Providers[0])
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(cat.Names) != 2 || cat.Names[0] != "alpha" || cat.Names[1] != "beta" {
		t.Fatalf("names=%v", cat.Names)
	}
	if sawAuth != "Bearer secret-token" {
		t.Fatalf("auth=%q", sawAuth)
	}
}

func TestDiscoverFailsCleanly(t *testing.T) {
	p := New(configFor(t, true))
	if _, err := p.Discover(context.Background(), config.Provider{}); err == nil {
		t.Fatal("a blank URL must not pretend to list models")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	if _, err := p.Discover(context.Background(), config.Provider{BaseURL: srv.URL}); err == nil {
		t.Fatal("want an error for a failing endpoint")
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[]}`)
	}))
	t.Cleanup(empty.Close)
	if _, err := p.Discover(context.Background(), config.Provider{BaseURL: empty.URL}); err == nil ||
		!strings.Contains(err.Error(), "no models") {
		t.Fatalf("empty list err=%v", err)
	}
}

func TestMockDiscoverNeverHitsTheNetwork(t *testing.T) {
	p := NewMock(configFor(t, false))
	cat, err := p.Discover(context.Background(), config.Provider{BaseURL: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Names) != 1 || cat.Names[0] != MockModelName {
		t.Fatalf("offline catalog=%v", cat.Names)
	}
	if cat.Windows[MockModelName] != MockContextWindow {
		t.Fatalf("offline window=%v", cat.Windows)
	}

	named := config.Provider{Model: "alpha", Catalog: []string{"alpha", "beta"}}
	got, err := p.Discover(context.Background(), named)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Names) != 2 {
		t.Fatalf("mock should return the configured catalog: %v", got.Names)
	}
	if got.Windows["alpha"] != MockContextWindow || got.Windows["beta"] != MockContextWindow {
		t.Fatalf("offline windows=%v", got.Windows)
	}
}

func TestResolveModelOverridesTheDefault(t *testing.T) {
	p := New(configFor(t, true))
	prov, err := p.ResolveModel("", "other")
	if err != nil {
		t.Fatal(err)
	}
	if prov.Model != "other" {
		t.Fatalf("model=%q", prov.Model)
	}
	def, err := p.ResolveModel("", "")
	if err != nil {
		t.Fatal(err)
	}
	if def.Model != "some-model" {
		t.Fatalf("empty override must keep the default: %q", def.Model)
	}
}

func TestDiscoverTimeoutUsesTheShorterBound(t *testing.T) {
	if got := discoverTimeout(config.Provider{}); got != config.DefaultDiscoverTimeout {
		t.Fatalf("blank provider timeout=%v", got)
	}
	short := discoverTimeout(config.Provider{TimeoutSeconds: 3})
	if short != 3*time.Second {
		t.Fatalf("short timeout=%v", short)
	}
	long := discoverTimeout(config.Provider{TimeoutSeconds: 600})
	if long != config.DefaultDiscoverTimeout {
		t.Fatalf("chat-length timeout must not apply to listing: %v", long)
	}
}

func TestMockDiscoverUsesInjectedFuncOnRealPool(t *testing.T) {
	p := New(configFor(t, true))
	p.discover = func(context.Context, config.Provider) (Catalog, error) {
		return Catalog{Names: []string{"injected"}}, nil
	}
	got, err := p.Discover(context.Background(), p.cfg.Models.Providers[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Names) != 1 || got.Names[0] != "injected" {
		t.Fatalf("got %v", got.Names)
	}
}
