// Package config is zwai's on-disk configuration: one YAML file under the
// data directory, loaded at startup and rewritten whenever the UI saves
// settings. Nothing in the codebase hardcodes an endpoint or a model name —
// everything a deployment needs to change lives here.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// EnvDataDir overrides the default data directory.
const EnvDataDir = "ZWAI_HOME"

// FileName is the config file's name inside the data directory.
const FileName = "config.yaml"

// Config is the whole configuration tree.
type Config struct {
	Server ServerConfig `yaml:"server" json:"server"`
	Models ModelsConfig `yaml:"models" json:"models"`
	Swarm  SwarmConfig  `yaml:"swarm" json:"swarm"`
	Tools  ToolsConfig  `yaml:"tools" json:"tools"`
	Memory MemoryConfig `yaml:"memory" json:"memory"`
	Log    LogConfig    `yaml:"log" json:"log"`

	// dataDir is where this config was loaded from. Not serialized: the file
	// cannot meaningfully record its own location.
	dataDir string `yaml:"-"`
}

// ServerConfig covers the HTTP surface both UIs are served from.
type ServerConfig struct {
	// Addr is the listen address for `zwai web`. Desktop mode always binds a
	// random loopback port instead.
	Addr string `yaml:"addr" json:"addr"`
	// OpenBrowser opens the default browser when `zwai web` starts.
	OpenBrowser bool `yaml:"open_browser" json:"open_browser"`
}

// Provider is one OpenAI-compatible endpoint the swarm can talk to.
type Provider struct {
	ID      string `yaml:"id" json:"id"`
	Label   string `yaml:"label" json:"label"`
	BaseURL string `yaml:"base_url" json:"base_url"`
	APIKey  string `yaml:"api_key" json:"-"`
	Model   string `yaml:"model" json:"model"`
	// TimeoutSeconds bounds a single model call. Long swarm answers need a
	// generous value; 0 means DefaultRequestTimeout.
	TimeoutSeconds int `yaml:"timeout_seconds" json:"timeout_seconds"`
}

// Timeout is the provider's per-request timeout.
func (p Provider) Timeout() time.Duration {
	if p.TimeoutSeconds <= 0 {
		return DefaultRequestTimeout
	}
	return time.Duration(p.TimeoutSeconds) * time.Second
}

// DisplayName is what the UI shows for this provider.
func (p Provider) DisplayName() string {
	if strings.TrimSpace(p.Label) != "" {
		return p.Label
	}
	if strings.TrimSpace(p.Model) != "" {
		return p.Model
	}
	return p.ID
}

// Ready reports whether this provider has enough configuration to be called.
// An API key is not required: local endpoints frequently have none.
func (p Provider) Ready() bool {
	return strings.TrimSpace(p.BaseURL) != "" && strings.TrimSpace(p.Model) != ""
}

// ModelsConfig is the provider list plus which one new conversations use.
type ModelsConfig struct {
	Default   string     `yaml:"default" json:"default"`
	Providers []Provider `yaml:"providers" json:"providers"`
}

// SwarmConfig bounds the agent swarm.
type SwarmConfig struct {
	MaxConcurrent        int `yaml:"max_concurrent" json:"max_concurrent"`
	AgentTimeoutSeconds  int `yaml:"agent_timeout_seconds" json:"agent_timeout_seconds"`
	MaxTurns             int `yaml:"max_turns" json:"max_turns"`
	ManagerMaxIterations int `yaml:"manager_max_iterations" json:"manager_max_iterations"`
	// ProgressIntervalSeconds is how often a running turn emits a progress
	// pulse. It is the only thing that moves on screen while every agent is
	// busy inside a long tool call, so it is a comfort setting, not a limit.
	ProgressIntervalSeconds int `yaml:"progress_interval_seconds" json:"progress_interval_seconds"`
	// DeltaCoalesceMS is how long streamed tokens wait to be sent as one
	// event. A token every few milliseconds would redraw the whole UI; one
	// pulse per interval keeps the screen moving without a frame per token.
	DeltaCoalesceMS int `yaml:"delta_coalesce_ms" json:"delta_coalesce_ms"`
}

