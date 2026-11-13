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

func TestProxyOfferedFollowsTheServerSwitch(t *testing.T) {
	list, call := ProxyNames("desk")
	otherList, _ := ProxyNames("other")
	servers := []config.MCPServer{{ID: "desk", Enabled: true}, {ID: "other", Enabled: false}}
	if !ProxyOffered(servers, list) || !ProxyOffered(servers, call) || ProxyOffered(servers, otherList) {
		t.Fatalf("enabled desk=%t/%t disabled other=%t", ProxyOffered(servers, list), ProxyOffered(servers, call), ProxyOffered(servers, otherList))
	}
	if !ProxyOffered(servers, "ask_user") || !ProxyOffered(nil, list) {
		t.Fatal("names this process did not mint for a configured server stay offered")
	}
	servers[0].Enabled = false
	if ProxyOffered(servers, list) || ProxyOffered(servers, call) {
		t.Fatal("a disabled server's proxies stay in the request")
	}
}

func TestToolSwitchesDoNotChangeTheMCPSessionFingerprint(t *testing.T) {
	base := config.MCPServer{ID: "desk", Transport: "stdio", Command: "tool", Enabled: true}
	switched := base
	switched.DisabledTools = []string{"lookup"}
	if fingerprint(base) != fingerprint(switched) {
		t.Fatal("a remote tool switch would retire the server session")
	}
	switched.Command = "other"
	if fingerprint(base) == fingerprint(switched) {
		t.Fatal("a connection change must still retire the session")
	}
}

func TestSwitchedOffRemoteToolIsOmittedAndNotExecuted(t *testing.T) {
	(*Manager)(nil).UseRemoteSwitches(nil)
	if (*Manager)(nil).remoteDisabled("desk", "lookup") || (*externalTool)(nil).switchedOff("lookup") {
		t.Fatal("a missing manager hid a tool")
	}
	kept := withoutSwitchedOffTools([]ToolDescriptor{
		{Name: "visible", OriginalName: "hidden"},
		{Name: "kept", OriginalName: "kept"},
	}, func(name string) bool { return name == "hidden" })
	if len(kept) != 1 || kept[0].Name != "kept" {
		t.Fatalf("catalog filter: %+v", kept)
	}
	if got := switchedOffRemoteResult(`say "hi"`); got != `{"ok":false,"code":"tool_switched_off","tool":"say \"hi\""}` {
		t.Fatalf("refusal: %s", got)
	}

	s, calls := testServer(t, "streamable_http")
	m := NewManager()
	t.Cleanup(m.Close)
	current := []config.MCPServer{s}
	m.UseRemoteSwitches(func() []config.MCPServer { return current })
	if m.remoteDisabled("desk", "") || m.remoteDisabled("missing", "lookup") || m.remoteDisabled(s.ID, "lookup") {
		t.Fatal("an unnamed, unknown or still-enabled tool was treated as off")
	}
	set, err := m.Tools(t.Context(), []config.MCPServer{s}, t.TempDir(), 10000)
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

	off := s
	off.DisabledTools = []string{"lookup"}
	current = []config.MCPServer{off}
	m.Reload(current)
	listed, err = list.InvokableRun(t.Context(), `{}`)
	if err != nil || strings.Contains(listed, `"name":"lookup"`) {
		t.Fatalf("switched-off tool stayed in the catalog: %s %v", listed, err)
	}
	listed, err = list.InvokableRun(t.Context(), `{"name":"lookup"}`)
	if err != nil || strings.Contains(listed, `"name":"lookup"`) {
		t.Fatalf("named list still returned the switched-off tool: %s %v", listed, err)
	}
	out, err = call.InvokableRun(t.Context(), `{"name":"lookup","arguments":{"key":"alpha"}}`)
	if err != nil || calls.Load() != 1 || strings.Contains(out, "session_expired") || !strings.Contains(out, `"code":"tool_switched_off"`) || !strings.Contains(out, `"tool":"lookup"`) {
		t.Fatalf("refused call: %s %v count=%d", out, err, calls.Load())
	}

	current = []config.MCPServer{s}
	m.Reload(current)
	out, err = call.InvokableRun(t.Context(), `{"name":"lookup","arguments":{}}`)
	if err != nil || calls.Load() != 2 || strings.Contains(out, "tool_switched_off") {
		t.Fatalf("turning the tool back on did not restore the same session: %s %v count=%d", out, err, calls.Load())
	}
}
