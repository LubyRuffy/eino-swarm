package engine

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/mcpclient"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/components/tool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPSchemaTokensCountCJKAboveAnEqualASCIILength(t *testing.T) {
	schema := map[string]any{"type": "object"}
	ascii := mcpSchemaTokens([]mcpclient.ToolDescriptor{{Name: "tool", Description: strings.Repeat("a", 12), InputSchema: schema}})
	cjk := mcpSchemaTokens([]mcpclient.ToolDescriptor{{Name: "tool", Description: strings.Repeat("中", 4), InputSchema: schema}})
	if cjk <= ascii {
		t.Fatalf("CJK schema tokens %d were not above ASCII %d", cjk, ascii)
	}
	if mcpMountBudget(0, config.DefaultAutoCompactTokens) != config.DefaultAutoCompactTokens || mcpMountBudget(128000, config.DefaultAutoCompactTokens) != 128000 || mcpMountBudget(0, 0) != 0 {
		t.Fatal("unknown window did not use the compact budget")
	}
	if toolsetHasMCP(nil) || toolsetHasMCP(&tools.Set{Names: []string{"read"}}) || !toolsetHasMCP(&tools.Set{Names: []string{"mcp_kept"}}) {
		t.Fatal("MCP names were not recognized")
	}
}

func TestSmallCatalogMountsRemoteToolsAndRestoreKeepsThoseNames(t *testing.T) {
	server, calls := engineMCPFixture(t)
	var mounted string
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			for _, offered := range request.Tools {
				if strings.Contains(offered.Function.Name, "_list_tools_") || strings.Contains(offered.Function.Name, "_call_tool_") {
					t.Errorf("small catalog kept a proxy: %s", offered.Function.Name)
				}
				if strings.Contains(offered.Function.Name, "inspect_record") {
					mounted = offered.Function.Name
				}
			}
			if mounted == "" || !strings.Contains(request.Messages[0].Content, mounted) {
				t.Fatalf("manager prompt did not list the remote tool %q", mounted)
			}
			writeScheduleCompletionTool(w, request, mounted, `{"key":"record"}`)
		default:
			raw, _ := json.Marshal(request.Messages)
			if calls.Load() != 1 || !strings.Contains(string(raw), "mcp-protocol-evidence") {
				t.Errorf("direct call did not run the remote tool: calls=%d %s", calls.Load(), raw)
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The record was inspected."}, "stop")
		}
	})
	e.cfg.MCP.Servers = []config.MCPServer{server}
	thread, err := e.CreateThread("Direct remote tools", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Inspect the external record.")
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnDone || mounted == "" {
		t.Fatalf("status=%s tool=%s err=%q", finished.Status, mounted, finished.Error)
	}
	events, err := e.store.ListEventsByTurn(thread.ID, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	var record mcpMountRecord
	found := false
	for _, ev := range events {
		if ev.Kind != KindMCPCatalog {
			continue
		}
		if strings.Contains(ev.Text, server.URL) {
			t.Fatalf("catalog trace included the endpoint: %s", ev.Text)
		}
		if json.Unmarshal([]byte(ev.Text), &record) != nil || record.Mode != "direct" || record.Window != 0 || record.Budget != config.DefaultAutoCompactTokens {
			t.Fatalf("mount trace: %s", ev.Text)
		}
		found = true
	}
	if !found || len(record.Servers) != 1 || len(record.Servers[0].Remotes) != 1 {
		t.Fatalf("missing direct decision: %+v", record)
	}
	set, err := e.conversationTools(t.Context(), thread)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := e.applyMCPSurface(t.Context(), turn, set, true)
	if err != nil || !strings.Contains(strings.Join(restored.Names, " "), mounted) || strings.Contains(strings.Join(restored.Names, " "), "_list_tools_") {
		t.Fatalf("restore changed the mounted names: %v %v", restored.Names, err)
	}
	if err := os.RemoveAll(filepath.Join(e.cfg.DataDir(), "mcp-catalogs")); err != nil {
		t.Fatal(err)
	}
	missing, err := e.applyMCPSurface(t.Context(), turn, set, true)
	joined := strings.Join(missing.Names, " ")
	if err != nil || strings.Contains(joined, "inspect_record") || strings.Contains(joined, "_list_tools_") || strings.Contains(joined, "_call_tool_") {
		t.Fatalf("a missing catalog was swapped for another tool surface: %v %v", missing.Names, err)
	}
}

func TestCatalogOverTheCompactBudgetKeepsAnIndexedProxy(t *testing.T) {
	server, _ := engineMCPFixture(t)
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, request scheduleCompletionRequest) {
		seen := false
		for _, offered := range request.Tools {
			if strings.Contains(offered.Function.Name, "_list_tools_") {
				seen = true
				if !strings.Contains(offered.Function.Description, "inspect_record") || !strings.Contains(offered.Function.Description, "query this catalog") || !strings.Contains(offered.Function.Description, "server title is not evidence") {
					t.Errorf("proxy index: %s", offered.Function.Description)
				}
			}
			if strings.Contains(offered.Function.Name, "inspect_record") && !strings.Contains(offered.Function.Name, "_list_tools_") {
				t.Errorf("over-budget catalog mounted a direct tool: %s", offered.Function.Name)
			}
		}
		if !seen {
			t.Error("over-budget catalog dropped the catalog proxy")
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "The catalog was queried."}, "stop")
	})
	e.cfg.Swarm.AutoCompactTokens = 32
	e.cfg.MCP.Servers = []config.MCPServer{server}
	thread, err := e.CreateThread("Indexed MCP", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Find a matching external tool.")
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnDone {
		t.Fatalf("status=%s err=%q", finished.Status, finished.Error)
	}
	events, err := e.store.ListEventsByTurn(thread.ID, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind != KindMCPCatalog {
			continue
		}
		var record mcpMountRecord
		if json.Unmarshal([]byte(ev.Text), &record) != nil || record.Mode != "progressive" || record.Window != 0 || record.Budget != 32 {
			t.Fatalf("unknown-window decision: %s", ev.Text)
		}
		return
	}
	t.Fatal("missing progressive decision")
}