// AgentTimeout is the per-sub-agent watchdog duration.
func (s SwarmConfig) AgentTimeout() time.Duration {
	if s.AgentTimeoutSeconds <= 0 {
		return DefaultAgentTimeoutSeconds * time.Second
	}
	return time.Duration(s.AgentTimeoutSeconds) * time.Second
}

// ProgressInterval is the gap between progress pulses during a running turn.
func (s SwarmConfig) ProgressInterval() time.Duration {
	if s.ProgressIntervalSeconds <= 0 {
		return DefaultProgressIntervalSeconds * time.Second
	}
	return time.Duration(s.ProgressIntervalSeconds) * time.Second
}

// DeltaCoalesce is how long streamed tokens wait to be sent as one event.
func (s SwarmConfig) DeltaCoalesce() time.Duration {
	if s.DeltaCoalesceMS <= 0 {
		return time.Duration(DefaultDeltaCoalesceMS) * time.Millisecond
	}
	return time.Duration(s.DeltaCoalesceMS) * time.Millisecond
}

// ProxyConfig is the outbound proxy applied to network tools.
type ProxyConfig struct {
	HTTP    string `yaml:"http" json:"http"`
	HTTPS   string `yaml:"https" json:"https"`
	NoProxy string `yaml:"no_proxy" json:"no_proxy"`
}

// Enabled reports whether any proxy is configured.
func (p ProxyConfig) Enabled() bool {
	return strings.TrimSpace(p.HTTP) != "" || strings.TrimSpace(p.HTTPS) != ""
}

// ToolsConfig controls the agents' toolset.
//
// Both lists record exceptions to a tool's default rather than enumerating the
// whole toolset, so a tool added in a later release behaves sensibly without
// anyone editing their config file: Disabled switches off a tool that is on by
// default, Enabled switches on one that is off by default (those need an
// external dependency, so they are opt-in).
type ToolsConfig struct {
	Disabled            []string    `yaml:"disabled" json:"disabled"`
	Enabled             []string    `yaml:"enabled" json:"enabled"`
	Proxy               ProxyConfig `yaml:"proxy" json:"proxy"`
	WebSearchMaxResults int         `yaml:"web_search_max_results" json:"web_search_max_results"`
}

// IsDisabled reports whether the named tool has been explicitly switched off.
func (t ToolsConfig) IsDisabled(name string) bool {
	return contains(t.Disabled, name)
}

