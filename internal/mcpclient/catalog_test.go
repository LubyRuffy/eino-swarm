package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/tool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCatalogCacheOmitsSecretsAndSkipsOneBadSchema(t *testing.T) {
	secret := "catalog-secret-value"
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
		result := map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "mixed", "version": "1"}}
		if request.Method == "tools/list" {
			result = map[string]any{"tools": []any{
				map[string]any{"name": "usable", "description": "usable tool", "inputSchema": map[string]any{"type": "object"}},
				map[string]any{"name": "broken", "description": "not a function", "inputSchema": map[string]any{"type": "array"}},
			}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	defer h.Close()
	server := config.MCPServer{
		ID: "mixed", Name: "Mixed", Transport: "streamable_http", URL: h.URL, Enabled: true,
		TimeoutSeconds: 2, Env: map[string]string{"TOKEN": secret}, Headers: map[string]string{"X-Token": secret},
	}
	dir := t.TempDir()
	m := NewManager()
	defer m.Close()
	m.SetCatalogDir(dir)
	catalog, err := m.Inspect(t.Context(), server, dir)
	if err != nil || len(catalog.Tools) != 1 || catalog.Tools[0].Name != "usable" || len(catalog.Skipped) != 1 || catalog.Skipped[0] != "broken" {
		t.Fatalf("catalog %+v %v", catalog, err)
	}
	if err := SaveCatalog(dir, server, catalog); err != nil {
		t.Fatal(err)
	}
	path := catalogPath(dir, fingerprint(server))
	info, err := os.Stat(path)
	body, readErr := os.ReadFile(path)
	if err != nil || readErr != nil || info.Mode().Perm() != 0o600 || strings.Contains(string(body), secret) || strings.Contains(string(body), h.URL) {
		t.Fatalf("cache mode=%v err=%v read=%v body=%s", info.Mode().Perm(), err, readErr, body)
	}
	loaded, ok := LoadCatalog(dir, server)
	if !ok || len(loaded.Tools) != 1 || loaded.Tools[0].Name != "usable" {
		t.Fatalf("reload %+v %v", loaded, ok)
	}
	mount, skipped := PrepareDirect(server, loaded, true, nil)
	if len(mount) != 1 || len(skipped) != 1 {
		t.Fatalf("prepare %d skipped %v", len(mount), skipped)
	}
	if DirectToolName(server.ID, "list_tools") == toolName(server.ID, "list_tools") {
		t.Fatal("a remote list_tools collided with the proxy")
	}
	tools, names := m.DirectTools(server, dir, 4000, mount)
	if len(tools) != 1 || !strings.Contains(names[0], "usable") {
		t.Fatalf("direct tools %v", names)
	}
	infoTool, err := tools[0].Info(t.Context())
	if err != nil || !strings.Contains(infoTool.Desc, "usable") || !strings.Contains(infoTool.Desc, "Mixed") {
		t.Fatalf("direct description %v %v", infoTool, err)
	}
}

func TestListQueryReturnsNamesUntilAnExactSchemaIsRequested(t *testing.T) {
	s := mcp.NewServer(&mcp.Implementation{Name: "indexed", Version: "test"}, nil)
	s.AddTool(&mcp.Tool{Name: "whois_query", Description: "Look up a domain registration", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"domain": map[string]any{"type": "string"}}}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "registered"}}}, nil
	})
	s.AddTool(&mcp.Tool{Name: "asset_count", Description: "Count matching assets", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})
	h := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil))
	defer h.Close()
	server := config.MCPServer{ID: "indexed", Name: "Indexed", Transport: "streamable_http", URL: h.URL, Enabled: true, TimeoutSeconds: 2}
	m := NewManager()
	defer m.Close()
	set, _, err := m.IndexedProxies(server, t.TempDir(), 8000, ToolIndex([]ToolDescriptor{{OriginalName: "whois_query", Description: "Look up a domain registration"}, {OriginalName: "asset_count", Description: "Count matching assets"}}))
	if err != nil || len(set) != 2 {
		t.Fatal(err)
	}
	listed, err := set[0].Info(t.Context())
	if err != nil || !strings.Contains(listed.Desc, "whois_query") || !strings.Contains(listed.Desc, "query this catalog") || !strings.Contains(listed.Desc, "server title is not evidence") {
		t.Fatalf("index missing: %v %v", listed, err)
	}
	out, err := set[0].(tool.InvokableTool).InvokableRun(t.Context(), `{"query":"domain"}`)
	if err != nil || strings.Contains(out, "input_schema") || !strings.Contains(out, "whois_query") || strings.Contains(out, "asset_count") {
		t.Fatalf("query: %s %v", out, err)
	}
	out, err = set[0].(tool.InvokableTool).InvokableRun(t.Context(), `{"name":"whois_query"}`)
	if err != nil || !strings.Contains(out, "input_schema") || !strings.Contains(out, "domain") {
		t.Fatalf("exact schema: %s %v", out, err)
	}
	calls := 0
	// The exact schema request already connected. A switched-off name must not call.
	m.UseRemoteSwitches(func() []config.MCPServer {
		next := server
		next.DisabledTools = []string{"whois_query"}
		return []config.MCPServer{next}
	})
	direct, names := m.DirectTools(server, t.TempDir(), 8000, []ToolDescriptor{{Name: "whois_query", OriginalName: "whois_query", Description: "Look up a domain registration", InputSchema: map[string]any{"type": "object"}}})
	if len(direct) != 1 || names[0] == "" {
		t.Fatal("direct tool missing")
	}
	refused, err := direct[0].(tool.InvokableTool).InvokableRun(t.Context(), `{"domain":"example.test"}`)
	if err != nil || !strings.Contains(refused, `"code":"tool_switched_off"`) || calls != 0 {
		t.Fatalf("switched off direct call ran: %s %v", refused, err)
	}
	if m.ToolOffered([]config.MCPServer{{ID: "indexed", Enabled: true, DisabledTools: []string{"whois_query"}}}, names[0]) {
		t.Fatal("a switched-off direct tool stayed offered")
	}
	if !m.ToolOffered([]config.MCPServer{{ID: "indexed", Enabled: true}}, names[0]) {
		t.Fatal("turning the remote tool back on did not offer the mounted name")
	}
	long := make([]ToolDescriptor, 40)
	for i := range long {
		long[i] = ToolDescriptor{OriginalName: fmt.Sprintf("tool_%02d", i), Description: strings.Repeat("word ", 20)}
	}
	index := ToolIndex(long)
	if !strings.Contains(index, "query list_tools to continue") || !strings.Contains(index, "tool_00") {
		t.Fatalf("index did not truncate: %s", index)
	}
}

