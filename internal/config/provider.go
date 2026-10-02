package config

import (
	"strings"
	"time"
)

// Provider is one OpenAI-compatible endpoint the swarm can talk to.
type Provider struct {
	ID      string `yaml:"id" json:"id"`
	Label   string `yaml:"label" json:"label"`
	BaseURL string `yaml:"base_url" json:"base_url"`
	APIKey  string `yaml:"api_key" json:"-"`
	Model   string `yaml:"model" json:"model"`
	// Catalog is the last discovered list of model names this endpoint
	// serves. The composer offers these without a provider row per name.
	// Empty means only Model is available until someone discovers.
	Catalog []string `yaml:"catalog" json:"catalog"`
	// TimeoutSeconds is how long a stream may stay silent after the first
	// byte. The wait for that first byte is DefaultFirstByteTimeout.
	// 0 means DefaultRequestTimeout.
	// A call that is still streaming is not cut off; a silent endpoint is.
	// Compact uses this idle clock; there is no second swarm compact timeout.
	TimeoutSeconds int `yaml:"timeout_seconds" json:"timeout_seconds"`
	// ContextWindow is the fallback token limit for this endpoint. Used when
	// a name is missing from ModelContext — never invented from the name.
	ContextWindow int `yaml:"context_window" json:"context_window"`
	// ModelContext is per-name windows, usually filled by Discover when the
	// listing included them. Empty for endpoints that only return names.
	ModelContext map[string]int `yaml:"model_context,omitempty" json:"model_context,omitempty"`
	// Enabled is whether the composer offers this endpoint. Nil means on:
	// a bool's zero value is false, and every file written before the
	// switch would otherwise disappear from the model list. False hides
	// it. A conversation that already picked it still runs — lookup does
	// not consult this flag.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

// WindowFor is this model's token limit: a discovered per-name value, else
// the provider fallback, else unknown (0). Zero means the UI shows a count
// without pretending to know how full the window is.
func (p Provider) WindowFor(model string) int {
	name := strings.TrimSpace(model)
	if name == "" {
		name = strings.TrimSpace(p.Model)
	}
	if n := p.ModelContext[name]; n > 0 {
		return n
	}
	if p.ContextWindow > 0 {
		return p.ContextWindow
	}
	return 0
}

// Timeout is how long a model call may stay silent.
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

// GroupName is the composer heading for this provider's models. It is the
// label the user typed, or the id — never the default model, which would
// make one endpoint look like a pile of unrelated names.
func (p Provider) GroupName() string {
	if s := strings.TrimSpace(p.Label); s != "" {
		return s
	}
	return p.ID
}

// Ready reports whether this provider has enough configuration to be called.
// An API key is not required: local endpoints frequently have none.
func (p Provider) Ready() bool {
	return p.EndpointReady() && strings.TrimSpace(p.Model) != ""
}

// EndpointReady reports whether the URL is set, which is enough to list
// models. A turn still needs Ready: a default model to actually call.
func (p Provider) EndpointReady() bool {
	return strings.TrimSpace(p.BaseURL) != ""
}

// Listed reports whether the composer model list includes this endpoint.
// Missing means on, so an older file stays visible.
func (p Provider) Listed() bool {
	return p.Enabled == nil || *p.Enabled
}

// Models is the names the UI can pick from: the discovered catalog, with the
// configured default included even if discovery never ran or missed it.
func (p Provider) Models() []string {
	return uniqueModels(append(append([]string{}, p.Catalog...), p.Model))
}

func uniqueModels(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func cleanModelContext(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for k, v := range in {
		k = strings.TrimSpace(k)
		if k == "" || v <= 0 {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (c *Config) anyProviderListed() bool {
	for _, p := range c.Models.Providers {
		if p.Listed() {
			return true
		}
	}
	return false
}