// IsEnabled reports whether the named tool has been explicitly switched on.
func (t ToolsConfig) IsEnabled(name string) bool {
	return contains(t.Enabled, name)
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// MemoryConfig governs a project's memory: the notes carried into every turn
// and the skill documents the agents write for themselves.
//
// The character limit is the reason the rest of it works. Memory is injected
// into the system prompt, so an unbounded store would grow the prompt of every
// turn forever; a limit forces the agent to consolidate instead of accumulate.
type MemoryConfig struct {
	// Enabled turns the memory tools and the prompt sections on. Projects can
	// still opt out one at a time.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// AutoReview runs a review after each completed turn, which is what makes
	// memory grow without anyone being asked to maintain it.
	AutoReview bool `yaml:"auto_review" json:"auto_review"`
	// CharLimit bounds MEMORY.md. A write that would exceed it fails with the
	// current entries attached, rather than silently dropping the oldest.
	CharLimit int `yaml:"char_limit" json:"char_limit"`
	// ReviewMaxIterations caps the reviewer's ReAct loop. It reads one
	// conversation and writes a handful of files; a high cap only buys a
	// runaway.
	ReviewMaxIterations int `yaml:"review_max_iterations" json:"review_max_iterations"`
	// SkillsIndexMax is how many skills the prompt lists. Only the name and
	// the one-line summary are listed, so the agent pays for the index and not
	// for every procedure it might not need.
	SkillsIndexMax int `yaml:"skills_index_max" json:"skills_index_max"`
	// Notifications is how chatty a completed review is in the transcript:
	// off (nothing), on (one line naming what changed), verbose (the line
	// plus a preview of the written text).
	Notifications string `yaml:"notifications" json:"notifications"`
}

// ReviewIterations is the reviewer's iteration cap.
func (m MemoryConfig) ReviewIterations() int {
	if m.ReviewMaxIterations <= 0 {
		return DefaultReviewMaxIterations
	}
	return m.ReviewMaxIterations
}

// Limit is the MEMORY.md character budget.
func (m MemoryConfig) Limit() int {
	if m.CharLimit <= 0 {
		return DefaultMemoryCharLimit
	}
	return m.CharLimit
}

// IndexMax is how many skills the prompt lists.
func (m MemoryConfig) IndexMax() int {
	if m.SkillsIndexMax <= 0 {
		return DefaultSkillsIndexMax
	}
	return m.SkillsIndexMax
}

const (
	MemoryNotifyOff     = "off"
	MemoryNotifyOn      = "on"
	MemoryNotifyVerbose = "verbose"
)

// NotifyLevel is how a completed review is announced. An unknown value is
// treated as the default rather than as silence: a typo in the config must
// not hide that something was stored.
func (m MemoryConfig) NotifyLevel() string {
	switch m.Notifications {
	case MemoryNotifyOff, MemoryNotifyOn, MemoryNotifyVerbose:
		return m.Notifications
	default:
		return DefaultMemoryNotifications
	}
}

// LogConfig configures slog.
type LogConfig struct {
	// Level is one of debug, info, warn, error.
	Level string `yaml:"level" json:"level"`
}

// Reasoning-effort levels. These are the OpenAI `reasoning_effort` values, not
// an app setting, so they live as constants rather than in the config file. An
// empty level means "let the model decide": no reasoning_effort is sent, which
// is what keeps a non-reasoning endpoint from being handed a field it rejects.
const (
	ReasoningDefault = ""
	ReasoningLow     = "low"
	ReasoningMedium  = "medium"
	ReasoningHigh    = "high"
)

// ReasoningEfforts is the ordered set of explicit levels the UI offers. The
// empty default is rendered as "Default" and is not one of these values.
func ReasoningEfforts() []string {
	return []string{ReasoningLow, ReasoningMedium, ReasoningHigh}
}

// NormalizeReasoning maps any input to a known level, falling back to the
// empty default so a typo or an old value never sends an invalid one on the
// wire.
func NormalizeReasoning(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case ReasoningLow:
		return ReasoningLow
	case ReasoningMedium:
		return ReasoningMedium
	case ReasoningHigh:
		return ReasoningHigh
	default:
		return ReasoningDefault
	}
}

// Defaults, all overridable from the config file.
const (
	DefaultAddr                = "127.0.0.1:8787"
	DefaultRequestTimeout      = 5 * time.Minute
	DefaultMaxConcurrent       = 6
	DefaultAgentTimeoutSeconds = 600
	DefaultMaxTurns            = 24
	DefaultManagerIterations   = 32
	// A pulse every few seconds is frequent enough that a silent swarm still
	// looks alive, and rare enough to be invisible next to streamed tokens.
	DefaultProgressIntervalSeconds = 5
	// One pulse per 50ms is ~20 frames a second: fast enough that streamed
	// text still looks live, slow enough that a five-agent swarm does not
	// spend the UI's whole budget redrawing the same markdown.
	DefaultDeltaCoalesceMS  = 50
	DefaultWebSearchResults = 8
	DefaultProviderID       = "default"
	DefaultLogLevel         = "info"
	// About 800 tokens: enough for a dozen dense notes, small enough that
	// every turn can afford to carry them.
	DefaultMemoryCharLimit     = 2200
	DefaultReviewMaxIterations = 8
	DefaultSkillsIndexMax      = 50
	DefaultMemoryNotifications = MemoryNotifyOn
	dirPerm                    = 0o700
	filePerm                   = 0o600
)

