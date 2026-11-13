package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type directBinding struct {
	ServerID string
	Remote   string
}

// PrepareDirect selects the remote tools that can be mounted as functions.
// only, when non-nil, keeps just those remote names. skipDisabled omits names
// in the server's disabled_tools. A schema that cannot be a function is
// skipped and does not reject the rest of the server.
func PrepareDirect(server config.MCPServer, catalog Catalog, skipDisabled bool, only map[string]struct{}) (mount []ToolDescriptor, skipped []string) {
	disabled := map[string]struct{}{}
	if skipDisabled {
		for _, name := range server.DisabledTools {
			disabled[name] = struct{}{}
		}
	}
	skipped = append([]string(nil), catalog.Skipped...)
	for _, remote := range catalog.Tools {
		name := remote.OriginalName
		if name == "" {
			name = remote.Name
		}
		if only != nil {
			if _, want := only[name]; !want {
				continue
			}
		}
		if _, off := disabled[name]; off {
			continue
		}
		if _, off := disabled[remote.Name]; off {
			continue
		}
		if _, err := toolInfo(remote); err != nil {
			skipped = append(skipped, name)
			continue
		}
		mount = append(mount, remote)
	}
	return mount, skipped
}

// DirectTools registers one eino function per prepared remote tool. The tools
// share one session holder and connect on the first call.
func (m *Manager) DirectTools(server config.MCPServer, workspace string, maxChars int, prepared []ToolDescriptor) ([]tool.BaseTool, []string) {
	if m == nil || len(prepared) == 0 {
		return nil, nil
	}
	server = cloneServer(server)
	holder := &sessionHolder{manager: m, server: server, workspace: workspace}
	out := make([]tool.BaseTool, 0, len(prepared))
	names := make([]string, 0, len(prepared))
	for _, remote := range prepared {
		remoteName := remote.OriginalName
		if remoteName == "" {
			remoteName = remote.Name
		}
		modelName := DirectToolName(server.ID, remoteName)
		item := &directTool{holder: holder, remote: remote, modelName: modelName}
		m.rememberDirect(modelName, directBinding{ServerID: server.ID, Remote: remoteName})
		out = append(out, tools.WithResultLimit(item, maxChars))
		names = append(names, modelName)
	}
	return out, names
}

// IndexedProxies is the progressive pair, with the enabled-tool index baked
// into both descriptions. An empty index still leaves the query proxy.
func (m *Manager) IndexedProxies(server config.MCPServer, workspace string, maxChars int, index string) ([]tool.BaseTool, []string, error) {
	if m == nil {
		return nil, nil, nil
	}
	server = cloneServer(server)
	holder := &sessionHolder{manager: m, server: server, workspace: workspace}
	list := tools.WithResultLimit(&externalTool{holder: holder, list: true, index: index}, maxChars)
	call := tools.WithResultLimit(&externalTool{holder: holder, index: index}, maxChars)
	out := []tool.BaseTool{list, call}
	names := make([]string, len(out))
	for i, item := range out {
		info, err := item.Info(context.Background())
		if err != nil {
			return nil, nil, err
		}
		names[i] = info.Name
	}
	return out, names, nil
}

func (m *Manager) rememberDirect(name string, binding directBinding) {
	if m == nil || name == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.direct == nil {
		m.direct = map[string]directBinding{}
	}
	m.direct[name] = binding
}

