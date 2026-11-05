// Package mcpclient adapts explicitly configured MCP tools to the host toolset.
package mcpclient

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/components/tool"
)

// ToolDescriptor describes a remote tool discovered from an MCP server.
type ToolDescriptor struct {
	Name         string `json:"name"`
	OriginalName string `json:"original_name"`
	Description  string `json:"description"`
	InputSchema  any    `json:"input_schema"`
}

type Catalog struct {
	ServerID string           `json:"server_id"`
	Tools    []ToolDescriptor `json:"tools"`
}

// Error deliberately omits transport errors: those may contain credentials,
// command arguments or endpoint query parameters supplied by an MCP server.
type Error struct {
	Code     string `json:"code"`
	ServerID string `json:"server_id"`
}

func (e *Error) Error() string { return fmt.Sprintf("mcp: %s (%s)", e.Code, e.ServerID) }

// Manager shares sessions only within the same configuration and working
// directory. Tool wrappers retain a configuration snapshot, never a live pointer.
type Manager struct {
	mu          sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	closed      bool
	entries     map[string]*entry
	current     map[string]bool
	inspections sync.WaitGroup
}

const maxIdleSessions = 8

func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{ctx: ctx, cancel: cancel, entries: map[string]*entry{}}
}

func fingerprint(s config.MCPServer) string {
	// Env and Headers are intentionally excluded from MCPServer's JSON view.
	raw, _ := json.Marshal(struct {
		Server       config.MCPServer
		Env, Headers map[string]string
	}{s, s.Env, s.Headers})
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

func (m *Manager) acquire(s config.MCPServer, workspace string) (*entry, error) {
	fp := fingerprint(s)
	key := fp + "\x00" + workspace
	m.mu.Lock()
	var evicted *entry
	defer func() {
		m.mu.Unlock()
		if evicted != nil {
			evicted.close()
		}
	}()
	if m.closed {
		return nil, &Error{Code: "closed", ServerID: s.ID}
	}
	if m.current != nil && !m.current[fp] {
		return nil, &Error{Code: "session_expired", ServerID: s.ID}
	}
	if e := m.entries[key]; e != nil {
		e.uses++
		return e, nil
	}
	// Bound workspaces per configured server, not the number of configured
	// servers. Building one workspace must not evict its earlier server tools.
	if m.sessionCount(fp) >= maxIdleSessions {
		for k, e := range m.entries {
			if e.uses == 0 && e.fingerprint == fp {
				delete(m.entries, k)
				evicted = e
				break
			}
		}
	}
	ctx, cancel := context.WithCancel(m.ctx)
	e := &entry{server: s, workspace: workspace, fingerprint: fp, key: key, ctx: ctx, cancel: cancel, uses: 1, gate: make(chan struct{}, 1)}
	m.entries[key] = e
	return e, nil
}

func (m *Manager) release(e *entry, broken bool) {
	m.mu.Lock()
	e.uses--
	e.broken = e.broken || broken
	retired := m.current != nil && !m.current[e.fingerprint]
	remove := e.uses == 0 && (e.broken || retired || m.closed || m.sessionCount(e.fingerprint) > maxIdleSessions)
	if remove {
		delete(m.entries, e.key)
	}
	m.mu.Unlock()
	if remove {
		e.close()
	}
}

// sessionCount is called only while Manager.mu is held.
func (m *Manager) sessionCount(fingerprint string) int {
	count := 0
	for _, e := range m.entries {
		if e.fingerprint == fingerprint {
			count++
		}
	}
	return count
}

// Reload retires unused old sessions without interrupting a tool in flight.
// A retired tool returns session_expired on its next call; it never silently
// creates a fresh stateful session or executes with changed credentials.
func (m *Manager) Reload(servers []config.MCPServer) {
	current := map[string]bool{}
	for _, s := range servers {
		if s.Enabled {
			current[fingerprint(s)] = true
		}
	}
	m.mu.Lock()
	m.current = current
	var retired []*entry
	for k, e := range m.entries {
		if e.uses == 0 && !current[e.fingerprint] {
			delete(m.entries, k)
			retired = append(retired, e)
		}
	}
	m.mu.Unlock()
	for _, e := range retired {
		e.close()
	}
}

func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	m.cancel()
	entries := m.entries
	m.entries = map[string]*entry{}
	m.mu.Unlock()
	for _, e := range entries {
		e.close()
	}
	m.inspections.Wait()
}

// Inspect owns a short-lived session, so a connection test never leaves a
// local subprocess running or affects an active conversation's connection.
func (m *Manager) Inspect(ctx context.Context, s config.MCPServer, workspace string) (Catalog, error) {
	if err := ctx.Err(); err != nil {
		return Catalog{}, err
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return Catalog{}, &Error{Code: "closed", ServerID: s.ID}
	}
	parent := m.ctx
	m.inspections.Add(1)
	m.mu.Unlock()
	defer m.inspections.Done()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(parent, cancel)
	defer stop()
	inspection := NewManager()
	defer inspection.Close()
	e, err := inspection.acquire(cloneServer(s), workspace)
	if err != nil {
		return Catalog{}, err
	}
	defer inspection.release(e, false)
	_, catalog, err := e.connect(ctx)
	return catalog, err
}

// Tools only constructs static proxies. Discovery belongs to a running tool's
// cancellation context, never to turn admission, restoration or parked setup.
func (m *Manager) Tools(ctx context.Context, servers []config.MCPServer, workspace string, maxChars int) ([]tool.BaseTool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var out []tool.BaseTool
	for _, s := range servers {
		if !s.Enabled {
			continue
		}
		s = cloneServer(s)
		holder := &sessionHolder{manager: m, server: s, workspace: workspace}
		out = append(out, tools.WithResultLimit(&externalTool{holder: holder, list: true}, maxChars), tools.WithResultLimit(&externalTool{holder: holder}, maxChars))
	}
	return out, nil
}

func (m *Manager) retain(e *entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return &Error{Code: "closed", ServerID: e.server.ID}
	}
	if e.broken || m.entries[e.key] != e {
		return &Error{Code: "session_expired", ServerID: e.server.ID}
	}
	if m.current != nil && !m.current[e.fingerprint] {
		return &Error{Code: "session_expired", ServerID: e.server.ID}
	}
	e.uses++
	return nil
}

func cloneServer(s config.MCPServer) config.MCPServer {
	s.Args = slices.Clone(s.Args)
	s.Env = maps.Clone(s.Env)
	s.Headers = maps.Clone(s.Headers)
	return s
}
