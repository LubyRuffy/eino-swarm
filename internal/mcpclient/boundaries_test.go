package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/tool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestUnavailableMCPDoesNotHideOtherToolsOrExposePrivateTransportErrors(t *testing.T) {
	good, calls := testServer(t, "streamable_http")
	bad := cloneServer(good)
	bad.ID = "bad"
	bad.Transport = "stdio"
	bad.Command = t.TempDir() + "/private-secret-command"
	disabled := cloneServer(bad)
	disabled.Enabled = false
	m := NewManager()
	defer m.Close()
	set, err := m.Tools(t.Context(), []config.MCPServer{disabled, bad, good}, t.TempDir(), 10000)
	if err != nil || len(set) != 4 {
		t.Fatalf("partial discovery: %d %v", len(set), err)
	}
	info, err := set[0].Info(t.Context())
	if err != nil || !strings.Contains(info.Desc, "query this catalog") {
		t.Fatalf("unavailable metadata %+v %v", info, err)
	}
	out, err := set[0].(tool.InvokableTool).InvokableRun(t.Context(), "{}")
	if err != nil || !strings.Contains(out, "connection_failed") || strings.Contains(out, "private-secret") {
		t.Fatalf("unsafe failure %s %v", out, err)
	}
	if _, err := m.Inspect(t.Context(), bad, t.TempDir()); err == nil || strings.Contains(err.Error(), "private-secret") {
		t.Fatalf("unsafe inspect error %v", err)
	}
	for _, args := range []string{"null", "[]", "{", "{} {}"} {
		out, err := set[3].(tool.InvokableTool).InvokableRun(t.Context(), args)
		if err != nil || !strings.Contains(out, "invalid_arguments") {
			t.Fatalf("invalid arguments %s %s %v", args, out, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid arguments reached external tool")
	}
	m.Close()
	out, err = set[3].(tool.InvokableTool).InvokableRun(t.Context(), `{"name":"lookup","arguments":{}}`)
	if err != nil || !strings.Contains(out, "closed") {
		t.Fatalf("closed call %s %v", out, err)
	}
	if _, err := m.Inspect(t.Context(), good, t.TempDir()); err == nil {
		t.Fatal("closed manager inspected a server")
	}
	if new(Error).Error() == "" {
		t.Fatal("empty error description")
	}
}

func TestConnectionCacheSeparatesCredentialsAndWorkspacesAndRetiresOldSnapshots(t *testing.T) {
	s, _ := testServer(t, "streamable_http")
	m := NewManager()
	defer m.Close()
	workspace := t.TempDir()
	first, err := m.Tools(t.Context(), []config.MCPServer{s}, workspace, 10000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Tools(t.Context(), []config.MCPServer{s}, workspace, 10000); err != nil {
		t.Fatal(err)
	}
	_, _ = first[0].(tool.InvokableTool).InvokableRun(t.Context(), "{}")
	if len(m.entries) != 1 {
		t.Fatalf("same configuration not reused: %d", len(m.entries))
	}
	variant := cloneServer(s)
	variant.Env = map[string]string{"AUTH": "changed"}
	if fingerprint(s) == fingerprint(variant) {
		t.Fatal("environment omitted from connection identity")
	}
	variant = cloneServer(s)
	variant.Headers["X-Credential"] = "changed"
	if fingerprint(s) == fingerprint(variant) {
		t.Fatal("headers omitted from connection identity")
	}
	variantSet, _ := m.Tools(t.Context(), []config.MCPServer{variant}, workspace, 10000)
	_, _ = variantSet[0].(tool.InvokableTool).InvokableRun(t.Context(), "{}")
	otherSet, _ := m.Tools(t.Context(), []config.MCPServer{s}, t.TempDir(), 10000)
	_, _ = otherSet[0].(tool.InvokableTool).InvokableRun(t.Context(), "{}")
	if len(m.entries) != 3 {
		t.Fatalf("credential/workspace collision: %d", len(m.entries))
	}
	// Editing the caller's maps after mounting does not alter the old turn.
	s.Headers["Authorization"] = "mutated-after-mount"
	m.Reload(nil)
	if len(m.entries) != 0 {
		t.Fatal("retired idle sessions survived reload")
	}
	out, err := first[1].(tool.InvokableTool).InvokableRun(t.Context(), `{"name":"lookup","arguments":{}}`)
	if err != nil || !strings.Contains(out, "session_expired") {
		t.Fatalf("old session silently reconnected: %s %v", out, err)
	}
	if len(m.entries) != 0 {
		t.Fatal("retired snapshot call left cached session")
	}
}

func TestIdleSessionCacheStaysBoundedAfterParallelAcquisition(t *testing.T) {
	m := NewManager()
	defer m.Close()
	var entries []*entry
	for i := 0; i < maxIdleSessions+3; i++ {
		e, err := m.acquire(config.MCPServer{ID: "cached"}, fmt.Sprintf("workspace%d", i))
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, e)
	}
	for _, e := range entries {
		m.release(e, false)
	}
	if len(m.entries) > maxIdleSessions {
		t.Fatalf("idle cache leaked %d sessions", len(m.entries))
	}
	e, err := m.acquire(config.MCPServer{ID: "cached"}, "new-workspace")
	if err != nil {
		t.Fatal(err)
	}
	m.release(e, false)
	m.Reload([]config.MCPServer{{ID: "new", Enabled: true}})
	if len(m.entries) != 0 {
		t.Fatal("changed enable flag retained wrong fingerprint")
	}
	m2 := NewManager()
	defer m2.Close()
	e, _ = m2.acquire(config.MCPServer{ID: "ref"}, "workspace")
	second, _ := m2.acquire(config.MCPServer{ID: "ref"}, "workspace")
	m2.release(e, true)
	m2.release(second, false)
	if len(m2.entries) != 0 {
		t.Fatal("later successful release concealed broken connection")
	}
}

func TestMCPCallsPreserveServerFailuresAndHonorCancellationWithoutReplay(t *testing.T) {
	for _, mode := range []string{"isError", "cancel", "timeout", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			started := make(chan struct{}, 1)
			release := make(chan struct{})
			s := mcp.NewServer(&mcp.Implementation{Name: "failures", Version: "test"}, nil)
			s.AddTool(&mcp.Tool{Name: "perform", InputSchema: map[string]any{"type": "object"}}, func(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				started <- struct{}{}
				if mode == "isError" {
					return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "operation refused"}}}, nil
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-release:
					return nil, errors.New("fixture cleanup")
				}
			})
			h := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil))
			defer h.Close()
			defer close(release)
			cfg := config.MCPServer{ID: "failures", Transport: "streamable_http", URL: h.URL, Enabled: true, TimeoutSeconds: 1}
			m := NewManager()
			defer m.Close()
			set, err := m.Tools(t.Context(), []config.MCPServer{cfg}, t.TempDir(), 10000)
			if err != nil || len(set) != 2 {
				t.Fatalf("set %d %v", len(set), err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan string, 1)
			go func() {
				out, err := set[1].(tool.InvokableTool).InvokableRun(ctx, `{"name":"perform","arguments":{}}`)
				if err != nil {
					done <- err.Error()
				} else {
					done <- out
				}
			}()
			<-started
			switch mode {
			case "cancel":
				cancel()
			case "disconnect":
				m.Close()
			}
			select {
			case out := <-done:
				want := map[string]string{"isError": `"isError":true`, "cancel": "cancelled", "timeout": "timeout", "disconnect": "closed"}[mode]
				if !strings.Contains(out, want) {
					t.Fatalf("result %s missing %s", out, want)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("tool call ignored its cancellation")
			}
			select {
			case <-started:
				t.Fatal("a failed MCP call was replayed")
			default:
			}
		})
	}
}

