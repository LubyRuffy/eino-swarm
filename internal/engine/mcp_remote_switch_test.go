package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/tool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The proxies mounted for a turn read the engine's current config. Turning one
// remote tool off must not wait for the next turn or retire the server session.
func TestMountedMCPCallFollowsARemoteToolSwitchWithoutRebuildingTheTurn(t *testing.T) {
	calls := new(atomic.Int32)
	fixture := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "test"}, nil)
	fixture.AddTool(&mcp.Tool{
		Name: "lookup", Description: "Look up a record",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "found"}}}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return fixture }, nil)
	remote := httptest.NewServer(handler)
	t.Cleanup(remote.Close)

	cfg := config.Default()
	cfg.MCP.Servers = []config.MCPServer{{
		ID: "desk", Name: "Desk", Transport: "streamable_http", URL: remote.URL, Enabled: true, TimeoutSeconds: 2,
	}}
	e := New(cfg, nil, nil, nil)
	t.Cleanup(e.mcp.Close)
	set, err := e.mcp.Tools(t.Context(), cfg.Snapshot().MCP.Servers, t.TempDir(), 10000)
	if err != nil || len(set) != 2 {
		t.Fatalf("mount %d %v", len(set), err)
	}
	list := set[0].(tool.InvokableTool)
	call := set[1].(tool.InvokableTool)
	listed, err := list.InvokableRun(t.Context(), `{}`)
	if err != nil || !strings.Contains(listed, `"name":"lookup"`) {
		t.Fatalf("list: %s %v", listed, err)
	}
	out, err := call.InvokableRun(t.Context(), `{"name":"lookup","arguments":{}}`)
	if err != nil || calls.Load() != 1 || strings.Contains(out, `"error"`) {
		t.Fatalf("call: %s %v count=%d", out, err, calls.Load())
	}

	cfg.MCP.Servers[0].DisabledTools = []string{"lookup"}
	listed, err = list.InvokableRun(t.Context(), `{}`)
	if err != nil || strings.Contains(listed, `"name":"lookup"`) {
		t.Fatalf("switched-off tool stayed visible: %s %v", listed, err)
	}
	out, err = call.InvokableRun(t.Context(), `{"name":"lookup","arguments":{}}`)
	if err != nil || calls.Load() != 1 || !strings.Contains(out, `"code":"tool_switched_off"`) || !strings.Contains(out, `"tool":"lookup"`) {
		t.Fatalf("refused call: %s %v count=%d", out, err, calls.Load())
	}

	cfg.MCP.Servers[0].DisabledTools = nil
	out, err = call.InvokableRun(t.Context(), `{"name":"lookup","arguments":{}}`)
	if err != nil || calls.Load() != 2 || strings.Contains(out, "tool_switched_off") {
		t.Fatalf("restored call: %s %v count=%d", out, err, calls.Load())
	}
}
