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

func toolName(server, name string) string {
	slug := func(s string) string {
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
	hash := sha256.Sum256([]byte(server + "\x00" + name))
	return fmt.Sprintf("mcp_%s_%s_%x", slug(server), slug(name), hash[:6])
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
}

const mcpRouteGuidance = "When this server may cover the request, inspect its current tool catalog and prefer a matching MCP tool unless the user specifies another route. Before reusing a saved shell or direct API fallback, verify that the current catalog has no match or the current attempt failed, and that the alternate route is authorized. A past failure alone does not justify bypassing this server. "

func (t *externalTool) Info(context.Context) (*schema.ToolInfo, error) {
	name := "call_tool"
	description := "Call one tool on this configured MCP server. First use its list_tools proxy to inspect exact remote names and JSON input schemas. Copy a returned remote name into name and supply its arguments object. A session_expired result requires a new turn; calls are never automatically replayed."
	input := map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string", "description": "Exact remote name returned by this server's list_tools proxy."}, "arguments": map[string]any{"type": "object", "description": "Arguments conforming to the remote tool's input_schema."}}, "required": []string{"name", "arguments"}}
	if t.list {
		name = "list_tools"
		description = fmt.Sprintf("Connect to this configured MCP server and list tool names, descriptions and JSON input schemas without executing a remote tool. Inspect this result before using this server's call_tool proxy. Use offset/limit to page (default limit %d), or name to retrieve one exact tool schema. If a result is truncated, narrow to one name; an individual schema that still exceeds the result budget requires increasing tools.result_max_chars in Settings.", config.DefaultMCPToolPageSize)
		input = map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string", "description": "Optional exact remote tool name; returns only that tool's complete schema."}, "offset": map[string]any{"type": "integer", "minimum": 0, "description": "First tool index in the stable discovered catalogue."}, "limit": map[string]any{"type": "integer", "minimum": 1, "description": fmt.Sprintf("Maximum tools to return; default %d.", config.DefaultMCPToolPageSize)}}}
	}
	label := t.holder.server.Name
	if label == "" {
		label = t.holder.server.ID
	}
	description = fmt.Sprintf("MCP server %q (id %q). %s%s", label, t.holder.server.ID, mcpRouteGuidance, description)
	return toolInfo(ToolDescriptor{Name: toolName(t.holder.server.ID, name), Description: description, InputSchema: input})
}

func (t *externalTool) failure(code string) string {
	return jsonResult(map[string]any{"error": &Error{Code: code, ServerID: t.holder.server.ID}})
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
		selected := catalog.Tools
		if listing.Name != "" {
			selected = []ToolDescriptor{}
			for _, d := range catalog.Tools {
				if d.Name == listing.Name {
					selected = append(selected, d)
				}
			}
		}
		total := len(selected)
		offset := min(listing.Offset, total)
		limit := config.DefaultMCPToolPageSize
		if listing.Limit != nil {
			limit = *listing.Limit
		}
		end := offset + min(limit, total-offset)
		return jsonResult(struct {
			Catalog
			Total  int  `json:"total"`
			Offset int  `json:"offset"`
			More   bool `json:"more"`
		}{Catalog{ServerID: catalog.ServerID, Tools: selected[offset:end]}, total, offset, end < total}), nil
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