func TestToolListChangedDropsTheSavedCatalog(t *testing.T) {
	s := mcp.NewServer(&mcp.Implementation{Name: "changing", Version: "test"}, nil)
	s.AddTool(&mcp.Tool{Name: "first", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})
	h := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil))
	defer h.Close()
	server := config.MCPServer{ID: "changing", Transport: "streamable_http", URL: h.URL, Enabled: true, TimeoutSeconds: 2}
	dir := t.TempDir()
	m := NewManager()
	defer m.Close()
	m.SetCatalogDir(dir)
	set, err := m.Tools(t.Context(), []config.MCPServer{server}, dir, 4000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := set[0].(tool.InvokableTool).InvokableRun(t.Context(), `{}`); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadCatalog(dir, server); !ok {
		t.Fatal("a successful list did not save the catalog")
	}
	s.AddTool(&mcp.Tool{Name: "second", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := LoadCatalog(dir, server); !ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("tools/list_changed left the saved catalog in place")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestDirectToolConnectsOnlyWhenCalled(t *testing.T) {
	var calls int
	s := mcp.NewServer(&mcp.Implementation{Name: "direct", Version: "test"}, nil)
	s.AddTool(&mcp.Tool{Name: "lookup", Description: "first line\nprivate detail", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls++
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "found"}}}, nil
	})
	h := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil))
	defer h.Close()
	server := config.MCPServer{ID: "direct", Transport: "streamable_http", URL: h.URL, Enabled: true, TimeoutSeconds: 2, DisabledTools: []string{"hidden"}}
	dir := t.TempDir()
	m := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer m.Close()
	catalog := Catalog{ServerID: server.ID, Tools: []ToolDescriptor{
		{Name: "lookup", OriginalName: "lookup", Description: "first line\nprivate detail", InputSchema: map[string]any{"type": "object"}},
		{Name: "hidden", OriginalName: "hidden", Description: "kept out", InputSchema: map[string]any{"type": "object"}},
		{Name: "other", Description: strings.Repeat("字", 90), InputSchema: map[string]any{"type": "object"}},
	}}
	if err := SaveCatalog("", server, catalog); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadCatalog("", server); ok {
		t.Fatal("an empty directory reported a catalog")
	}
	mount, skipped := PrepareDirect(server, catalog, true, map[string]struct{}{"lookup": {}, "hidden": {}})
	if len(mount) != 1 || mount[0].Name != "lookup" || len(skipped) != 0 {
		t.Fatalf("only the requested enabled tool mounts: %+v skipped %v", mount, skipped)
	}
	kept, _ := PrepareDirect(server, catalog, false, map[string]struct{}{"hidden": {}})
	if len(kept) != 1 || kept[0].Name != "hidden" {
		t.Fatalf("restore keeps a currently disabled name: %+v", kept)
	}
	tools, names := m.DirectTools(server, dir, 4000, mount)
	if calls != 0 || len(tools) != 1 {
		t.Fatalf("registration connected or dropped the tool: calls=%d names=%v", calls, names)
	}
	info, err := tools[0].Info(t.Context())
	if err != nil || !strings.Contains(info.Desc, "direct") || !strings.Contains(info.Desc, "lookup") {
		t.Fatalf("description %v %v", info, err)
	}
	out, err := tools[0].(tool.InvokableTool).InvokableRun(t.Context(), `{"q":"example.test"}`)
	if err != nil || calls != 1 || !strings.Contains(out, "found") {
		t.Fatalf("call %s %v calls=%d", out, err, calls)
	}
	empty, err := tools[0].(tool.InvokableTool).InvokableRun(t.Context(), "")
	if err != nil || calls != 2 || !strings.Contains(empty, "found") {
		t.Fatalf("empty arguments %s %v", empty, err)
	}
	bad, err := tools[0].(tool.InvokableTool).InvokableRun(t.Context(), "[]")
	if err != nil || !strings.Contains(bad, "invalid_arguments") || calls != 2 {
		t.Fatalf("bad arguments ran: %s %v calls=%d", bad, err, calls)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	stopped, err := tools[0].(tool.InvokableTool).InvokableRun(cancelled, `{}`)
	if err != nil || !strings.Contains(stopped, "cancelled") || calls != 2 {
		t.Fatalf("cancelled call ran: %s %v", stopped, err)
	}
	if m.ToolOffered(nil, names[0]) || m.ToolOffered([]config.MCPServer{{ID: server.ID, Enabled: false}}, names[0]) {
		t.Fatal("a direct tool stayed offered without an enabled server")
	}
	list, _ := ProxyNames(server.ID)
	if !m.ToolOffered([]config.MCPServer{{ID: server.ID, Enabled: true}}, list) {
		t.Fatal("an unknown proxy name was hidden")
	}
	dead := server
	dead.URL = "http://127.0.0.1:1"
	deadTools, _ := m.DirectTools(dead, dir, 4000, mount)
	failed, err := deadTools[0].(tool.InvokableTool).InvokableRun(t.Context(), `{}`)
	if err != nil || !strings.Contains(failed, "connection_failed") || strings.Contains(failed, "127.0.0.1") {
		t.Fatalf("dead server: %s %v", failed, err)
	}
	if _, names := m.DirectTools(server, dir, 4000, nil); names != nil {
		t.Fatal("empty preparation registered tools")
	}
	var none *Manager
	if built, builtNames := none.DirectTools(server, dir, 4000, mount); built != nil || builtNames != nil {
		t.Fatal("nil manager registered tools")
	}
	if proxies, proxyNames, err := none.IndexedProxies(server, dir, 4000, ""); proxies != nil || proxyNames != nil || err != nil {
		t.Fatal("nil manager built proxies")
	}
	index := ToolIndex(nil)
	if index != "" || !strings.Contains(ToolIndex(catalog.Tools), "lookup — first line") || strings.Contains(ToolIndex(catalog.Tools), "private detail") || !strings.Contains(ToolIndex(catalog.Tools), "…") {
		t.Fatalf("index %q", ToolIndex(catalog.Tools))
	}
	if err := SaveCatalog(dir, server, catalog); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(catalogPath(dir, fingerprint(server)), []byte(`{"server_id":"other"}`), 0o600)
	if _, ok := LoadCatalog(dir, server); ok {
		t.Fatal("a catalog for another server was accepted")
	}
	os.WriteFile(catalogPath(dir, fingerprint(server)), []byte(`{"server_id":"direct"}`), 0o600)
	if loaded, ok := LoadCatalog(dir, server); !ok || loaded.Tools == nil {
		t.Fatal("a catalog without tools was not an empty list")
	}
	os.WriteFile(catalogPath(dir, fingerprint(server)), []byte("{"), 0o600)
	if _, ok := LoadCatalog(dir, server); ok {
		t.Fatal("a corrupt catalog was accepted")
	}
	blocked := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveCatalog(blocked, server, catalog); err == nil {
		t.Fatal("save into a file path succeeded")
	}
	if err := SaveCatalog(dir, server, Catalog{Tools: []ToolDescriptor{{InputSchema: map[string]any{"bad": make(chan int)}}}}); err == nil {
		t.Fatal("an unencodable schema was saved")
	}
	readonly := t.TempDir()
	if err := os.MkdirAll(filepath.Join(readonly, "mcp-catalogs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(readonly, "mcp-catalogs"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(readonly, "mcp-catalogs"), 0o700) })
	if err := SaveCatalog(readonly, server, catalog); err == nil {
		t.Fatal("save into a read-only catalog directory succeeded")
	}
	// Chmod follows a symlink. /dev/null refuses the mode change, so the temp
	// file is removed and the catalog is not published.
	linkDir := t.TempDir()
	linkPath := catalogPath(linkDir, fingerprint(server))
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/null", linkPath+".tmp"); err != nil {
		t.Fatal(err)
	}
	if err := SaveCatalog(linkDir, server, catalog); err == nil {
		t.Fatal("chmod of the catalog temp file succeeded")
	}
	if _, err := os.Lstat(linkPath + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("failed chmod left the temp file")
	}
	m.log = nil
	if out, err := tools[0].(tool.InvokableTool).InvokableRun(t.Context(), `{}`); err != nil || !strings.Contains(out, "found") {
		t.Fatalf("unlogged call %s %v", out, err)
	}
	bare := []ToolDescriptor{{Name: "lookup", Description: "named only", InputSchema: map[string]any{"type": "object"}}}
	named, _ := PrepareDirect(config.MCPServer{ID: "direct", DisabledTools: []string{"alias"}}, Catalog{Tools: append(bare, ToolDescriptor{Name: "alias", OriginalName: "real", InputSchema: map[string]any{"type": "object"}}, ToolDescriptor{Name: "bad", OriginalName: "bad", InputSchema: map[string]any{"type": "array"}})}, true, nil)
	if len(named) != 1 || named[0].Name != "lookup" || named[0].OriginalName != "" {
		t.Fatalf("name switch and bad schema were not skipped: %+v", named)
	}
	onlyTools, onlyNames := m.DirectTools(server, dir, 4000, named)
	onlyOut, err := onlyTools[0].(tool.InvokableTool).InvokableRun(t.Context(), `{}`)
	if err != nil || onlyNames[0] == "" || !strings.Contains(onlyOut, "found") {
		t.Fatalf("nameless original %s %v", onlyOut, err)
	}
	m.rememberDirect("", directBinding{})
	var absent *Manager
	absent.rememberDirect("x", directBinding{})
	if _, ok := absent.lookupDirect("x"); ok {
		t.Fatal("nil manager remembered a tool")
	}
	absent.SetCatalogDir(dir)
	absent.RememberCatalog(server, catalog)
	if absent.catalogDirectory() != "" {
		t.Fatal("nil manager had a catalog directory")
	}
}

func TestDirectCallReportsTheSameOutcomesAsTheProxy(t *testing.T) {
	for _, mode := range []string{"isError", "cancel", "timeout", "disconnect", "unknown", "closed", "connect-cancel"} {
		t.Run(mode, func(t *testing.T) {
			started := make(chan struct{}, 1)
			release := make(chan struct{})
			stop := make(chan struct{})
			var once sync.Once
			handler := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				once.Do(func() { close(started) })
				select {
				case <-r.Context().Done():
				case <-stop:
				}
			}))
			if mode != "connect-cancel" {
				s := mcp.NewServer(&mcp.Implementation{Name: "direct-failures", Version: "test"}, nil)
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
				handler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil)
			}
			h := httptest.NewServer(handler)
			defer func() {
				close(stop)
				close(release)
				h.Close()
			}()
			cfg := config.MCPServer{ID: "failures", Transport: "streamable_http", URL: h.URL, Enabled: true, TimeoutSeconds: 1}
			if mode == "connect-cancel" {
				cfg.TimeoutSeconds = 30
			}
			m := NewManager()
			defer m.Close()
			remote := "perform"
			if mode == "unknown" {
				remote = "missing"
			}
			prepared := []ToolDescriptor{{Name: remote, OriginalName: remote, Description: "perform", InputSchema: map[string]any{"type": "object"}}}
			tools, _ := m.DirectTools(cfg, t.TempDir(), 8000, prepared)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "closed" {
				m.Close()
			}
			done := make(chan string, 1)
			go func() {
				out, err := tools[0].(tool.InvokableTool).InvokableRun(ctx, `{}`)
				if err != nil {
					done <- err.Error()
				} else {
					done <- out
				}
			}()
			if mode != "unknown" && mode != "closed" {
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					t.Fatal("direct call did not reach the server")
				}
			}
			switch mode {
			case "cancel", "connect-cancel":
				cancel()
			case "disconnect":
				m.Close()
			}
			want := map[string]string{"isError": `"isError":true`, "cancel": "cancelled", "timeout": "timeout", "disconnect": "closed", "unknown": "unknown_tool", "closed": "closed", "connect-cancel": "cancelled"}[mode]
			select {
			case out := <-done:
				if !strings.Contains(out, want) {
					t.Fatalf("result %s missing %s", out, want)
				}
			case <-time.After(4 * time.Second):
				t.Fatal("direct call ignored its outcome")
			}
		})
	}
}
