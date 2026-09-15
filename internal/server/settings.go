package server

import (
	"net/http"

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
			// buttons that fail: revealing a file needs a desktop shell.
			"reveal": s.opts.Reveal != nil,
		},
		Swarm: cfg.Swarm,
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
	Swarm config.SwarmConfig `json:"swarm"`
	Tools config.ToolsConfig `json:"tools"`
	Log   config.LogConfig   `json:"log"`
}

type providerView struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	BaseURL        string `json:"base_url"`
	Model          string `json:"model"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	HasAPIKey      bool   `json:"has_api_key"`
	Ready          bool   `json:"ready"`
}

func toSettingsView(cfg *config.Config) settingsView {
	var v settingsView
	v.Server = cfg.Server
	v.Swarm = cfg.Swarm
	v.Tools = cfg.Tools
	v.Log = cfg.Log
	v.Models.Default = cfg.Models.Default
	for _, p := range cfg.Models.Providers {
		v.Models.Providers = append(v.Models.Providers, providerView{
			ID:             p.ID,
			Label:          p.Label,
			BaseURL:        p.BaseURL,
			Model:          p.Model,
			TimeoutSeconds: p.TimeoutSeconds,
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
			ID             string  `json:"id"`
			Label          string  `json:"label"`
			BaseURL        string  `json:"base_url"`
			Model          string  `json:"model"`
			TimeoutSeconds int     `json:"timeout_seconds"`
			APIKey         *string `json:"api_key"`
		} `json:"providers"`
	} `json:"models"`
	Swarm *config.SwarmConfig `json:"swarm"`
	Tools *config.ToolsConfig `json:"tools"`
	Log   *config.LogConfig   `json:"log"`
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
	if req.Log != nil {
		next.Log = *req.Log
	}
	if req.Models != nil {
		existing := map[string]string{}
		for _, p := range cfg.Models.Providers {
			existing[p.ID] = p.APIKey
		}
		providers := make([]config.Provider, 0, len(req.Models.Providers))
		for _, p := range req.Models.Providers {
			key := existing[p.ID]
			if p.APIKey != nil {
				key = *p.APIKey
			}
			providers = append(providers, config.Provider{
				ID:             p.ID,
				Label:          p.Label,
				BaseURL:        p.BaseURL,
				APIKey:         key,
				Model:          p.Model,
				TimeoutSeconds: p.TimeoutSeconds,
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
