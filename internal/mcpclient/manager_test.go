package mcpclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/tool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testServer(t *testing.T, transport string) (config.MCPServer, *atomic.Int32) {
	t.Helper()
	calls := new(atomic.Int32)
	s := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "test"}, nil)
	s.AddTool(&mcp.Tool{Name: "lookup", Description: "Look up a record", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"key": map[string]any{"type": "string"}}}}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(req.Params.Arguments)}}, StructuredContent: map[string]any{"found": true}}, nil
	})
	var handler http.Handler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil)
	if transport == "sse" {
		handler = mcp.NewSSEHandler(func(*http.Request) *mcp.Server { return s }, nil)
	}
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-fixture" {
			t.Error("missing MCP authorization")
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(h.Close)
	return config.MCPServer{ID: "records", Name: "Records", Transport: transport, URL: h.URL, Headers: map[string]string{"Authorization": "Bearer private-fixture"}, Enabled: true, TimeoutSeconds: 2}, calls
}

func TestConfiguredMCPToolsExecuteThroughRealTransports(t *testing.T) {
	for _, transport := range []string{"streamable_http", "sse"} {
		t.Run(transport, func(t *testing.T) {
			s, calls := testServer(t, transport)
			m := NewManager()
			t.Cleanup(m.Close)
			catalog, err := m.Inspect(t.Context(), s, t.TempDir())
			if err != nil || len(catalog.Tools) != 1 {
				t.Fatalf("inspect: %+v %v", catalog, err)
			}
			tools, err := m.Tools(t.Context(), []config.MCPServer{s}, t.TempDir(), 10000)
			if err != nil || len(tools) != 2 {
				t.Fatalf("tools: %d %v", len(tools), err)
			}
			info, err := tools[0].Info(t.Context())
			if err != nil || !strings.HasPrefix(info.Name, "mcp_") || len(info.Name) > 64 {
				t.Fatalf("info: %+v %v", info, err)
			}
			listed, err := tools[0].(tool.InvokableTool).InvokableRun(t.Context(), `{}`)
			if err != nil || !strings.Contains(listed, `"name":"lookup"`) {
				t.Fatalf("list: %s %v", listed, err)
			}
			out, err := tools[1].(tool.InvokableTool).InvokableRun(t.Context(), `{"name":"lookup","arguments":{"key":"alpha"}}`)
			if err != nil || !json.Valid([]byte(out)) || !strings.Contains(out, `"found":true`) || calls.Load() != 1 {
				t.Fatalf("call: %s %v count=%d", out, err, calls.Load())
			}
		})
	}
}
