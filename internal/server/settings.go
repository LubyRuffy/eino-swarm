package server

import (
	"net/http"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/gin-gonic/gin"
)

// fileEntryAlias lets the files handler extend engine.FileEntry without the
// engine depending on the HTTP layer.
type fileEntryAlias = engine.FileEntry

// metaView is what the UI reads once at startup to decide what to render:
// whether setup is still needed, which shell it is in, and what this build can
// do.
type metaView struct {
	Version         string             `json:"version"`
	Mode            string             `json:"mode"`
	Mock            bool               `json:"mock"`
	Configured      bool               `json:"configured"`
	DefaultProvider string             `json:"default_provider"`
	ReasoningLevels []string           `json:"reasoning_levels"`
	DataDir         string             `json:"data_dir"`
	Capabilities    map[string]bool    `json:"capabilities"`
	Swarm           config.SwarmConfig `json:"swarm"`
	Locale          string             `json:"locale"`
}

func (s *Server) getMeta(c *gin.Context) {
	cfg := s.engine.Config()
	pool := s.engine.Providers()
	c.JSON(http.StatusOK, metaView{
		Version:         s.opts.Version,
		Mode:            s.opts.Mode,
		Mock:            pool.IsMock(),
		Configured:      pool.IsMock() || cfg.Configured(),
		DefaultProvider: cfg.Models.Default,
		ReasoningLevels: config.ReasoningEfforts(),
		DataDir:         cfg.DataDir(),
		Capabilities: map[string]bool{
			// The UI hides affordances it cannot deliver rather than showing
			// buttons that fail: revealing a file needs a desktop shell,
			// opening a URL in the system browser does too, and a
			// project's memory switch would promise nothing while memory is
			// off for the whole install.
			"reveal":   s.opts.Reveal != nil,
			"open_url": s.opts.OpenURL != nil,
			"memory":   cfg.Memory.Enabled,
		},
		Swarm:  cfg.Swarm,
		Locale: cfg.UI.Locale,
	})
}

// settingsView is the editable configuration. The API key is write-only: it is
// returned as a "set / not set" flag so the settings dialog can show that one
// exists without handing it back out to anything that can read the endpoint.
type settingsView struct {
	Server config.ServerConfig `json:"server"`
	Models struct {
		Default   string         `json:"default"`
		Providers []providerView `json:"providers"`
	} `json:"models"`
	Swarm  config.SwarmConfig  `json:"swarm"`
	Tools  config.ToolsConfig  `json:"tools"`
	Memory config.MemoryConfig `json:"memory"`
	Log    config.LogConfig    `json:"log"`
	UI     config.UIConfig     `json:"ui"`
}

type providerView struct {
	ID             string         `json:"id"`
	Label          string         `json:"label"`
	BaseURL        string         `json:"base_url"`
	Model          string         `json:"model"`
	Catalog        []string       `json:"catalog"`
	TimeoutSeconds int            `json:"timeout_seconds"`
	ContextWindow  int            `json:"context_window"`
	ModelContext   map[string]int `json:"model_context"`
	HasAPIKey      bool           `json:"has_api_key"`
	Ready          bool           `json:"ready"`
}

func toSettingsView(cfg *config.Config) settingsView {
	var v settingsView
	v.Server = cfg.Server
	v.Swarm = cfg.Swarm
	v.Tools = cfg.Tools
	v.Memory = cfg.Memory
	v.Log = cfg.Log
	v.UI = cfg.UI
	v.Models.Default = cfg.Models.Default
	for _, p := range cfg.Models.Providers {
		catalog := p.Catalog
		if catalog == nil {
			catalog = []string{}
		}
		windows := p.ModelContext
		if windows == nil {
			windows = map[string]int{}
		}
		v.Models.Providers = append(v.Models.Providers, providerView{
			ID:             p.ID,
			Label:          p.Label,
			BaseURL:        p.BaseURL,
			Model:          p.Model,
			Catalog:        catalog,
			TimeoutSeconds: p.TimeoutSeconds,
			ContextWindow:  p.ContextWindow,
			ModelContext:   windows,
			HasAPIKey:      p.APIKey != "",
			Ready:          p.Ready(),
		})
	}
	return v
}

func (s *Server) getSettings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"settings": toSettingsView(s.engine.Config())})
}

// putSettingsRequest mirrors settingsView, plus the optional new API key per
// provider. An absent api_key keeps whatever is stored; an empty string
// clears it. That distinction is why the field is a pointer.
type putSettingsRequest struct {
	Server *config.ServerConfig `json:"server"`
	Models *struct {
		Default   string `json:"default"`
		Providers []struct {
			ID             string          `json:"id"`
			Label          string          `json:"label"`
			BaseURL        string          `json:"base_url"`
			Model          string          `json:"model"`
			Catalog        *[]string       `json:"catalog"`
			TimeoutSeconds int             `json:"timeout_seconds"`
			ContextWindow  *int            `json:"context_window"`
			ModelContext   *map[string]int `json:"model_context"`
			APIKey         *string         `json:"api_key"`
		} `json:"providers"`
	} `json:"models"`
	Swarm  *config.SwarmConfig  `json:"swarm"`
	Tools  *config.ToolsConfig  `json:"tools"`
	Memory *config.MemoryConfig `json:"memory"`
	Log    *config.LogConfig    `json:"log"`
	UI     *config.UIConfig     `json:"ui"`
}

