package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadCreatesDefaultsOnFirstRun(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := os.Stat(cfg.Path()); err != nil {
		t.Fatalf("first run must write the config file: %v", err)
	}
	if _, err := os.Stat(cfg.WorkspacesDir()); err != nil {
		t.Fatalf("first run must create the workspaces dir: %v", err)
	}
	if cfg.Server.Addr != DefaultAddr {
		t.Fatalf("addr=%q", cfg.Server.Addr)
	}
	if cfg.Configured() {
		t.Fatal("a blank provider must not count as configured")
	}
	if cfg.DataDir() != mustAbs(t, dir) {
		t.Fatalf("data dir=%q", cfg.DataDir())
	}
	if filepath.Base(cfg.DBPath()) != "zwai.db" {
		t.Fatalf("db path=%q", cfg.DBPath())
	}
	if cfg.WorkspaceDir("t1") != filepath.Join(cfg.WorkspacesDir(), "t1") {
		t.Fatalf("workspace dir=%q", cfg.WorkspaceDir("t1"))
	}
}

func TestLoadSeedsBlanksFromEnvOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENAI_BASE_URL", "http://endpoint.invalid/v1")
	t.Setenv("OPENAI_API_KEY", "seed-key")
	t.Setenv("OPENAI_MODEL", "seed-model")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p, ok := cfg.DefaultProvider()
	if !ok {
		t.Fatal("no default provider")
	}
	if p.BaseURL != "http://endpoint.invalid/v1" || p.APIKey != "seed-key" || p.Model != "seed-model" {
		t.Fatalf("env seeding failed: %+v", p)
	}
	if !cfg.Configured() {
		t.Fatal("a seeded provider should count as configured")
	}

	// a value the user already set must survive a different environment
	p.Model = "user-choice"
	cfg.Models.Providers[0] = p
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	t.Setenv("OPENAI_MODEL", "env-tries-again")
	reloaded, err := Load(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, _ := reloaded.DefaultProvider()
	if got.Model != "user-choice" {
		t.Fatalf("env overwrote a configured value: %q", got.Model)
	}
}

func TestDefaultDataDirHonorsEnv(t *testing.T) {
	t.Setenv(EnvDataDir, "/tmp/zwai-custom")
	if got := DefaultDataDir(); got != "/tmp/zwai-custom" {
		t.Fatalf("DefaultDataDir=%q", got)
	}
	t.Setenv(EnvDataDir, "")
	if got := DefaultDataDir(); !strings.HasSuffix(got, ".zwai-swarm") {
		t.Fatalf("DefaultDataDir=%q", got)
	}
}

func TestNormalizeRepairsHandEditedConfig(t *testing.T) {
	dir := t.TempDir()
	raw := strings.Join([]string{
		"server:",
		"  addr: \"\"",
		"models:",
		"  default: does-not-exist",
		"  providers:",
		"    - id: \"\"",
		"      base_url: http://a.invalid/v1",
		"      model: m-a",
		"    - id: \"\"",
		"      base_url: http://b.invalid/v1",
		"      model: m-b",
		"swarm:",
		"  max_concurrent: 0",
		"  max_turns: -3",
		"  progress_interval_seconds: 0",
		"tools:",
		"  web_search_max_results: 0",
		"log:",
		"  level: \"\"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Addr != DefaultAddr {
		t.Fatalf("blank addr not repaired: %q", cfg.Server.Addr)
	}
	if cfg.Swarm.MaxConcurrent != DefaultMaxConcurrent || cfg.Swarm.MaxTurns != DefaultMaxTurns {
		t.Fatalf("swarm bounds not repaired: %+v", cfg.Swarm)
	}
	// A hand-edited zero must not switch the pulse off silently: a turn that
	// reports nothing is indistinguishable from a stuck one.
	if cfg.Swarm.ProgressIntervalSeconds != DefaultProgressIntervalSeconds {
		t.Fatalf("progress interval not repaired: %+v", cfg.Swarm)
	}
	if cfg.Tools.WebSearchMaxResults != DefaultWebSearchResults {
		t.Fatalf("web search results not repaired: %d", cfg.Tools.WebSearchMaxResults)
	}
	if cfg.Log.Level != DefaultLogLevel {
		t.Fatalf("log level not repaired: %q", cfg.Log.Level)
	}
	ids := map[string]bool{}
	for _, p := range cfg.Models.Providers {
		if p.ID == "" {
			t.Fatal("anonymous provider kept an empty id")
		}
		if ids[p.ID] {
			t.Fatalf("duplicate provider id %q", p.ID)
		}
		ids[p.ID] = true
		if p.TimeoutSeconds <= 0 {
			t.Fatalf("provider %q has no timeout", p.ID)
		}
	}
	// a default pointing at a provider that is not there must fall back to a
	// real one, or every turn fails with "unknown provider"
	if _, ok := cfg.DefaultProvider(); !ok {
		t.Fatalf("dangling default not repaired: %q", cfg.Models.Default)
	}
	if cfg.Models.Default != cfg.Models.Providers[0].ID {
		t.Fatalf("default=%q", cfg.Models.Default)
	}
}

// The settings UI reads the tool exception lists as arrays, so they must never
// reach it as JSON null.
func TestToolExceptionListsSerializeAsLists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("tools:\n  proxy:\n    http: \"\"\n"), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	raw, err := json.Marshal(cfg.Tools)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(raw, []byte("null")) {
		t.Fatalf("tools carry a null list: %s", raw)
	}
	if d := Default(); d.Tools.Disabled == nil || d.Tools.Enabled == nil {
		t.Fatalf("defaults carry nil lists: %+v", d.Tools)
	}
}

func TestSaveAndReplaceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	next := Default()
	next.Server.Addr = "127.0.0.1:9999"
	next.Server.OpenBrowser = false
	next.Swarm.MaxConcurrent = 3
	next.Tools.Disabled = []string{"exec"}
	next.Tools.Proxy = ProxyConfig{HTTP: "http://127.0.0.1:7890", NoProxy: "localhost"}
	next.Models.Providers = []Provider{{
		ID: "local", Label: "Local", BaseURL: "http://local.invalid/v1",
		Model: "m", TimeoutSeconds: 30,
	}}
	next.Models.Default = "local"
	if err := cfg.Replace(next); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	reloaded, err := Load(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Server.Addr != "127.0.0.1:9999" || reloaded.Server.OpenBrowser {
		t.Fatalf("server not persisted: %+v", reloaded.Server)
	}
	if reloaded.Swarm.MaxConcurrent != 3 {
		t.Fatalf("swarm not persisted: %+v", reloaded.Swarm)
	}
	if !reloaded.Tools.IsDisabled("exec") || reloaded.Tools.IsDisabled("read") {
		t.Fatalf("tool toggles not persisted: %+v", reloaded.Tools.Disabled)
	}
	if !reloaded.Tools.Proxy.Enabled() {
		t.Fatal("proxy not persisted")
	}
	p, ok := reloaded.DefaultProvider()
	if !ok || p.ID != "local" || p.Timeout() != 30*time.Second {
		t.Fatalf("provider not persisted: %+v ok=%v", p, ok)
	}
	if err := cfg.Replace(nil); err == nil {
		t.Fatal("Replace(nil) must fail")
	}
	if err := (&Config{}).Save(); err == nil {
		t.Fatal("Save without a data dir must fail")
	}
}

func TestLoadRejectsBrokenYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("server: [this is not a map"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("a broken config file must fail loudly, not silently reset")
	}
}

func TestProviderHelpers(t *testing.T) {
	p := Provider{ID: "x"}
	if p.Ready() {
		t.Fatal("a provider with no base url is not ready")
	}
	if p.DisplayName() != "x" {
		t.Fatalf("DisplayName=%q", p.DisplayName())
	}
	p.Model = "m"
	if p.DisplayName() != "m" {
		t.Fatalf("DisplayName=%q", p.DisplayName())
	}
	p.Label = "Nice Name"
	if p.DisplayName() != "Nice Name" {
		t.Fatalf("DisplayName=%q", p.DisplayName())
	}
	if p.Timeout() != DefaultRequestTimeout {
		t.Fatalf("Timeout=%v", p.Timeout())
	}
	p.BaseURL = "http://x.invalid/v1"
	if !p.Ready() {
		t.Fatal("base url + model is ready even without an api key")
	}

	cfg := Default()
	if _, ok := cfg.Provider("nope"); ok {
		t.Fatal("unknown provider must not resolve")
	}
	if _, ok := cfg.Provider(""); !ok {
		t.Fatal("empty id must resolve to the default provider")
	}
}

func TestSwarmAndLogHelpers(t *testing.T) {
	s := SwarmConfig{}
	if s.AgentTimeout() != DefaultAgentTimeoutSeconds*time.Second {
		t.Fatalf("AgentTimeout=%v", s.AgentTimeout())
	}
	s.AgentTimeoutSeconds = 5
	if s.AgentTimeout() != 5*time.Second {
		t.Fatalf("AgentTimeout=%v", s.AgentTimeout())
	}
	// An unset or nonsensical pulse interval must still pulse: a turn that
	// reports nothing is the problem the pulse exists to fix.
	if s.ProgressInterval() != DefaultProgressIntervalSeconds*time.Second {
		t.Fatalf("ProgressInterval=%v", s.ProgressInterval())
	}
	s.ProgressIntervalSeconds = 2
	if s.ProgressInterval() != 2*time.Second {
		t.Fatalf("ProgressInterval=%v", s.ProgressInterval())
	}
	for name, want := range map[string]int{
		"debug": -4, "info": 0, "warn": 4, "warning": 4, "error": 8, "": 0, "bogus": 0,
	} {
		c := &Config{Log: LogConfig{Level: name}}
		if got := c.SlogLevel(); got != want {
			t.Fatalf("SlogLevel(%q)=%d want %d", name, got, want)
		}
	}
	if !(ProxyConfig{HTTPS: "x"}).Enabled() {
		t.Fatal("https-only proxy counts as enabled")
	}
	if (ProxyConfig{}).Enabled() {
		t.Fatal("empty proxy is not enabled")
	}
}

func TestLoadEmptyDirUsesDefaultDataDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDataDir, dir)
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DataDir() != mustAbs(t, dir) {
		t.Fatalf("data dir=%q want %q", cfg.DataDir(), dir)
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// The thinking level goes on the wire as reasoning_effort, so an unknown value
// must collapse to the empty default rather than reaching an endpoint that
// rejects it. Only low/medium/high survive.
func TestNormalizeReasoningKeepsOnlyKnownLevels(t *testing.T) {
	cases := map[string]string{
		"low":     ReasoningLow,
		"MEDIUM":  ReasoningMedium,
		" high ":  ReasoningHigh,
		"":        ReasoningDefault,
		"extreme": ReasoningDefault,
		"none":    ReasoningDefault,
	}
	for in, want := range cases {
		if got := NormalizeReasoning(in); got != want {
			t.Fatalf("NormalizeReasoning(%q)=%q, want %q", in, got, want)
		}
	}
	if levels := ReasoningEfforts(); len(levels) != 3 ||
		levels[0] != ReasoningLow || levels[2] != ReasoningHigh {
		t.Fatalf("the UI needs low/medium/high in order, got %v", levels)
	}
}