func TestZeroDirectLoadDoesNotConnectBeforeTheModel(t *testing.T) {
	var hits atomic.Int32
	s := mcp.NewServer(&mcp.Implementation{Name: "quiet", Version: "test"}, nil)
	s.AddTool(&mcp.Tool{Name: "inspect_record", Description: "Inspect external evidence", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})
	inner := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil)
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(remote.Close)
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, request scheduleCompletionRequest) {
		if hits.Load() != 0 {
			t.Errorf("progressive mode connected before the model request: %d", hits.Load())
		}
		seen := false
		for _, offered := range request.Tools {
			if strings.Contains(offered.Function.Name, "_list_tools_") {
				seen = true
			}
		}
		if !seen {
			t.Error("percent 0 did not keep the catalog proxy")
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "No external call was required."}, "stop")
	})
	e.cfg.MCP.DirectLoadPercent = 0
	e.cfg.MCP.Servers = []config.MCPServer{{ID: "quiet", Name: "Quiet", Transport: "streamable_http", URL: remote.URL, Enabled: true, TimeoutSeconds: 2}}
	thread, err := e.CreateThread("Progressive MCP", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Answer from the available tools.")
	if err != nil {
		t.Fatal(err)
	}
	if finished := waitForTurn(t, e, turn.ID); finished.Status != store.TurnDone || hits.Load() != 0 {
		t.Fatalf("status=%s hits=%d err=%q", finished.Status, hits.Load(), finished.Error)
	}
	set, err := e.conversationTools(t.Context(), thread)
	if err != nil {
		t.Fatal(err)
	}
	e.cfg.MCP.DirectLoadPercent = 5
	restored, err := e.applyMCPSurface(t.Context(), turn, set, true)
	joined := strings.Join(restored.Names, " ")
	if err != nil || !strings.Contains(joined, "_list_tools_") || hits.Load() != 0 {
		t.Fatalf("resume replaced the stored proxies: %v hits=%d %v", restored.Names, hits.Load(), err)
	}
}

func TestSwitchingOffADirectToolRemovesItFromTheNextRequest(t *testing.T) {
	server, calls := engineMCPFixture(t)
	var mounted string
	var e *Engine
	e, _ = newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			for _, offered := range request.Tools {
				if strings.Contains(offered.Function.Name, "inspect_record") {
					mounted = offered.Function.Name
				}
			}
			if mounted == "" {
				t.Fatal("direct tool was not mounted")
			}
			if err := e.UpdateConfig(func(cfg *config.Config) error {
				cfg.MCP.Servers[0].DisabledTools = []string{"inspect_record"}
				return nil
			}); err != nil {
				t.Error(err)
			}
			writeScheduleCompletionTool(w, request, mounted, `{"key":"record"}`)
		default:
			raw, _ := json.Marshal(request)
			if requestHasTool(request, mounted) || calls.Load() != 0 || !strings.Contains(string(raw), "tool_switched_off") {
				t.Errorf("switched-off tool remained callable: calls=%d tools=%s", calls.Load(), raw)
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The tool stayed off."}, "stop")
		}
	})
	e.cfg.MCP.Servers = []config.MCPServer{server}
	thread, err := e.CreateThread("Switch a remote tool", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Use the external record tool.")
	if err != nil {
		t.Fatal(err)
	}
	if finished := waitForTurn(t, e, turn.ID); finished.Status != store.TurnDone {
		t.Fatalf("status=%s err=%q", finished.Status, finished.Error)
	}
}

