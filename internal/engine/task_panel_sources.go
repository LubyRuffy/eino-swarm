package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/mcpclient"
	"github.com/LubyRuffy/eino-swarm/internal/panelplugins/easm"
	"github.com/LubyRuffy/eino-swarm/internal/panelplugins/fofaworld"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
)

// panelProducer is one registered source. current is the panel state the call
// starts from, so a producer can merge into what the user already sees.
type panelProducer func(context.Context, json.RawMessage, json.RawMessage, config.TaskPanelConfig, *tools.Set, func(context.Context, json.RawMessage) error) (any, error)

// This registry is the only host wiring that knows the bundled producers.
// A producer receives its input, settings and a publisher; it never adds
// business fields to the publication protocol or the turn lifecycle.
func (e *Engine) panelProducerFor(id string) panelProducer {
	switch id {
	case "fofa-world-map":
		return runFOFAWorldSource
	case "easm-asset-portrait":
		return e.runEASMSource
	}
	return nil
}

func runFOFAWorldSource(ctx context.Context, raw json.RawMessage, _ json.RawMessage, settings config.TaskPanelConfig, set *tools.Set, publish func(context.Context, json.RawMessage) error) (any, error) {
	var input struct {
		Query     string `json:"query"`
		MaxAssets int    `json:"max_assets,omitempty"`
	}
	if err := decodeToolArgs(string(raw), &input); err != nil {
		return nil, err
	}
	maxAssets := settings.MaxAssets
	if input.MaxAssets < 0 || input.MaxAssets > maxAssets {
		return nil, fmt.Errorf("max_assets exceeds configured source limit")
	}
	if input.MaxAssets > 0 {
		maxAssets = input.MaxAssets
	}
	return fofaworld.Run(ctx, fofaworld.Input{Query: input.Query, ClientPath: settings.FOFAClient, Workspace: set.WorkspaceDir, MaxAssets: maxAssets, BatchSize: min(settings.BatchSize, maxAssets), Concurrency: settings.Concurrency, Timeout: time.Duration(settings.TimeoutSeconds) * time.Second, FlushInterval: time.Duration(settings.FlushIntervalMS) * time.Millisecond}, publish)
}

func (e *Engine) runEASMSource(ctx context.Context, raw json.RawMessage, current json.RawMessage, settings config.TaskPanelConfig, set *tools.Set, publish func(context.Context, json.RawMessage) error) (any, error) {
	var input easm.Input
	if err := decodeToolArgs(string(raw), &input); err != nil {
		return nil, err
	}
	names := make(map[string]string, len(set.PanelMCP))
	servers := make(map[string]config.MCPServer, len(set.PanelMCP))
	invoker, _ := swarm.ToolCaller(ctx)
	workerHasTool := func(name string) bool { return swarm.ToolCallerHasWorkerTool(ctx, name) }
	for role, binding := range set.PanelMCP {
		if invoker.AgentID != swarm.DefaultManagerID && !panelMCPToolMounted(binding.Server.ID, binding.Name, workerHasTool) {
			continue
		}
		names[role] = binding.Name
		servers[binding.Name] = binding.Server
	}
	caller := &easmCaller{manager: e.mcp, servers: servers, workspace: set.WorkspaceDir}
	limits := easm.Settings{
		Tools:         names,
		MaxDomains:    settings.EASM.MaxDomains,
		MaxComponents: settings.EASM.MaxComponents,
		MaxClouds:     settings.EASM.MaxClouds,
		MaxServices:   settings.EASM.MaxServices,
	}
	return easm.Run(ctx, input, limits, current, caller, publish)
}

// panelMCPBinding stores only identities alongside the turn's mount receipt.
type panelMCPBinding struct {
	ServerID    string `json:"server_id"`
	Name        string `json:"name"`
	Destination string `json:"destination"`
}

type mountedPanelMCP struct {
	server        config.MCPServer
	tools         []mcpclient.ToolDescriptor
	uncachedProxy bool
}

func panelMCPBindings(settings config.EASMPanelConfig, mounted []mountedPanelMCP) map[string]panelMCPBinding {
	bindings := make(map[string]panelMCPBinding)
	for role, name := range settings.Tools {
		if name == "" {
			continue
		}
		var matched config.MCPServer
		for _, candidate := range mounted {
			if settings.ServerID != "" && candidate.server.ID != settings.ServerID {
				continue
			}
			offered := slices.ContainsFunc(candidate.tools, func(d mcpclient.ToolDescriptor) bool {
				return d.OriginalName == name || d.OriginalName == "" && d.Name == name
			})
			// A cold progressive proxy can call a configured name directly.
			// Automatic selection still needs a catalog to identify its server.
			configured := candidate.uncachedProxy && settings.ServerID != "" &&
				!slices.Contains(candidate.server.DisabledTools, name)
			if !offered && !configured {
				continue
			}
			if matched.ID != "" {
				matched = config.MCPServer{}
				break
			}
			matched = candidate.server
		}
		if matched.ID != "" {
			bindings[role] = panelMCPBinding{ServerID: matched.ID, Name: name, Destination: panelMCPDestination(matched)}
		}
	}
	return bindings
}

func bindPanelMCP(set *tools.Set, bindings map[string]panelMCPBinding, servers []config.MCPServer) {
	if len(bindings) == 0 {
		return
	}
	set.PanelMCP = make(map[string]tools.PanelMCPTool, len(bindings))
	mounted := func(name string) bool { return slices.Contains(set.Names, name) }
	for role, binding := range bindings {
		if !panelMCPToolMounted(binding.ServerID, binding.Name, mounted) {
			continue
		}
		for _, server := range servers {
			if server.ID == binding.ServerID && server.Enabled && panelMCPDestination(server) == binding.Destination {
				set.PanelMCP[role] = tools.PanelMCPTool{Name: binding.Name, Server: server}
				break
			}
		}
	}
}

func panelMCPToolMounted(serverID, name string, mounted func(string) bool) bool {
	_, call := mcpclient.ProxyNames(serverID)
	return mounted(call) || mounted(mcpclient.DirectToolName(serverID, name))
}

// The mount receipt can distinguish a replaced server without storing its
// connection details. Credential rotation uses the existing holder rules.
func panelMCPDestination(server config.MCPServer) string {
	body, _ := json.Marshal(struct {
		Transport, URL, Command string
		Args                    []string `json:",omitempty"`
	}{server.Transport, server.URL, server.Command, server.Args})
	return fmt.Sprintf("%x", sha256.Sum256(body))
}

type easmCaller struct {
	manager   *mcpclient.Manager
	servers   map[string]config.MCPServer
	workspace string
}

func (c *easmCaller) Call(ctx context.Context, tool string, arguments map[string]any) (json.RawMessage, error) {
	server, ok := c.servers[tool]
	if !ok || c.manager == nil {
		return nil, easm.ErrUnavailable
	}
	body, err := c.manager.CallTool(ctx, server, c.workspace, tool, arguments)
	var failure *mcpclient.Error
	switch {
	case err == nil:
		return body, nil
	case errors.As(err, &failure) && (failure.Code == "unknown_tool" || failure.Code == "tool_switched_off"):
		return nil, easm.ErrUnavailable
	case errors.As(err, &failure):
		return nil, &easm.ToolError{Code: failure.Code}
	}
	return nil, err
}
