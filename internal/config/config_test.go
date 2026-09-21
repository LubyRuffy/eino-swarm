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
	if _, err := os.Stat(cfg.InputsDir()); err != nil {
		t.Fatalf("first run must create the pasted-images dir: %v", err)
	}
	if cfg.ThreadInputsDir("t1") != filepath.Join(cfg.InputsDir(), "t1") {
		t.Fatalf("thread inputs dir=%q", cfg.ThreadInputsDir("t1"))
	}
	if cfg.ThreadPlanFile("t1") != filepath.Join(cfg.PlansDir(), "t1", "PLAN.md") {
		t.Fatalf("plan file=%q", cfg.ThreadPlanFile("t1"))
	}
	if _, err := os.Stat(cfg.ProjectsDir()); err != nil {
		t.Fatalf("first run must create the projects dir: %v", err)
	}
}

// A project's memory must land under the data directory, not inside the
// working directory the user pointed at: the agents rewrite it after most
// turns, and a store inside a repository would pollute every `git status`.
func TestProjectPathsStayUnderTheDataDirectory(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for name, got := range map[string]string{
		"project":   cfg.ProjectDir("pj_1"),
		"workspace": cfg.ProjectWorkspaceDir("pj_1"),
		"memory":    cfg.ProjectMemoryDir("pj_1"),
	} {
		if !strings.HasPrefix(got, cfg.ProjectsDir()+string(filepath.Separator)) {
			t.Fatalf("%s dir %q escaped %q", name, got, cfg.ProjectsDir())
		}
	}
	if cfg.ProjectWorkspaceDir("pj_1") == cfg.ProjectMemoryDir("pj_1") {
		t.Fatal("the working directory and the memory store must not be the same directory")
	}
}

