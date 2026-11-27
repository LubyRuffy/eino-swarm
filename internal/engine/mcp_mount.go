package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/mcpclient"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/components/tool"
)

type mcpMountServer struct {
	ID      string   `json:"id"`
	Mode    string   `json:"mode"`
	Error   string   `json:"error,omitempty"`
	Remotes []string `json:"remotes,omitempty"`
	Skipped []string `json:"skipped,omitempty"`
}

type mcpMountRecord struct {
	Mode         string                     `json:"mode"`
	Percent      int                        `json:"percent"`
	Window       int                        `json:"window"`
	Budget       int                        `json:"budget"`
	SchemaTokens int                        `json:"schema_tokens"`
	Servers      []mcpMountServer           `json:"servers"`
	PanelTools   map[string]panelMCPBinding `json:"panel_tools,omitempty"`
}

func mcpMountBudget(window, fallback int) int {
	if window > 0 {
		return window
	}
	if fallback > 0 {
		return fallback
	}
	return 0
}

func mcpSchemaTokens(tools []mcpclient.ToolDescriptor) int {
	var b strings.Builder
	for _, item := range tools {
		raw, _ := json.Marshal(item.InputSchema)
		name := item.OriginalName
		if name == "" {
			name = item.Name
		}
		b.WriteString(name)
		b.WriteByte('\n')
		b.WriteString(item.Description)
		b.WriteByte('\n')
		b.Write(raw)
		b.WriteByte('\n')
	}
	return estimateTextTokens(b.String())
}

func toolsetHasMCP(set *tools.Set) bool {
	if set == nil {
		return false
	}
	for _, name := range set.Names {
		if strings.HasPrefix(name, "mcp_") {
			return true
		}
	}
	return false
}

func withMCPTools(set *tools.Set, extra []tool.BaseTool, extraNames []string) *tools.Set {
	next := *set
	next.MCPAdmission = nil
	next.PanelMCP = nil
	next.Tools = make([]tool.BaseTool, 0, len(set.Names)+len(extra))
	next.Names = make([]string, 0, len(set.Names)+len(extraNames))
	inserted := false
	for i, name := range set.Names {
		if strings.HasPrefix(name, "mcp_") {
			continue
		}
		if !inserted && name == ToolQueryHistory {
			next.Tools = append(next.Tools, extra...)
			next.Names = append(next.Names, extraNames...)
			inserted = true
		}
		if i < len(set.Tools) {
			next.Tools = append(next.Tools, set.Tools[i])
		}
		next.Names = append(next.Names, name)
	}
	if !inserted {
		next.Tools = append(next.Tools, extra...)
		next.Names = append(next.Names, extraNames...)
	}
	return &next
}

