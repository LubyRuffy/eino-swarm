package mcpclient

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ProxyNames are the two model-facing tools one configured server contributes.
func ProxyNames(serverID string) (list, call string) {
	return toolName(serverID, "list_tools"), toolName(serverID, "call_tool")
}

// ProxyOffered reports whether a model-facing name belongs to an enabled server.
// Names this process did not mint for a configured server stay offered.
func ProxyOffered(servers []config.MCPServer, name string) bool {
	matched := false
	for _, server := range servers {
		list, call := ProxyNames(server.ID)
		if name != list && name != call {
			continue
		}
		matched = true
		if server.Enabled {
			return true
		}
	}
	return !matched
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
		if b.Len() >= 18 {
			break
		}
	}
	return b.String()
}

func toolName(server, name string) string {
	hash := sha256.Sum256([]byte(server + "\x00" + name))
	return fmt.Sprintf("mcp_%s_%s_%x", slug(server), slug(name), hash[:6])
}

// DirectToolName is the model-facing function for one remote tool. Its hash
// input is distinct from list_tools and call_tool, so a remote tool with one
// of those names does not collide with the catalog proxies.
func DirectToolName(server, remote string) string {
	hash := sha256.Sum256([]byte(server + "\x00direct\x00" + remote))
	return fmt.Sprintf("mcp_%s_%s_%x", slug(server), slug(remote), hash[:6])
}

func toolInfo(d ToolDescriptor) (*schema.ToolInfo, error) {
	raw, err := json.Marshal(d.InputSchema)
	if err != nil {
		return nil, err
	}
	var params jsonschema.Schema
	if err = json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	if params.Type != "object" && (len(params.TypeEnhanced) != 1 || params.TypeEnhanced[0] != "object") {
		return nil, fmt.Errorf("MCP tool schema must be an object")
	}
	return &schema.ToolInfo{Name: d.Name, Desc: d.Description, ParamsOneOf: schema.NewParamsOneOfByJSONSchema(&params)}, nil
}

// A toolset binds the first session it uses. A later call cannot silently
// replace server-side state after eviction, configuration change or failure.
type sessionHolder struct {
	mu        sync.Mutex
	manager   *Manager
	server    config.MCPServer
	workspace string
	entry     *entry
}

func (h *sessionHolder) acquire() (*entry, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.entry != nil {
		return h.entry, h.manager.retain(h.entry)
	}
	e, err := h.manager.acquire(h.server, h.workspace)
	if err == nil {
		h.entry = e
	}
	return e, err
}

type externalTool struct {
	holder *sessionHolder
	list   bool
	index  string
}

const mcpRouteGuidance = "Before a local command or a direct API call, query this catalog for the task. The server title is not evidence that no tool matches. Prefer a matching MCP tool unless the user specifies another route. A past failure alone does not justify bypassing this server. "

func (t *externalTool) Info(context.Context) (*schema.ToolInfo, error) {
	name := "call_tool"
	description := "Call one tool on this configured MCP server. First use its list_tools proxy to inspect exact remote names and JSON input schemas. Copy a returned remote name into name and supply its arguments object. A session_expired result requires a new turn; calls are never automatically replayed."
	input := map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string", "description": "Exact remote name returned by this server's list_tools proxy."}, "arguments": map[string]any{"type": "object", "description": "Arguments conforming to the remote tool's input_schema."}}, "required": []string{"name", "arguments"}}
	if t.list {
		name = "list_tools"
		description = fmt.Sprintf("Query this configured MCP server's tool catalog without executing a remote tool. A query returns matching names and descriptions. Pass name to retrieve one complete JSON input schema before using this server's call_tool proxy. Use offset/limit to page (default limit %d). If a result is truncated, narrow the query or one name; an individual schema that still exceeds the result budget requires increasing tools.result_max_chars in Settings.", config.DefaultMCPToolPageSize)
		input = map[string]any{"type": "object", "properties": map[string]any{
			"query":  map[string]any{"type": "string", "description": "Optional case-insensitive match against remote names and descriptions. Omit it to list the enabled catalog."},
			"name":   map[string]any{"type": "string", "description": "Optional exact remote tool name; returns only that tool's complete schema."},
			"offset": map[string]any{"type": "integer", "minimum": 0, "description": "First tool index in the filtered catalogue."},
			"limit":  map[string]any{"type": "integer", "minimum": 1, "description": fmt.Sprintf("Maximum tools to return; default %d.", config.DefaultMCPToolPageSize)},
		}}
	}
	label := t.holder.server.Name
	if label == "" {
		label = t.holder.server.ID
	}
	description = fmt.Sprintf("MCP server %q (id %q). %s%s", label, t.holder.server.ID, mcpRouteGuidance, description)
	if t.index != "" {
		description += " Enabled tools: " + t.index + "."
	}
	return toolInfo(ToolDescriptor{Name: toolName(t.holder.server.ID, name), Description: description, InputSchema: input})
}

func (t *externalTool) failure(code string) string {
	return jsonResult(map[string]any{"error": &Error{Code: code, ServerID: t.holder.server.ID}})
}

func (t *externalTool) switchedOff(name string) bool {
	if t == nil || t.holder == nil || t.holder.manager == nil {
		return false
	}
	return t.holder.manager.remoteDisabled(t.holder.server.ID, name)
}