func (m *Manager) lookupDirect(name string) (directBinding, bool) {
	if m == nil {
		return directBinding{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	binding, ok := m.direct[name]
	return binding, ok
}

// ToolOffered reports whether a model-facing MCP name may be sent on the next
// request. A direct tool this process mounted follows the server's enabled
// flag and disabled_tools. Other names keep the proxy rule.
func (m *Manager) ToolOffered(servers []config.MCPServer, name string) bool {
	if binding, ok := m.lookupDirect(name); ok {
		for _, server := range servers {
			if server.ID != binding.ServerID {
				continue
			}
			if !server.Enabled {
				return false
			}
			for _, off := range server.DisabledTools {
				if off == binding.Remote {
					return false
				}
			}
			return true
		}
		return false
	}
	return ProxyOffered(servers, name)
}

type directTool struct {
	holder    *sessionHolder
	remote    ToolDescriptor
	modelName string
}

func (t *directTool) Info(context.Context) (*schema.ToolInfo, error) {
	label := t.holder.server.Name
	if label == "" {
		label = t.holder.server.ID
	}
	remoteName := t.remote.OriginalName
	if remoteName == "" {
		remoteName = t.remote.Name
	}
	described := t.remote
	described.Name = t.modelName
	described.Description = "MCP server " + quoteLabel(label) + " (id " + quoteLabel(t.holder.server.ID) + "), remote tool " + quoteLabel(remoteName) + ". " + t.remote.Description
	return toolInfo(described)
}

func quoteLabel(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

func (t *directTool) failure(code string) string {
	return jsonResult(map[string]any{"error": &Error{Code: code, ServerID: t.holder.server.ID}})
}

func (t *directTool) InvokableRun(ctx context.Context, args string, _ ...tool.Option) (string, error) {
	if ctx.Err() != nil {
		return t.failure("cancelled"), nil
	}
	remoteName := t.remote.OriginalName
	if remoteName == "" {
		remoteName = t.remote.Name
	}
	if t.holder.manager != nil && t.holder.manager.remoteDisabled(t.holder.server.ID, remoteName) {
		return switchedOffRemoteResult(remoteName), nil
	}
	var arguments map[string]json.RawMessage
	if args == "" || args == "null" {
		arguments = map[string]json.RawMessage{}
	} else if json.Unmarshal([]byte(args), &arguments) != nil || arguments == nil {
		return t.failure("invalid_arguments"), nil
	}
	return t.holder.runCall(ctx, remoteName, arguments), nil
}

func (h *sessionHolder) runCall(ctx context.Context, name string, arguments map[string]json.RawMessage) string {
	started := time.Now()
	fail := func(code string) string {
		return jsonResult(map[string]any{"error": &Error{Code: code, ServerID: h.server.ID}})
	}
	e, err := h.acquire()
	if err != nil {
		code := err.(*Error).Code
		h.logCall(ctx, started, "", "session", code, "")
		return fail(code)
	}
	broken := false
	defer func() { h.manager.release(e, broken) }()
	session, catalog, err := e.connect(ctx)
	if err != nil {
		var waiting *waiterCancellation
		broken = !errors.As(err, &waiting)
		code := "connection_failed"
		if ctx.Err() != nil {
			code = "cancelled"
		} else if failure, ok := err.(*Error); ok {
			code = failure.Code
		}
		h.logCall(ctx, started, "", "connect", code, "")
		return fail(code)
	}
	found := false
	for _, remote := range catalog.Tools {
		if remote.OriginalName == name || remote.Name == name {
			found = true
			break
		}
	}
	if !found {
		return fail("unknown_tool")
	}
	started = time.Now()
	callCtx, cancel := context.WithTimeout(ctx, h.server.Timeout())
	defer cancel()
	stop := context.AfterFunc(e.ctx, cancel)
	defer stop()
	result, err, digest := e.callTool(callCtx, session, &mcp.CallToolParams{Name: name, Arguments: arguments}, h.manager.log)
	if err != nil {
		broken = true
		code := "call_failed"
		if ctx.Err() != nil {
			code = "cancelled"
		} else if e.ctx.Err() != nil {
			code = "closed"
		} else if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			code = "timeout"
		}
		h.logCall(ctx, started, name, "call", code, digest)
		return fail(code)
	}
	code := "ok"
	if result != nil && result.IsError {
		code = "tool_error"
	}
	h.logCall(ctx, started, name, "call", code, digest)
	return jsonResult(result)
}

func (h *sessionHolder) logCall(ctx context.Context, started time.Time, name, phase, code, digest string) {
	if h == nil || h.manager == nil || h.manager.log == nil {
		return
	}
	level := slog.LevelInfo
	if code != "ok" {
		level = slog.LevelWarn
	}
	attrs := []slog.Attr{
		slog.String("server_id", h.server.ID),
		slog.String("transport", h.server.Transport),
		slog.String("tool_call_id", compose.GetToolCallID(ctx)),
		slog.String("phase", phase),
		slog.String("code", code),
		slog.Int64("elapsed_ms", time.Since(started).Milliseconds()),
		slog.Int64("timeout_ms", h.server.Timeout().Milliseconds()),
	}
	if name != "" {
		attrs = append(attrs, slog.String("tool", progressMessage(name)))
	}
	if digest != "" {
		attrs = append(attrs, slog.String("progress_token_sha256", digest))
	}
	h.manager.log.LogAttrs(ctx, level, "MCP tool call", attrs...)
}