func TestInvalidCatalogAndSchemaFailBeforeMountingExternalTools(t *testing.T) {
	for _, kind := range []string{"duplicate", "discovery", "configuration"} {
		t.Run(kind, func(t *testing.T) {
			h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					ID     any    `json:"id"`
					Method string `json:"method"`
				}
				_ = json.NewDecoder(r.Body).Decode(&request)
				w.Header().Set("Content-Type", "application/json")
				if request.ID == nil {
					w.WriteHeader(http.StatusAccepted)
					return
				}
				var result any = map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "invalid", "version": "1"}}
				if request.Method == "tools/list" {
					if kind == "discovery" {
						_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + fmt.Sprint(request.ID) + `,"error":{"code":-32000,"message":"private-server-secret"}}`))
						return
					}
					item := map[string]any{"name": "x", "inputSchema": map[string]any{"type": "object"}}
					list := []any{item}
					if kind == "duplicate" {
						list = append(list, item)
					}
					result = map[string]any{"tools": list}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
			}))
			defer h.Close()
			cfg := config.MCPServer{ID: "invalid", Transport: "streamable_http", URL: h.URL, Enabled: true, TimeoutSeconds: 1}
			if kind == "configuration" {
				cfg.Transport = "invalid"
			}
			m := NewManager()
			defer m.Close()
			_, err := m.Inspect(t.Context(), cfg, t.TempDir())
			var failure *Error
			if !errors.As(err, &failure) || strings.Contains(err.Error(), "private-server-secret") {
				t.Fatalf("unsafe or missing failure: %v", err)
			}
		})
	}
	for _, schema := range []any{make(chan int), "invalid", nil, map[string]any{"type": "object"}} {
		_, err := toolInfo(ToolDescriptor{InputSchema: schema})
		if err == nil && schema == nil {
			t.Fatal("nil schema mounted")
		}
	}
	if toolName(strings.Repeat("a", 100), strings.Repeat("中文", 100)) == toolName(strings.Repeat("a", 100), strings.Repeat("中文", 101)) {
		t.Fatal("long tool names collided")
	}
}

func TestExternalHTTPTransportRefusesCrossOriginCredentialForwarding(t *testing.T) {
	if !errors.Is(&waiterCancellation{context.Canceled}, context.Canceled) {
		t.Fatal("waiting cancellation lost its context identity")
	}
	req, _ := http.NewRequest(http.MethodPost, "https://other.invalid/messages", nil)
	if _, err := (headerTransport{endpoint: "https://source.invalid/sse", headers: map[string]string{"Authorization": "private"}}).RoundTrip(req); err == nil {
		t.Fatal("cross-origin endpoint accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	m := NewManager()
	defer m.Close()
	if _, err := m.Tools(ctx, []config.MCPServer{{ID: "cancelled", Enabled: true}}, "", 1000); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled discovery %v", err)
	}
}

func TestMCPReferencesCannotJoinAfterClosureOrConfigurationRetirement(t *testing.T) {
	server := config.MCPServer{ID: "retained", Enabled: true}
	m := NewManager()
	e, err := m.acquire(server, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	m.Reload(nil)
	if err := m.retain(e); err == nil || !strings.Contains(err.Error(), "session_expired") {
		t.Fatalf("retired session admitted a new call: %v", err)
	}
	m.release(e, false)
	m.Close()
	if err := m.retain(e); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("closed manager admitted a new call: %v", err)
	}
}