func TestFailedCatalogListKeepsTheTurnOnTheProxy(t *testing.T) {
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, request scheduleCompletionRequest) {
		seen := false
		for _, offered := range request.Tools {
			if strings.Contains(offered.Function.Name, "_list_tools_") {
				seen = true
			}
		}
		if !seen {
			t.Error("a failed catalog dropped the built-in proxy")
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "Built-in tools remain available."}, "stop")
	})
	e.cfg.MCP.Servers = []config.MCPServer{{
		ID: "down", Name: "Down", Transport: "streamable_http", URL: "http://127.0.0.1:1", Enabled: true, TimeoutSeconds: 1,
	}}
	thread, err := e.CreateThread("Unavailable MCP", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Continue with the tools that are available.")
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnDone {
		t.Fatalf("status=%s err=%q", finished.Status, finished.Error)
	}
	events, err := e.store.ListEventsByTurn(thread.ID, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind != KindMCPCatalog {
			continue
		}
		if strings.Contains(ev.Text, "127.0.0.1") || !strings.Contains(ev.Text, "connection_failed") {
			t.Fatalf("failure trace: %s", ev.Text)
		}
		return
	}
	t.Fatal("missing catalog failure")
}

func TestNuwaManagerSeesDirectMCPNamesOnlyOnTheWorker(t *testing.T) {
	server, _ := engineMCPFixture(t)
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		for _, offered := range request.Tools {
			if strings.HasPrefix(offered.Function.Name, "mcp_") {
				t.Errorf("Nuwa manager received an MCP tool: %s", offered.Function.Name)
			}
		}
		if step == 1 {
			writeScheduleCompletionTool(w, request, "list_worker_tools", "{}")
			return
		}
		raw, _ := json.Marshal(request.Messages)
		text := string(raw)
		if !strings.Contains(text, "inspect_record") || strings.Contains(text, "_list_tools_") || strings.Contains(text, "_call_tool_") {
			t.Errorf("worker catalog did not list the direct tool: %s", text)
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "The worker tools were listed."}, "stop")
	})
	e.cfg.MCP.Servers = []config.MCPServer{server}
	project, err := e.CreateProject("Direct worker tools", "", "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Nuwa direct tools", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "List the worker tools.")
	if err != nil {
		t.Fatal(err)
	}
	if finished := waitForTurn(t, e, turn.ID); finished.Status != store.TurnDone {
		t.Fatalf("status=%s err=%q", finished.Status, finished.Error)
	}
}