func (s *Server) putSettings(c *gin.Context) {
	var req putSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	cfg := s.engine.Config()

	next := *cfg
	if req.Server != nil {
		next.Server = *req.Server
	}
	if req.Swarm != nil {
		next.Swarm = *req.Swarm
	}
	if req.Tools != nil {
		next.Tools = *req.Tools
	}
	if req.Memory != nil {
		next.Memory = *req.Memory
	}
	if req.Log != nil {
		next.Log = *req.Log
	}
	if req.UI != nil {
		next.UI = *req.UI
	}
	if req.Models != nil {
		existing := map[string]config.Provider{}
		for _, p := range cfg.Models.Providers {
			existing[p.ID] = p
		}
		providers := make([]config.Provider, 0, len(req.Models.Providers))
		for _, p := range req.Models.Providers {
			prev := existing[p.ID]
			key := prev.APIKey
			if p.APIKey != nil {
				key = *p.APIKey
			}
			catalog := prev.Catalog
			if p.Catalog != nil {
				catalog = *p.Catalog
			}
			window := prev.ContextWindow
			if p.ContextWindow != nil {
				window = *p.ContextWindow
			}
			modelCtx := prev.ModelContext
			if p.ModelContext != nil {
				modelCtx = *p.ModelContext
			}
			providers = append(providers, config.Provider{
				ID:             p.ID,
				Label:          p.Label,
				BaseURL:        p.BaseURL,
				APIKey:         key,
				Model:          p.Model,
				Catalog:        catalog,
				TimeoutSeconds: p.TimeoutSeconds,
				ContextWindow:  window,
				ModelContext:   modelCtx,
			})
		}
		if len(providers) == 0 {
			badRequest(c, "at least one model provider is required")
			return
		}
		next.Models = config.ModelsConfig{Default: req.Models.Default, Providers: providers}
	}

	if err := cfg.Replace(&next); err != nil {
		s.fail(c, err)
		return
	}
	// The next turn must use the new endpoint without a restart.
	s.engine.Providers().Invalidate()
	c.JSON(http.StatusOK, gin.H{"settings": toSettingsView(cfg)})
}

func (s *Server) getModels(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"models":  s.engine.Providers().List(),
		"default": s.engine.Config().Models.Default,
		"mock":    s.engine.Providers().IsMock(),
	})
}

type discoverModelsRequest struct {
	ProviderID string  `json:"provider_id"`
	BaseURL    string  `json:"base_url"`
	APIKey     *string `json:"api_key"`
}

func (s *Server) discoverModels(c *gin.Context) {
	var req discoverModelsRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	cfg := s.engine.Config()
	prov := config.Provider{
		BaseURL: strings.TrimSpace(req.BaseURL),
	}
	if id := strings.TrimSpace(req.ProviderID); id != "" {
		if stored, ok := cfg.Provider(id); ok {
			prov = stored
			if req.BaseURL != "" {
				prov.BaseURL = strings.TrimSpace(req.BaseURL)
			}
		} else if prov.BaseURL == "" {
			// An id we do not know, with no URL, cannot be listed. A URL
			// still can: Settings lets you Discover on a row you have not
			// saved yet, the same way you can type a default before Save.
			badRequest(c, "unknown provider %q", id)
			return
		}
	}
	if req.APIKey != nil {
		prov.APIKey = *req.APIKey
	}
	cat, err := s.engine.Providers().Discover(c.Request.Context(), prov)
	if err != nil {
		s.fail(c, err)
		return
	}
	windows := cat.Windows
	if windows == nil {
		windows = map[string]int{}
	}
	c.JSON(http.StatusOK, gin.H{"models": cat.Names, "context_windows": windows})
}

func (s *Server) getTools(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"catalog": tools.Catalog(),
		"enabled": tools.Enabled(s.engine.Config().Tools),
	})
}

// getTrace is the one-shot troubleshooting view: paste a turn id and get its
// whole timeline plus every model call it made.
func (s *Server) getTrace(c *gin.Context) {
	turnID := c.Param("turn")
	turn, err := s.engine.Store().GetTurn(turnID)
	if err != nil {
		s.fail(c, err)
		return
	}
	events, err := s.engine.Store().ListTurnEvents(turnID)
	if err != nil {
		s.fail(c, err)
		return
	}
	calls, err := s.engine.Store().ListLLMCalls(turnID)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"turn": turn, "events": events, "llm_calls": calls})
}