// Default returns the configuration a fresh install starts with. The single
// provider is intentionally blank: the UI walks the user through filling it in,
// and OPENAI_* environment variables seed it when present.
func Default() *Config {
	return &Config{
		Server: ServerConfig{Addr: DefaultAddr, OpenBrowser: true},
		Models: ModelsConfig{
			Default: DefaultProviderID,
			Providers: []Provider{{
				ID:             DefaultProviderID,
				Label:          "",
				BaseURL:        "",
				APIKey:         "",
				Model:          "",
				TimeoutSeconds: int(DefaultRequestTimeout / time.Second),
			}},
		},
		Swarm: SwarmConfig{
			MaxConcurrent:           DefaultMaxConcurrent,
			AgentTimeoutSeconds:     DefaultAgentTimeoutSeconds,
			MaxTurns:                DefaultMaxTurns,
			ManagerMaxIterations:    DefaultManagerIterations,
			ProgressIntervalSeconds: DefaultProgressIntervalSeconds,
			DeltaCoalesceMS:         DefaultDeltaCoalesceMS,
		},
		Tools: ToolsConfig{
			Disabled:            []string{},
			Enabled:             []string{},
			WebSearchMaxResults: DefaultWebSearchResults,
		},
		Memory: MemoryConfig{
			Enabled:             true,
			AutoReview:          true,
			CharLimit:           DefaultMemoryCharLimit,
			ReviewMaxIterations: DefaultReviewMaxIterations,
			SkillsIndexMax:      DefaultSkillsIndexMax,
			Notifications:       DefaultMemoryNotifications,
		},
		Log: LogConfig{Level: DefaultLogLevel},
	}
}

// DefaultDataDir is ZWAI_HOME when set, else ~/.zwai-swarm.
func DefaultDataDir() string {
	if v := strings.TrimSpace(os.Getenv(EnvDataDir)); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".zwai-swarm"
	}
	return filepath.Join(home, ".zwai-swarm")
}

