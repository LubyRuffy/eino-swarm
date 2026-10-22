package config

import (
	"errors"
	"math"
	"strings"
	"time"
)

var ErrTimeoutSettings = errors.New("config: invalid timeout settings")

type TimeoutSettings struct {
	AgentTimeoutSeconds int                       `json:"agent_timeout_seconds"`
	Providers           []ProviderTimeoutSettings `json:"providers"`
}

type ProviderTimeoutSettings struct {
	ID                               string `json:"id"`
	Label                            string `json:"label"`
	TimeoutSeconds                   int    `json:"timeout_seconds"`
	FirstByteTimeoutSeconds          int    `json:"first_byte_timeout_seconds"`
	EffectiveFirstByteTimeoutSeconds int    `json:"effective_first_byte_timeout_seconds"`
}

type TimeoutPatch struct {
	AgentTimeoutSeconds *int                   `json:"agent_timeout_seconds,omitempty"`
	Providers           []ProviderTimeoutPatch `json:"providers,omitempty"`
}

type ProviderTimeoutPatch struct {
	ID                      string `json:"id"`
	TimeoutSeconds          *int   `json:"timeout_seconds,omitempty"`
	FirstByteTimeoutSeconds *int   `json:"first_byte_timeout_seconds,omitempty"`
}

func (c *Config) Timeouts() TimeoutSettings {
	s := c.Snapshot()
	v := TimeoutSettings{AgentTimeoutSeconds: int(s.Swarm.AgentTimeout() / time.Second), Providers: []ProviderTimeoutSettings{}}
	for _, p := range s.Models.Providers {
		v.Providers = append(v.Providers, ProviderTimeoutSettings{
			ID: p.ID, Label: p.GroupName(), TimeoutSeconds: int(p.Timeout() / time.Second),
			FirstByteTimeoutSeconds:          int(p.FirstByteTimeout() / time.Second),
			EffectiveFirstByteTimeoutSeconds: int(p.FirstByteTimeout() / time.Second),
		})
	}
	return v
}

func validTimeout(n *int) bool {
	return n == nil || (*n > 0 && int64(*n) <= math.MaxInt64/int64(time.Second))
}

// ApplyTimeoutPatch only edits a detached candidate inside Config.Update.
func ApplyTimeoutPatch(c *Config, p TimeoutPatch) error {
	if !validTimeout(p.AgentTimeoutSeconds) {
		return ErrTimeoutSettings
	}
	changed := p.AgentTimeoutSeconds != nil
	seen := map[string]bool{}
	for _, row := range p.Providers {
		id := strings.TrimSpace(row.ID)
		if id == "" || id != row.ID || seen[id] || (row.TimeoutSeconds == nil && row.FirstByteTimeoutSeconds == nil) || !validTimeout(row.TimeoutSeconds) || !validTimeout(row.FirstByteTimeoutSeconds) {
			return ErrTimeoutSettings
		}
		seen[id] = true
		i := c.providerIndex(id)
		if i < 0 {
			return ErrTimeoutSettings
		}
		if row.TimeoutSeconds != nil {
			c.Models.Providers[i].TimeoutSeconds = *row.TimeoutSeconds
		}
		if row.FirstByteTimeoutSeconds != nil {
			c.Models.Providers[i].FirstByteTimeoutSeconds = *row.FirstByteTimeoutSeconds
		}
		changed = true
	}
	if !changed {
		return ErrTimeoutSettings
	}
	if p.AgentTimeoutSeconds != nil {
		c.Swarm.AgentTimeoutSeconds = *p.AgentTimeoutSeconds
	}
	return nil
}
