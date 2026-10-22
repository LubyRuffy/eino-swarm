package config

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sync"
)

// Literal Config values used by embedders share a lock without a racy lazy write.
var literalConfigMu sync.RWMutex

func (c *Config) mutex() *sync.RWMutex {
	if c.mu == nil {
		return &literalConfigMu
	}
	return c.mu
}

func cloneProvider(p Provider) Provider {
	p.Catalog = slices.Clone(p.Catalog)
	p.BuiltinTools = cloneToolOption(reflect.ValueOf(p.BuiltinTools)).Interface().([]map[string]any)
	p.ModelContext = maps.Clone(p.ModelContext)
	if p.Enabled != nil {
		on := *p.Enabled
		p.Enabled = &on
	}
	return p
}

func (c *Config) clone() *Config {
	next := *c
	next.mu = &sync.RWMutex{}
	next.Models.Providers = slices.Clone(c.Models.Providers)
	for i, p := range next.Models.Providers {
		next.Models.Providers[i] = cloneProvider(p)
	}
	next.Tools.Disabled = slices.Clone(c.Tools.Disabled)
	next.Tools.Enabled = slices.Clone(c.Tools.Enabled)
	return &next
}

// Snapshot returns detached configuration for readers that cross a live save.
func (c *Config) Snapshot() *Config {
	mu := c.mutex()
	mu.RLock()
	defer mu.RUnlock()
	return c.clone()
}

func (c *Config) SwarmSnapshot() SwarmConfig {
	mu := c.mutex()
	mu.RLock()
	defer mu.RUnlock()
	return c.Swarm
}

func (c *Config) ModelsSnapshot() ModelsConfig {
	mu := c.mutex()
	mu.RLock()
	defer mu.RUnlock()
	m := c.Models
	m.Providers = slices.Clone(m.Providers)
	for i, p := range m.Providers {
		m.Providers[i] = cloneProvider(p)
	}
	return m
}

// Update merges with the latest state. Its callback may only edit the detached
// candidate; pool/runtime locks and side effects belong after this returns.
func (c *Config) Update(edit func(*Config) error) error {
	if edit == nil {
		return fmt.Errorf("config: nothing to update")
	}
	mu := c.mutex()
	mu.Lock()
	defer mu.Unlock()
	next := c.clone()
	if err := edit(next); err != nil {
		return err
	}
	if err := next.save(); err != nil {
		return err
	}
	c.publish(next)
	return nil
}

// Unchanged sections keep their backing storage, so a timeout edit does not
// write unrelated live settings. Readers of changed sections use snapshots.
func (c *Config) publish(n *Config) {
	if c.Server != n.Server {
		c.Server = n.Server
	}
	if !reflect.DeepEqual(c.Models, n.Models) {
		c.Models = n.Models
	}
	if c.Swarm != n.Swarm {
		c.Swarm = n.Swarm
	}
	if !reflect.DeepEqual(c.Tools, n.Tools) {
		c.Tools = n.Tools
	}
	if c.Memory != n.Memory {
		c.Memory = n.Memory
	}
	if c.Personality != n.Personality {
		c.Personality = n.Personality
	}
	if c.Log != n.Log {
		c.Log = n.Log
	}
	if c.UI != n.UI {
		c.UI = n.UI
	}
	if c.Remote != n.Remote {
		c.Remote = n.Remote
	}
	if c.Search != n.Search {
		c.Search = n.Search
	}
	if c.Clients != n.Clients {
		c.Clients = n.Clients
	}
}