func TestMCPMountFileKeepsTheDecisionAndRejectsABadTurnID(t *testing.T) {
	if _, err := mcpMountPath("", "tn_mount"); err == nil {
		t.Fatal("empty directory built a path")
	}
	if _, err := mcpMountPath(t.TempDir(), "../tn"); err == nil || func() error { _, err := mcpMountPath(t.TempDir(), `tn\x`); return err }() == nil {
		t.Fatal("turn id traversal was accepted")
	}
	e := newTestEngine(t)
	var missing *Engine
	base := &tools.Set{Names: []string{"read", ToolQueryHistory}, Tools: []tool.BaseTool{nil, nil}}
	if next, err := missing.applyMCPSurface(t.Context(), &store.Turn{ID: "tn_mount"}, base, false); next != base || err != nil {
		t.Fatal("nil engine changed the toolset")
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := e.applyMCPSurface(cancelled, &store.Turn{ID: "tn_mount"}, &tools.Set{Names: []string{"mcp_kept"}}, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled resolve: %v", err)
	}
	old := &tools.Set{Names: []string{"mcp_kept_list_tools"}}
	if next, err := e.applyMCPSurface(t.Context(), &store.Turn{ID: "tn_legacy"}, old, true); next != old || err != nil {
		t.Fatal("a resumed turn without a decision redialed")
	}
	turn := &store.Turn{ID: "tn_mountfile", ThreadID: "th_mountfile"}
	e.storeMCPMount(turn, mcpMountRecord{Mode: "direct", Percent: 5, Budget: config.DefaultAutoCompactTokens, Servers: []mcpMountServer{{ID: "kept", Mode: "direct", Remotes: []string{"lookup"}}}})
	path, err := mcpMountPath(e.cfg.DataDir(), turn.ID)
	info, statErr := os.Stat(path)
	loaded, ok := e.loadMCPMount(turn.ID)
	if err != nil || statErr != nil || info.Mode().Perm() != 0o600 || !ok || loaded.Mode != "direct" || loaded.Percent != 5 {
		t.Fatalf("mount file path=%s mode=%v err=%v stat=%v loaded=%+v ok=%v", path, info.Mode().Perm(), err, statErr, loaded, ok)
	}
	e.cfg.MCP.Servers = []config.MCPServer{{ID: "kept", Name: "Kept", Enabled: true, Transport: "stdio", Command: "unused"}}
	restored := e.rebuildMCPSurface(base, loaded)
	for _, name := range restored.Names {
		if strings.HasPrefix(name, "mcp_") {
			t.Fatalf("a missing catalog became a tool: %s", name)
		}
	}
	progressive := e.rebuildMCPSurface(base, mcpMountRecord{Mode: "progressive", Servers: []mcpMountServer{{ID: "kept", Mode: "progressive"}, {ID: "gone", Mode: "direct"}}})
	var proxies int
	for _, name := range progressive.Names {
		if strings.Contains(name, "_list_tools_") || strings.Contains(name, "_call_tool_") {
			proxies++
		}
	}
	if proxies != 2 {
		t.Fatalf("progressive restore names %v", progressive.Names)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.loadMCPMount(turn.ID); ok {
		t.Fatal("a corrupt decision was restored")
	}
	e.republishWorkerSurface(nil, base, nil, turn)
	e.storeMCPMount(nil, mcpMountRecord{Mode: "direct"})
	if _, ok := e.loadMCPMount(""); ok {
		t.Fatal("an empty turn id was loaded")
	}
	if _, ok := (&Engine{}).loadMCPMount("tn_mount"); ok || func() bool { _, ok := missing.loadMCPMount("tn_mount"); return ok }() {
		t.Fatal("an engine without configuration loaded a decision")
	}
	if !missing.mcpOffered(nil, "read") || !(&Engine{}).mcpOffered(nil, "read") {
		t.Fatal("a name outside MCP was hidden")
	}
	added := withMCPTools(&tools.Set{Names: []string{"read"}, Tools: []tool.BaseTool{nil}}, []tool.BaseTool{nil}, []string{"mcp_added"})
	if added.Names[len(added.Names)-1] != "mcp_added" {
		t.Fatalf("tools were not appended: %v", added.Names)
	}
}

func TestCachedCatalogMountsDirectlyWithoutDialing(t *testing.T) {
	e := newTestEngine(t)
	server := config.MCPServer{ID: "cached", Name: "Cached", Transport: "streamable_http", URL: "http://127.0.0.1:1", Enabled: true, TimeoutSeconds: 1}
	catalog := mcpclient.Catalog{ServerID: server.ID, Tools: []mcpclient.ToolDescriptor{{Name: "lookup", Description: "find", InputSchema: map[string]any{"type": "object"}}}}
	if err := mcpclient.SaveCatalog(e.cfg.DataDir(), server, catalog); err != nil {
		t.Fatal(err)
	}
	e.cfg.MCP.DirectLoadPercent = 5
	e.cfg.MCP.Servers = []config.MCPServer{server, {ID: "off", Enabled: false}}
	set := &tools.Set{Names: []string{"mcp_cached_list_tools", ToolQueryHistory}, Tools: []tool.BaseTool{nil, nil}, WorkspaceDir: t.TempDir()}
	next, record, err := e.resolveMCPSurface(t.Context(), &store.Turn{ID: "tn_cached"}, set)
	if err != nil || record.Mode != "direct" || record.Servers[0].Remotes[0] != "lookup" {
		t.Fatalf("cached decision %+v %v", record, err)
	}
	var direct bool
	for _, name := range next.Names {
		if strings.Contains(name, "lookup") {
			direct = true
		}
		if strings.Contains(name, "_list_tools_") || strings.Contains(name, "_call_tool_") {
			t.Fatalf("cache hit kept a proxy: %s", name)
		}
	}
	if !direct {
		t.Fatalf("cache hit did not mount the remote tool: %v", next.Names)
	}
	e.cfg.MCP.Servers = nil
	_, empty, err := e.resolveMCPSurface(t.Context(), &store.Turn{ID: "tn_empty"}, set)
	if err != nil || empty.Mode != "progressive" || len(empty.Servers) != 0 {
		t.Fatalf("empty server list %+v %v", empty, err)
	}
}