func mcpMountPath(dir, turnID string) (string, error) {
	if dir == "" || turnID == "" || strings.Contains(turnID, "..") || strings.ContainsAny(turnID, `/\`) {
		return "", fmt.Errorf("mcp mount path")
	}
	return filepath.Join(dir, "mcp-mounts", turnID+".json"), nil
}

// applyMCPSurface freezes MCP tools for this turn. A new turn may list on
// ctx. A resumed turn rebuilds the stored names and does not dial.
func (e *Engine) applyMCPSurface(ctx context.Context, turn *store.Turn, set *tools.Set, resumed bool) (*tools.Set, error) {
	if e == nil || e.mcp == nil || turn == nil || !toolsetHasMCP(set) {
		return set, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if record, ok := e.loadMCPMount(turn.ID); ok {
		e.rememberRemoteRoutes(turn.ID, e.routesForMount(record))
		return e.rebuildMCPSurface(set, record), nil
	}
	if resumed {
		e.rememberRemoteRoutes(turn.ID, nil)
		return set, nil
	}
	next, record, err := e.resolveMCPSurface(ctx, turn, set)
	if err != nil {
		return nil, err
	}
	e.rememberRemoteRoutes(turn.ID, e.routesForMount(record))
	e.storeMCPMount(turn, record)
	return next, nil
}

func (e *Engine) rememberRemoteRoutes(turnID string, routes map[string]string) {
	if e == nil || turnID == "" {
		return
	}
	if len(routes) == 0 {
		e.remoteRoutes.Delete(turnID)
		return
	}
	cloned := make(map[string]string, len(routes))
	for name, call := range routes {
		if name == "" {
			continue
		}
		cloned[name] = call
	}
	if len(cloned) == 0 {
		e.remoteRoutes.Delete(turnID)
		return
	}
	e.remoteRoutes.Store(turnID, cloned)
}

func (e *Engine) bindRemoteToolRoutes(ctx context.Context, turnID string) context.Context {
	if e == nil || turnID == "" {
		return ctx
	}
	value, ok := e.remoteRoutes.LoadAndDelete(turnID)
	if !ok {
		return ctx
	}
	routes, _ := value.(map[string]string)
	return swarm.WithRemoteToolRoutes(ctx, routes)
}

func (e *Engine) routesForMount(record mcpMountRecord) map[string]string {
	if e == nil || e.cfg == nil {
		return nil
	}
	snap := e.cfg.Snapshot()
	byID := make(map[string]config.MCPServer, len(snap.MCP.Servers))
	for _, server := range snap.MCP.Servers {
		byID[server.ID] = server
	}
	routes := map[string]string{}
	for _, item := range record.Servers {
		if item.Mode == "direct" {
			continue
		}
		server, ok := byID[item.ID]
		if !ok {
			continue
		}
		catalog, _ := mcpclient.LoadCatalog(e.cfg.DataDir(), server)
		for name, call := range remoteCallRoutes(server, catalog) {
			if prev, exists := routes[name]; exists && prev != call {
				routes[name] = ""
				continue
			}
			routes[name] = call
		}
	}
	if len(routes) == 0 {
		return nil
	}
	return routes
}

func remoteCallRoutes(server config.MCPServer, catalog mcpclient.Catalog) map[string]string {
	mountable, _ := mcpclient.PrepareDirect(server, catalog, true, nil)
	_, call := mcpclient.ProxyNames(server.ID)
	if call == "" {
		return nil
	}
	routes := make(map[string]string, len(mountable))
	for _, remote := range mountable {
		name := remote.OriginalName
		if name == "" {
			name = remote.Name
		}
		if name == "" {
			continue
		}
		routes[name] = call
	}
	return routes
}

func (e *Engine) resolveMCPSurface(ctx context.Context, turn *store.Turn, set *tools.Set) (*tools.Set, mcpMountRecord, error) {
	snap := e.cfg.Snapshot()
	admission := set.MCPAdmission
	if admission == nil {
		admission = &tools.MCPAdmission{Config: snap.MCP, Panel: snap.TaskPanels.EASM}
	}
	percent := admission.Config.DirectLoadPercent
	window := 0
	if e.pool != nil {
		window = e.pool.WindowFor(turn.ProviderID, turn.Model)
	}
	budget := mcpMountBudget(window, snap.Swarm.AutoCompactLimit())
	record := mcpMountRecord{Percent: percent, Window: window, Budget: budget}
	dir := e.cfg.DataDir()
	type ready struct {
		server        config.MCPServer
		catalog       mcpclient.Catalog
		uncachedProxy bool
		errCode       string
	}
	var rows []ready
	for _, server := range admission.Config.Servers {
		if !server.Enabled {
			continue
		}
		if ctx.Err() != nil {
			return nil, mcpMountRecord{}, ctx.Err()
		}
		if percent <= 0 {
			catalog, cached := mcpclient.LoadCatalog(dir, server)
			rows = append(rows, ready{server: server, catalog: catalog, uncachedProxy: !cached})
			continue
		}
		if catalog, ok := mcpclient.LoadCatalog(dir, server); ok {
			rows = append(rows, ready{server: server, catalog: catalog})
			continue
		}
		catalog, err := e.mcp.Inspect(ctx, server, set.WorkspaceDir)
		if ctx.Err() != nil {
			return nil, mcpMountRecord{}, ctx.Err()
		}
		if err != nil {
			code := "connection_failed"
			var failure *mcpclient.Error
			if errors.As(err, &failure) && failure.Code != "" {
				code = failure.Code
			}
			rows = append(rows, ready{server: server, errCode: code})
			continue
		}
		e.mcp.RememberCatalog(server, catalog)
		rows = append(rows, ready{server: server, catalog: catalog})
	}
	var measured []mcpclient.ToolDescriptor
	for _, row := range rows {
		if row.errCode != "" {
			continue
		}
		mount, _ := mcpclient.PrepareDirect(row.server, row.catalog, true, nil)
		measured = append(measured, mount...)
	}
	record.SchemaTokens = mcpSchemaTokens(measured)
	direct := config.FitsDirectLoad(percent, budget, record.SchemaTokens)
	maxChars := snap.Tools.ToolResultLimit()
	var extra []tool.BaseTool
	var names []string
	mounted := make([]mountedPanelMCP, 0, len(rows))
	for _, row := range rows {
		item := mcpMountServer{ID: row.server.ID, Mode: "progressive"}
		mountable, skipped := mcpclient.PrepareDirect(row.server, row.catalog, true, nil)
		item.Skipped = skipped
		if row.errCode != "" {
			item.Error = row.errCode
		}
		if direct && row.errCode == "" {
			item.Mode = "direct"
			for _, remote := range mountable {
				name := remote.OriginalName
				if name == "" {
					name = remote.Name
				}
				item.Remotes = append(item.Remotes, name)
			}
			built, builtNames := e.mcp.DirectTools(row.server, set.WorkspaceDir, maxChars, mountable)
			extra = append(extra, built...)
			names = append(names, builtNames...)
			mounted = append(mounted, mountedPanelMCP{server: row.server, tools: mountable})
			record.Servers = append(record.Servers, item)
			continue
		}
		index := ""
		if row.errCode == "" {
			index = mcpclient.ToolIndex(mountable)
		}
		proxies, proxyNames, err := e.mcp.IndexedProxies(row.server, set.WorkspaceDir, maxChars, index)
		if err != nil {
			item.Error = "invalid_schema"
		} else {
			extra = append(extra, proxies...)
			names = append(names, proxyNames...)
			mounted = append(mounted, mountedPanelMCP{server: row.server, tools: mountable, uncachedProxy: row.uncachedProxy})
		}
		record.Servers = append(record.Servers, item)
	}
	record.Mode = "direct"
	for _, item := range record.Servers {
		if item.Mode != "direct" {
			record.Mode = "progressive"
			break
		}
	}
	if len(record.Servers) == 0 {
		record.Mode = "progressive"
	}
	record.PanelTools = panelMCPBindings(admission.Panel, mounted)
	next := withMCPTools(set, extra, names)
	bindPanelMCP(next, record.PanelTools, admission.Config.Servers)
	return next, record, nil
}

func (e *Engine) rebuildMCPSurface(set *tools.Set, record mcpMountRecord) *tools.Set {
	snap := e.cfg.Snapshot()
	dir := e.cfg.DataDir()
	maxChars := snap.Tools.ToolResultLimit()
	byID := make(map[string]config.MCPServer, len(snap.MCP.Servers))
	for _, server := range snap.MCP.Servers {
		byID[server.ID] = server
	}
	var extra []tool.BaseTool
	var names []string
	for _, item := range record.Servers {
		server, ok := byID[item.ID]
		if !ok {
			continue
		}
		if item.Mode == "direct" {
			catalog, ok := mcpclient.LoadCatalog(dir, server)
			if !ok {
				continue
			}
			only := make(map[string]struct{}, len(item.Remotes))
			for _, name := range item.Remotes {
				only[name] = struct{}{}
			}
			mount, _ := mcpclient.PrepareDirect(server, catalog, false, only)
			built, builtNames := e.mcp.DirectTools(server, set.WorkspaceDir, maxChars, mount)
			extra = append(extra, built...)
			names = append(names, builtNames...)
			continue
		}
		catalog, _ := mcpclient.LoadCatalog(dir, server)
		mount, _ := mcpclient.PrepareDirect(server, catalog, true, nil)
		proxies, proxyNames, err := e.mcp.IndexedProxies(server, set.WorkspaceDir, maxChars, mcpclient.ToolIndex(mount))
		if err != nil {
			continue
		}
		extra = append(extra, proxies...)
		names = append(names, proxyNames...)
	}
	next := withMCPTools(set, extra, names)
	bindPanelMCP(next, record.PanelTools, snap.MCP.Servers)
	return next
}

func (e *Engine) storeMCPMount(turn *store.Turn, record mcpMountRecord) {
	body, err := json.Marshal(record)
	if err != nil || turn == nil {
		return
	}
	if path, err := mcpMountPath(e.cfg.DataDir(), turn.ID); err == nil {
		_ = os.MkdirAll(filepath.Dir(path), 0o700)
		tmp := path + ".tmp"
		if os.WriteFile(tmp, body, 0o600) == nil && os.Chmod(tmp, 0o600) == nil {
			_ = os.Rename(tmp, path)
		}
	}
	e.record(store.Event{
		ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: KindMCPCatalog,
		AgentID: swarm.DefaultManagerID, Text: string(body),
	})
}

func (e *Engine) loadMCPMount(turnID string) (mcpMountRecord, bool) {
	if e == nil || e.cfg == nil {
		return mcpMountRecord{}, false
	}
	path, err := mcpMountPath(e.cfg.DataDir(), turnID)
	if err != nil {
		return mcpMountRecord{}, false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return mcpMountRecord{}, false
	}
	var record mcpMountRecord
	if json.Unmarshal(body, &record) != nil || record.Mode == "" {
		return mcpMountRecord{}, false
	}
	return record, true
}

// republishWorkerSurface replaces the tools a registry already snapshotted at
// construction. ConfigureWorkers is what new workers read.
func (e *Engine) republishWorkerSurface(reg *swarm.Registry, toolset *tools.Set, pc *projectContext, turn *store.Turn) {
	if e == nil || reg == nil {
		return
	}
	e.wireWorkerSurface(reg, toolset, pc, turn)
	e.wireWorkerModelRecovery(reg, turn)
	e.wireGraphRegistry(reg, turn)
	e.wireTaskPanelRegistry(reg, toolset, turn)
	config := reg.WorkerConfiguration()
	config.SubAgentTools = append([]tool.BaseTool(nil), reg.SubAgentTools...)
	config.WorkerPreamble = reg.WorkerPreamble
	config.RestoreWorkerModel = reg.RestoreWorkerModel
	reg.ConfigureWorkers(config)
}

func (e *Engine) mcpOffered(servers []config.MCPServer, name string) bool {
	if e == nil || e.mcp == nil {
		return mcpclient.ProxyOffered(servers, name)
	}
	return e.mcp.ToolOffered(servers, name)
}