// The two switches are the user's call. A hand-edited `false` that normalize
// "repaired" would turn memory back on behind their back, which is exactly the
// surprise the setting exists to prevent.
func TestMemorySwitchesSurviveNormalizeAndBudgetsAreRepaired(t *testing.T) {
	dir := t.TempDir()
	raw := strings.Join([]string{
		"memory:",
		"  enabled: false",
		"  auto_review: false",
		"  char_limit: 0",
		"  entry_max: 0",
		"  review_max_iterations: -1",
		"  skills_index_max: 0",
		"  notifications: shouting",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Memory.Enabled || cfg.Memory.AutoReview {
		t.Fatalf("normalize switched memory back on: %+v", cfg.Memory)
	}
	if cfg.Memory.CharLimit != DefaultMemoryCharLimit ||
		cfg.Memory.EntryMax != DefaultMemoryEntryMax ||
		cfg.Memory.ReviewMaxIterations != DefaultReviewMaxIterations ||
		cfg.Memory.SkillsIndexMax != DefaultSkillsIndexMax ||
		cfg.Memory.Notifications != DefaultMemoryNotifications {
		t.Fatalf("memory budgets not repaired: %+v", cfg.Memory)
	}

	// A config written before this feature existed has no memory block at all,
	// and must come up with memory on rather than half-configured.
	fresh := t.TempDir()
	if err := os.WriteFile(filepath.Join(fresh, FileName), []byte("log:\n  level: info\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	older, err := Load(fresh)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !older.Memory.Enabled || !older.Memory.AutoReview ||
		older.Memory.Notifications != DefaultMemoryNotifications ||
		older.Memory.EntryMax != DefaultMemoryEntryMax {
		t.Fatalf("an older config must default to memory on: %+v", older.Memory)
	}
	if !older.Swarm.AutoTitle {
		t.Fatal("an older config must keep naming conversations")
	}
}

func TestEntryMaxAboveTheNotesBudgetIsClamped(t *testing.T) {
	dir := t.TempDir()
	raw := "memory:\n  char_limit: 200\n  entry_max: 900\n"
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Memory.EntryMax != 200 {
		t.Fatalf("a per-note cap above the total must clamp: %+v", cfg.Memory)
	}
}

func TestAutoTitleFalseSurvivesNormalize(t *testing.T) {
	dir := t.TempDir()
	raw := "swarm:\n  auto_title: false\n"
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Swarm.AutoTitle {
		t.Fatal("normalize switched auto-title back on")
	}
}

func TestResolveTitleFollowsTheConversationUnlessPinned(t *testing.T) {
	const convProvider = "conv"
	const convModel = "heavy"
	auto := SwarmConfig{}
	p, m := auto.ResolveTitle(convProvider, convModel)
	if p != convProvider || m != convModel {
		t.Fatalf("empty pin must follow the conversation: %q %q", p, m)
	}

	nameOnly := SwarmConfig{TitleModel: "tiny"}
	p, m = nameOnly.ResolveTitle(convProvider, convModel)
	if p != convProvider || m != "tiny" {
		t.Fatalf("a model with no provider must stay on the conversation's endpoint: %q %q", p, m)
	}

	provOnly := SwarmConfig{TitleProvider: "other"}
	p, m = provOnly.ResolveTitle(convProvider, convModel)
	if p != "other" || m != "" {
		t.Fatalf("a provider with no model must use that endpoint's default: %q %q", p, m)
	}

	pinned := SwarmConfig{TitleProvider: "other", TitleModel: "tiny"}
	p, m = pinned.ResolveTitle(convProvider, convModel)
	if p != "other" || m != "tiny" {
		t.Fatalf("a full pin must win: %q %q", p, m)
	}

	compact := SwarmConfig{CompactProvider: "other", CompactModel: "tiny"}
	p, m = compact.ResolveCompact(convProvider, convModel)
	if p != "other" || m != "tiny" {
		t.Fatalf("compact pin must win: %q %q", p, m)
	}
	autoC := SwarmConfig{}
	p, m = autoC.ResolveCompact(convProvider, convModel)
	if p != convProvider || m != convModel {
		t.Fatalf("empty compact pin must follow the conversation: %q %q", p, m)
	}
}

func TestUnknownTitleProviderIsCleared(t *testing.T) {
	dir := t.TempDir()
	raw := "swarm:\n  title_provider: gone\n  title_model: tiny\n"
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Swarm.TitleProvider != "" || cfg.Swarm.TitleModel != "" {
		t.Fatalf("a deleted endpoint must not stay pinned: %+v", cfg.Swarm)
	}
}

func TestUnknownCompactProviderIsCleared(t *testing.T) {
	dir := t.TempDir()
	raw := "swarm:\n  compact_provider: gone\n  compact_model: tiny\n"
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Swarm.CompactProvider != "" || cfg.Swarm.CompactModel != "" {
		t.Fatalf("a deleted compact endpoint must not stay pinned: %+v", cfg.Swarm)
	}
}

func TestManagerIterationsFallsBackToDefault(t *testing.T) {
	if (SwarmConfig{}).ManagerIterations() != DefaultManagerIterations {
		t.Fatalf("empty config: %d", SwarmConfig{}.ManagerIterations())
	}
	if (SwarmConfig{ManagerMaxIterations: -3}).ManagerIterations() != DefaultManagerIterations {
		t.Fatal("a negative cap must not disable the limit")
	}
	if got := (SwarmConfig{ManagerMaxIterations: 50}).ManagerIterations(); got != 50 {
		t.Fatalf("got %d", got)
	}
	if DefaultManagerIterations != 200 {
		t.Fatalf("the documented default is 200, got %d", DefaultManagerIterations)
	}
}

func TestMemoryHelpersFallBackToDefaults(t *testing.T) {
	m := MemoryConfig{}
	if m.Limit() != DefaultMemoryCharLimit {
		t.Fatalf("Limit=%d", m.Limit())
	}
	if m.ReviewIterations() != DefaultReviewMaxIterations {
		t.Fatalf("ReviewIterations=%d", m.ReviewIterations())
	}
	if m.IndexMax() != DefaultSkillsIndexMax {
		t.Fatalf("IndexMax=%d", m.IndexMax())
	}
	if m.NotifyLevel() != DefaultMemoryNotifications {
		t.Fatalf("NotifyLevel=%s", m.NotifyLevel())
	}
	if m.EntryLimit() != DefaultMemoryEntryMax {
		t.Fatalf("EntryLimit=%d", m.EntryLimit())
	}
	m = MemoryConfig{CharLimit: 10, EntryMax: 50, ReviewMaxIterations: 2, SkillsIndexMax: 3,
		Notifications: MemoryNotifyVerbose}
	if m.Limit() != 10 || m.EntryLimit() != 10 || m.ReviewIterations() != 2 || m.IndexMax() != 3 ||
		m.NotifyLevel() != MemoryNotifyVerbose {
		t.Fatalf("configured values ignored: %+v", m)
	}
	m.EntryMax = 8
	if m.EntryLimit() != 8 {
		t.Fatalf("EntryLimit should keep a cap under the total: %d", m.EntryLimit())
	}
	m.Notifications = "nope"
	if m.NotifyLevel() != DefaultMemoryNotifications {
		t.Fatal("an unknown notification mode must not go silent")
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
		"  schedule_min_interval_seconds: 0",
		"  schedule_tick_ms: -1",
		"  schedule_max_active: 0",
		// leftover wall-clock key: Load must ignore it, not fail
		"  goal_session_max_seconds: -3",
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
	if cfg.Swarm.DeltaCoalesceMS != DefaultDeltaCoalesceMS {
		t.Fatalf("delta coalesce not repaired: %+v", cfg.Swarm)
	}
	if cfg.Swarm.ContextCharBudget != DefaultContextCharBudget {
		t.Fatalf("context budget not repaired: %+v", cfg.Swarm)
	}
	if cfg.Swarm.CompactKeepMessages != DefaultCompactKeepMessages {
		t.Fatalf("compact keep not repaired: %+v", cfg.Swarm)
	}
	if cfg.Swarm.GoalMaxAutoTurns != DefaultGoalMaxAutoTurns {
		t.Fatalf("goal auto-continue cap not repaired: %+v", cfg.Swarm)
	}
	if cfg.Swarm.ScheduleMinIntervalSeconds != DefaultScheduleMinIntervalSeconds ||
		cfg.Swarm.ScheduleTickMS != DefaultScheduleTickMS ||
		cfg.Swarm.ScheduleMaxActive != DefaultScheduleMaxActive {
		t.Fatalf("schedule caps not repaired: %+v", cfg.Swarm)
	}
	if cfg.Swarm.GoalSessionMaxIterations != DefaultGoalSessionMaxIterations ||
		cfg.Swarm.GoalAutoCompactPercent != DefaultGoalAutoCompactPercent {
		t.Fatalf("goal session bounds not repaired: %+v", cfg.Swarm)
	}
	if cfg.Swarm.AutoCompactTokens != DefaultAutoCompactTokens {
		t.Fatalf("auto-compact token budget not repaired: %+v", cfg.Swarm)
	}
	if !cfg.Swarm.AutoTitle {
		t.Fatal("an older config without auto_title must keep naming conversations")
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

func TestObsoleteGoalSessionMaxSecondsIsIgnored(t *testing.T) {
	dir := t.TempDir()
	raw := "swarm:\n  goal_session_max_seconds: 600\n  goal_max_auto_turns: 12\n"
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Swarm.GoalMaxAutoTurns != 12 {
		t.Fatalf("known keys must still load: %+v", cfg.Swarm)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "goal_session_max_seconds") {
		t.Fatal("Save must drop the wall-clock key")
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
	next.Swarm.AutoTitle = false
	next.Swarm.TitleProvider = "local"
	next.Swarm.TitleModel = "tiny"
	next.Swarm.AutoCompactTokens = 12_000
	next.Tools.Disabled = []string{"exec"}
	next.Tools.Proxy = ProxyConfig{HTTP: "http://127.0.0.1:7890", NoProxy: "localhost"}
	next.Memory.AutoReview = false
	next.Memory.CharLimit = 1200
	next.Memory.Notifications = MemoryNotifyOff
	next.Personality.Instructions = "prefer compact replies"
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
	if reloaded.Swarm.AutoTitle {
		t.Fatal("auto_title false did not survive the round trip")
	}
	if reloaded.Swarm.AutoCompactTokens != 12_000 {
		t.Fatalf("auto-compact token budget not persisted: %+v", reloaded.Swarm)
	}
	if reloaded.Swarm.TitleProvider != "local" || reloaded.Swarm.TitleModel != "tiny" {
		t.Fatalf("title pin did not survive the round trip: %+v", reloaded.Swarm)
	}
	if !reloaded.Tools.IsDisabled("exec") || reloaded.Tools.IsDisabled("read") {
		t.Fatalf("tool toggles not persisted: %+v", reloaded.Tools.Disabled)
	}
	if !reloaded.Tools.Proxy.Enabled() {
		t.Fatal("proxy not persisted")
	}
	if reloaded.Memory.AutoReview || reloaded.Memory.CharLimit != 1200 ||
		reloaded.Memory.Notifications != MemoryNotifyOff {
		t.Fatalf("memory settings not persisted: %+v", reloaded.Memory)
	}
	if reloaded.Personality.Instructions != "prefer compact replies" {
		t.Fatalf("personality not persisted: %+v", reloaded.Personality)
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
	p.Label = ""
	if p.GroupName() != "x" {
		t.Fatalf("an unnamed provider groups as its id, not the model, got %q", p.GroupName())
	}
	p.Label = "Nice Name"
	if p.GroupName() != "Nice Name" {
		t.Fatalf("GroupName=%q", p.GroupName())
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

	if p.EndpointReady() && !p.Ready() {
		t.Fatal("a default model is still required to run a turn")
	}
}

// A provider is one endpoint, not one model: the catalog is what the composer
// lists, and the default is just the name new conversations start on.
func TestProviderCatalogDedupesAndIncludesTheDefault(t *testing.T) {
	p := Provider{
		Model:   "alpha",
		Catalog: []string{" beta ", "alpha", "", "gamma", "beta"},
	}
	got := p.Models()
	want := []string{"beta", "alpha", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("Models=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Models=%v want %v", got, want)
		}
	}

	blank := Provider{}
	if names := blank.Models(); len(names) != 0 {
		t.Fatalf("an empty provider listed models: %v", names)
	}

	dir := t.TempDir()
	raw := strings.Join([]string{
		"models:",
		"  providers:",
		"    - id: default",
		"      catalog:",
		"        - one",
		"        - one",
		"        - two",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Models.Providers[0].Catalog; len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("catalog not cleaned on load: %v", got)
	}
	if cfg.Models.Providers[0].Catalog == nil {
		t.Fatal("catalog must be a list, not null")
	}
}

func TestSwarmScheduleDefaultsRepairZero(t *testing.T) {
	cfg := Default()
	if cfg.Swarm.ScheduleMinInterval() != DefaultScheduleMinIntervalSeconds*time.Second {
		t.Fatalf("min interval=%s", cfg.Swarm.ScheduleMinInterval())
	}
	if cfg.Swarm.ScheduleTick() != time.Duration(DefaultScheduleTickMS)*time.Millisecond {
		t.Fatalf("tick=%s", cfg.Swarm.ScheduleTick())
	}
	if cfg.Swarm.MaxActiveSchedules() != DefaultScheduleMaxActive {
		t.Fatalf("max active=%d", cfg.Swarm.MaxActiveSchedules())
	}
	zero := SwarmConfig{}
	if zero.ScheduleMinInterval() != DefaultScheduleMinIntervalSeconds*time.Second ||
		zero.ScheduleTick() != time.Duration(DefaultScheduleTickMS)*time.Millisecond ||
		zero.MaxActiveSchedules() != DefaultScheduleMaxActive {
		t.Fatal("zero values must repair")
	}
	neg := SwarmConfig{
		ScheduleMinIntervalSeconds: -3,
		ScheduleTickMS:             -1,
		ScheduleMaxActive:          -8,
	}
	if neg.ScheduleMinInterval() != DefaultScheduleMinIntervalSeconds*time.Second ||
		neg.ScheduleTick() != time.Duration(DefaultScheduleTickMS)*time.Millisecond ||
		neg.MaxActiveSchedules() != DefaultScheduleMaxActive {
		t.Fatal("negative values must repair")
	}
	custom := SwarmConfig{
		ScheduleMinIntervalSeconds: 45,
		ScheduleTickMS:             250,
		ScheduleMaxActive:          4,
	}
	if custom.ScheduleMinInterval() != 45*time.Second ||
		custom.ScheduleTick() != 250*time.Millisecond ||
		custom.MaxActiveSchedules() != 4 {
		t.Fatalf("explicit values must stick: %+v", custom)
	}
}

func TestCompactBudgetsRepairFromZero(t *testing.T) {
	if (SwarmConfig{}).ContextBudget() != DefaultContextCharBudget {
		t.Fatal("a zero budget must repair")
	}
	if (SwarmConfig{}).CompactKeep() != DefaultCompactKeepMessages {
		t.Fatal("a zero keep must repair")
	}
	if (SwarmConfig{}).GoalAutoTurns() != DefaultGoalMaxAutoTurns {
		t.Fatal("a zero goal auto-continue cap must repair")
	}
	if (SwarmConfig{}).GoalSessionIterations() != DefaultGoalSessionMaxIterations {
		t.Fatal("a zero goal session iteration cap must repair")
	}
	if (SwarmConfig{}).GoalCompactPercent() != DefaultGoalAutoCompactPercent {
		t.Fatal("a zero goal compact percent must repair")
	}
	if (SwarmConfig{GoalAutoCompactPercent: 140}).GoalCompactPercent() != 100 {
		t.Fatal("a compact percent over 100 must clamp")
	}
	if (SwarmConfig{}).AutoCompactLimit() != DefaultAutoCompactTokens {
		t.Fatal("a zero auto-compact token budget must repair")
	}
	s := SwarmConfig{ContextCharBudget: 12_000, CompactKeepMessages: 3, AutoCompactTokens: 12_000}
	if s.ContextBudget() != 12_000 || s.CompactKeep() != 3 || s.AutoCompactLimit() != 12_000 {
		t.Fatalf("explicit values must stick: %+v", s)
	}
}

func TestProviderWindowPrefersADiscoveredNameOverTheFallback(t *testing.T) {
	p := Provider{
		Model:         "alpha",
		ContextWindow: 8000,
		ModelContext:  map[string]int{"alpha": 32000, "beta": 16000},
	}
	if got := p.WindowFor("alpha"); got != 32000 {
		t.Fatalf("named window=%d", got)
	}
	if got := p.WindowFor(""); got != 32000 {
		t.Fatalf("empty name should follow the default model: %d", got)
	}
	if got := p.WindowFor("gamma"); got != 8000 {
		t.Fatalf("unknown name should use the fallback: %d", got)
	}
	if got := (Provider{}).WindowFor("alpha"); got != 0 {
		t.Fatalf("unknown must stay 0, not invent a window: %d", got)
	}
}

func TestNormalizeDropsJunkContextWindows(t *testing.T) {
	dir := t.TempDir()
	raw := strings.Join([]string{
		"models:",
		"  providers:",
		"    - id: default",
		"      context_window: -12",
		"      model_context:",
		"        alpha: 128000",
		"        \"\": 9",
		"        junk: 0",
		"        beta: -3",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p := cfg.Models.Providers[0]
	if p.ContextWindow != 0 {
		t.Fatalf("negative fallback survived: %d", p.ContextWindow)
	}
	if len(p.ModelContext) != 1 || p.ModelContext["alpha"] != 128000 {
		t.Fatalf("junk windows not pruned: %v", p.ModelContext)
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
	if s.DeltaCoalesce() != time.Duration(DefaultDeltaCoalesceMS)*time.Millisecond {
		t.Fatalf("DeltaCoalesce=%v", s.DeltaCoalesce())
	}
	s.DeltaCoalesceMS = 16
	if s.DeltaCoalesce() != 16*time.Millisecond {
		t.Fatalf("DeltaCoalesce=%v", s.DeltaCoalesce())
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

// Chrome language is a preference, not a third protocol. Junk, blanks and a
// config written before this key existed must all come up as follow-the-system
// rather than a blank UI or an invented language.
func TestUILocaleNormalizesToSystemEnOrZh(t *testing.T) {
	cases := map[string]string{
		"en":     LocaleEn,
		"ZH":     LocaleZh,
		" zh ":   LocaleZh,
		"system": LocaleSystem,
		"":       LocaleSystem,
		"fr":     LocaleSystem,
		"zh-CN":  LocaleSystem,
	}
	for in, want := range cases {
		if got := NormalizeLocale(in); got != want {
			t.Fatalf("NormalizeLocale(%q)=%q, want %q", in, got, want)
		}
	}

	fresh := t.TempDir()
	if err := os.WriteFile(filepath.Join(fresh, FileName), []byte("log:\n  level: info\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	older, err := Load(fresh)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if older.UI.Locale != LocaleSystem {
		t.Fatalf("an older config must follow the system language: %+v", older.UI)
	}
	if older.UI.Font != DefaultFont || older.UI.FontSize != DefaultFontSize ||
		older.UI.ContentWidth != DefaultContentWidth ||
		older.UI.Palette != DefaultPalette {
		t.Fatalf("an older config must keep the current column and type: %+v", older.UI)
	}

	pinned := t.TempDir()
	if err := os.WriteFile(filepath.Join(pinned, FileName), []byte("ui:\n  locale: zh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(pinned)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.UI.Locale != LocaleZh {
		t.Fatalf("a pinned language must survive load: %+v", got.UI)
	}
}