func switchedOffRemoteResult(name string) string {
	encoded, _ := json.Marshal(name)
	return `{"ok":false,"code":"tool_switched_off","tool":` + string(encoded) + `}`
}

func withoutSwitchedOffTools(tools []ToolDescriptor, off func(string) bool) []ToolDescriptor {
	selected := make([]ToolDescriptor, 0, len(tools))
	for _, remote := range tools {
		if off(remote.Name) || off(remote.OriginalName) {
			continue
		}
		selected = append(selected, remote)
	}
	return selected
}

func (t *externalTool) InvokableRun(ctx context.Context, args string, _ ...tool.Option) (string, error) {
	if ctx.Err() != nil {
		return t.failure("cancelled"), nil
	}
	var input map[string]json.RawMessage
	if json.Unmarshal([]byte(args), &input) != nil || input == nil {
		return t.failure("invalid_arguments"), nil
	}
	var request struct {
		Name      string                     `json:"name"`
		Arguments map[string]json.RawMessage `json:"arguments"`
	}
	var listing struct {
		Query  string `json:"query"`
		Name   string `json:"name"`
		Offset int    `json:"offset"`
		Limit  *int   `json:"limit"`
	}
	if t.list && (json.Unmarshal([]byte(args), &listing) != nil || listing.Offset < 0 || listing.Limit != nil && *listing.Limit < 1) {
		return t.failure("invalid_arguments"), nil
	}
	if !t.list && (json.Unmarshal([]byte(args), &request) != nil || request.Name == "" || request.Arguments == nil) {
		return t.failure("invalid_arguments"), nil
	}
	if !t.list && t.switchedOff(request.Name) {
		return switchedOffRemoteResult(request.Name), nil
	}
	started := time.Now()
	e, err := t.holder.acquire()
	if err != nil {
		code := err.(*Error).Code
		t.logCall(ctx, started, "", "session", code, "")
		return t.failure(code), nil
	}
	broken := false
	defer func() { t.holder.manager.release(e, broken) }()
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
		t.logCall(ctx, started, "", "connect", code, "")
		return t.failure(code), nil
	}
	if t.list {
		selected := withoutSwitchedOffTools(catalog.Tools, t.switchedOff)
		if listing.Name != "" {
			matched := []ToolDescriptor{}
			for _, d := range selected {
				if d.Name == listing.Name || d.OriginalName == listing.Name {
					matched = append(matched, d)
				}
			}
			selected = matched
		} else {
			selected = filterToolQuery(selected, listing.Query)
		}
		total := len(selected)
		offset := min(listing.Offset, total)
		limit := config.DefaultMCPToolPageSize
		if listing.Limit != nil {
			limit = *listing.Limit
		}
		end := offset + min(limit, total-offset)
		page := append([]ToolDescriptor(nil), selected[offset:end]...)
		if listing.Name == "" {
			for i := range page {
				page[i].InputSchema = nil
			}
		}
		t.holder.manager.RememberCatalog(t.holder.server, catalog)
		return jsonResult(struct {
			Catalog
			Total  int  `json:"total"`
			Offset int  `json:"offset"`
			More   bool `json:"more"`
		}{Catalog{ServerID: catalog.ServerID, Tools: page}, total, offset, end < total}), nil
	}
	found := false
	for _, remote := range catalog.Tools {
		if remote.OriginalName == request.Name {
			found = true
			break
		}
	}
	if !found {
		return t.failure("unknown_tool"), nil
	}
	started = time.Now()
	callCtx, cancel := context.WithTimeout(ctx, t.holder.server.Timeout())
	defer cancel()
	stop := context.AfterFunc(e.ctx, cancel)
	defer stop()
	result, err, digest := e.callTool(callCtx, session, &mcp.CallToolParams{Name: request.Name, Arguments: request.Arguments}, t.holder.manager.log)
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
		t.logCall(ctx, started, request.Name, "call", code, digest)
		return t.failure(code), nil
	}
	code := "ok"
	if result != nil && result.IsError {
		code = "tool_error"
	}
	t.logCall(ctx, started, request.Name, "call", code, digest)
	return jsonResult(result), nil
}

func (t *externalTool) logCall(ctx context.Context, started time.Time, name, phase, code, digest string) {
	if t.list {
		return
	}
	level := slog.LevelInfo
	if code != "ok" {
		level = slog.LevelWarn
	}
	server := t.holder.server
	attrs := []slog.Attr{
		slog.String("server_id", server.ID),
		slog.String("transport", server.Transport),
		slog.String("tool_call_id", compose.GetToolCallID(ctx)),
		slog.String("phase", phase),
		slog.String("code", code),
		slog.Int64("elapsed_ms", time.Since(started).Milliseconds()),
		slog.Int64("timeout_ms", server.Timeout().Milliseconds()),
	}
	if name != "" {
		// Only catalog-confirmed names reach this point. Never log arguments,
		// results, transport errors, or untrusted stderr.
		attrs = append(attrs, slog.String("tool", progressMessage(name)))
	}
	if digest != "" {
		attrs = append(attrs, slog.String("progress_token_sha256", digest))
	}
	t.holder.manager.log.LogAttrs(ctx, level, "MCP tool call", attrs...)
}