// Load reads the config file under dataDir, creating the directory tree and a
// default config file when they do not exist yet. An empty dataDir means
// DefaultDataDir.
func Load(dataDir string) (*Config, error) {
	if strings.TrimSpace(dataDir) == "" {
		dataDir = DefaultDataDir()
	}
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("config: resolve data dir: %w", err)
	}
	for _, d := range []string{abs, filepath.Join(abs, workspacesDirName), filepath.Join(abs, projectsDirName)} {
		if err := os.MkdirAll(d, dirPerm); err != nil {
			return nil, fmt.Errorf("config: create %s: %w", d, err)
		}
	}

	cfg := Default()
	cfg.dataDir = abs

	raw, err := os.ReadFile(filepath.Join(abs, FileName))
	switch {
	case err == nil:
		if err := yaml.Unmarshal(raw, cfg); err != nil {
			return nil, fmt.Errorf("config: parse %s: %w", filepath.Join(abs, FileName), err)
		}
	case os.IsNotExist(err):
		// first run: fall through and write the seeded defaults below
	default:
		return nil, fmt.Errorf("config: read: %w", err)
	}

	cfg.dataDir = abs
	cfg.seedFromEnv()
	cfg.normalize()

	if os.IsNotExist(err) {
		if err := cfg.Save(); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

// seedFromEnv fills blank provider fields from OPENAI_* variables. It only
// fills blanks, so it can never silently override what a user configured in
// the UI, and no endpoint or model name is baked into the binary.
func (c *Config) seedFromEnv() {
	base := firstEnv("ZWAI_MODEL_BASE_URL", "OPENAI_BASE_URL")
	key := firstEnv("ZWAI_MODEL_API_KEY", "OPENAI_API_KEY")
	name := firstEnv("ZWAI_MODEL_NAME", "OPENAI_MODEL")
	if base == "" && key == "" && name == "" {
		return
	}
	if len(c.Models.Providers) == 0 {
		c.Models.Providers = []Provider{{ID: DefaultProviderID}}
	}
	idx := c.providerIndex(c.Models.Default)
	if idx < 0 {
		idx = 0
	}
	p := &c.Models.Providers[idx]
	if strings.TrimSpace(p.BaseURL) == "" {
		p.BaseURL = base
	}
	if strings.TrimSpace(p.APIKey) == "" {
		p.APIKey = key
	}
	if strings.TrimSpace(p.Model) == "" {
		p.Model = name
	}
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

// normalize repairs a hand-edited or partially-filled config so the rest of
// the process never has to second-guess it.
func (c *Config) normalize() {
	d := Default()
	if strings.TrimSpace(c.Server.Addr) == "" {
		c.Server.Addr = d.Server.Addr
	}
	if c.Swarm.MaxConcurrent <= 0 {
		c.Swarm.MaxConcurrent = d.Swarm.MaxConcurrent
	}
	if c.Swarm.AgentTimeoutSeconds <= 0 {
		c.Swarm.AgentTimeoutSeconds = d.Swarm.AgentTimeoutSeconds
	}
	if c.Swarm.MaxTurns <= 0 {
		c.Swarm.MaxTurns = d.Swarm.MaxTurns
	}
	if c.Swarm.ManagerMaxIterations <= 0 {
		c.Swarm.ManagerMaxIterations = d.Swarm.ManagerMaxIterations
	}
	if c.Swarm.ProgressIntervalSeconds <= 0 {
		c.Swarm.ProgressIntervalSeconds = d.Swarm.ProgressIntervalSeconds
	}
	if c.Swarm.DeltaCoalesceMS <= 0 {
		c.Swarm.DeltaCoalesceMS = d.Swarm.DeltaCoalesceMS
	}
	if c.Tools.WebSearchMaxResults <= 0 {
		c.Tools.WebSearchMaxResults = d.Tools.WebSearchMaxResults
	}
	// Only the budgets are repaired. The two switches are booleans a user may
	// legitimately have set to false, and "repairing" a false to the default
	// would turn memory back on behind their back.
	if c.Memory.CharLimit <= 0 {
		c.Memory.CharLimit = d.Memory.CharLimit
	}
	if c.Memory.ReviewMaxIterations <= 0 {
		c.Memory.ReviewMaxIterations = d.Memory.ReviewMaxIterations
	}
	if c.Memory.SkillsIndexMax <= 0 {
		c.Memory.SkillsIndexMax = d.Memory.SkillsIndexMax
	}
	switch c.Memory.Notifications {
	case MemoryNotifyOff, MemoryNotifyOn, MemoryNotifyVerbose:
	default:
		c.Memory.Notifications = d.Memory.Notifications
	}
	// A nil slice marshals to JSON null, which the settings UI would have to
	// guard on every read; keep the wire shape a list.
	if c.Tools.Disabled == nil {
		c.Tools.Disabled = []string{}
	}
	if c.Tools.Enabled == nil {
		c.Tools.Enabled = []string{}
	}
	if strings.TrimSpace(c.Log.Level) == "" {
		c.Log.Level = d.Log.Level
	}
	if len(c.Models.Providers) == 0 {
		c.Models.Providers = d.Models.Providers
	}
	// de-duplicate ids and give anonymous providers one, so the UI can address
	// every row and Save round-trips.
	seen := map[string]bool{}
	for i := range c.Models.Providers {
		p := &c.Models.Providers[i]
		p.ID = strings.TrimSpace(p.ID)
		if p.ID == "" || seen[p.ID] {
			p.ID = fmt.Sprintf("provider-%d", i+1)
		}
		seen[p.ID] = true
		if p.TimeoutSeconds <= 0 {
			p.TimeoutSeconds = int(DefaultRequestTimeout / time.Second)
		}
	}
	if c.providerIndex(c.Models.Default) < 0 {
		c.Models.Default = c.Models.Providers[0].ID
	}
}

func (c *Config) providerIndex(id string) int {
	for i, p := range c.Models.Providers {
		if p.ID == id {
			return i
		}
	}
	return -1
}

// Provider looks up a provider by id; an empty id means the default one.
func (c *Config) Provider(id string) (Provider, bool) {
	if strings.TrimSpace(id) == "" {
		id = c.Models.Default
	}
	if i := c.providerIndex(id); i >= 0 {
		return c.Models.Providers[i], true
	}
	return Provider{}, false
}

// DefaultProvider returns the provider new conversations use.
func (c *Config) DefaultProvider() (Provider, bool) {
	return c.Provider(c.Models.Default)
}

// Configured reports whether the app can actually run a turn. The UI shows a
// setup banner until this is true.
func (c *Config) Configured() bool {
	p, ok := c.DefaultProvider()
	return ok && p.Ready()
}

// DataDir is the directory this config was loaded from.
func (c *Config) DataDir() string { return c.dataDir }

// Path is the config file's full path.
func (c *Config) Path() string { return filepath.Join(c.dataDir, FileName) }

// DBPath is the SQLite database file's full path.
func (c *Config) DBPath() string { return filepath.Join(c.dataDir, "zwai.db") }

// Directory names under the data directory. They are also what tells a
// managed directory apart from a path the user chose: deleting a conversation
// may remove a directory under WorkspacesDir, and never one outside it.
const (
	workspacesDirName = "workspaces"
	projectsDirName   = "projects"
	// A project's own subdirectories: the working directory zwai manages when
	// the user did not name one, and the memory store.
	projectWorkspaceName = "workspace"
	projectMemoryName    = "memory"
)

// WorkspacesDir is the parent of every conversation's own workspace.
func (c *Config) WorkspacesDir() string { return filepath.Join(c.dataDir, workspacesDirName) }

// WorkspaceDir is one conversation's workspace: the directory relative tool
// paths resolve against and the one the Files panel shows. It is an anchor,
// not a boundary — see the access model in internal/tools.
func (c *Config) WorkspaceDir(threadID string) string {
	return filepath.Join(c.WorkspacesDir(), threadID)
}

// ProjectsDir is the parent of every project's managed directory.
func (c *Config) ProjectsDir() string { return filepath.Join(c.dataDir, projectsDirName) }

// ProjectDir is one project's managed directory.
func (c *Config) ProjectDir(projectID string) string {
	return filepath.Join(c.ProjectsDir(), projectID)
}

// ProjectWorkspaceDir is the working directory of a project that did not name
// one of its own.
func (c *Config) ProjectWorkspaceDir(projectID string) string {
	return filepath.Join(c.ProjectDir(projectID), projectWorkspaceName)
}

// ProjectMemoryDir holds a project's MEMORY.md and its skills.
//
// It sits under the data directory rather than inside the project's working
// directory on purpose: the agents rewrite it after most turns, and a store
// that lived in the user's repository would show up as unexplained changes in
// every `git status`.
func (c *Config) ProjectMemoryDir(projectID string) string {
	return filepath.Join(c.ProjectDir(projectID), projectMemoryName)
}

// Save writes the config back to disk atomically, so a crash mid-write cannot
// leave a truncated file that fails to parse on the next start.
func (c *Config) Save() error {
	if strings.TrimSpace(c.dataDir) == "" {
		return fmt.Errorf("config: no data dir; load the config before saving it")
	}
	c.normalize()
	raw, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	if err := os.MkdirAll(c.dataDir, dirPerm); err != nil {
		return fmt.Errorf("config: create %s: %w", c.dataDir, err)
	}
	tmp := c.Path() + ".tmp"
	if err := os.WriteFile(tmp, raw, filePerm); err != nil {
		return fmt.Errorf("config: write: %w", err)
	}
	if err := os.Rename(tmp, c.Path()); err != nil {
		return fmt.Errorf("config: replace: %w", err)
	}
	return nil
}

// Replace overwrites the mutable parts of c with those of next and persists
// the result. The data directory is never taken from the caller: it is where
// this config already lives.
func (c *Config) Replace(next *Config) error {
	if next == nil {
		return fmt.Errorf("config: nothing to apply")
	}
	c.Server = next.Server
	c.Models = next.Models
	c.Swarm = next.Swarm
	c.Tools = next.Tools
	c.Memory = next.Memory
	c.Log = next.Log
	return c.Save()
}

// SlogLevel maps the configured level name to its slog value, defaulting to
// info for anything unrecognized.
func (c *Config) SlogLevel() int {
	switch strings.ToLower(strings.TrimSpace(c.Log.Level)) {
	case "debug":
		return -4
	case "warn", "warning":
		return 4
	case "error":
		return 8
	default:
		return 0
	}
}
