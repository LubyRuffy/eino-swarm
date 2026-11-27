package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/mcpclient"
	"github.com/LubyRuffy/eino-swarm/internal/panelplugins/easm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/taskpanel"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const easmFixtureQuery = `org="Subject Co"`

// easmFixtureServer offers the aggregations the producer owns. The heat tool
// answers; the stats tool refuses, so one failing aggregation cannot hide the
// other steps' results.
func easmFixtureServer(t *testing.T) config.MCPServer {
	t.Helper()
	remote := mcp.NewServer(&mcp.Implementation{Name: "easm-fixture", Version: "test"}, nil)
	remote.AddTool(&mcp.Tool{Name: "heat_fixture", InputSchema: map[string]any{"type": "object"}}, func(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct{ Query string }
		_ = json.Unmarshal(request.Params.Arguments, &args)
		raw, _ := json.Marshal(map[string]any{
			"query": args.Query,
			"hot_trend": []any{
				map[string]any{"date": "2026-01-02", "value": 3},
				map[string]any{"date": "2026-01-09", "value": 8},
			},
		})
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}, nil
	})
	remote.AddTool(&mcp.Tool{Name: "stats_fixture", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "quota exhausted"}}}, nil
	})
	endpoint := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return remote }, nil))
	t.Cleanup(endpoint.Close)
	return config.MCPServer{ID: "easm-fixture", Name: "EASM fixture", Transport: "streamable_http", URL: endpoint.URL, Enabled: true, TimeoutSeconds: 3}
}

// easmToolCall writes one tool call with its own id: a later call reusing an id
// would be the same operation, and the store refuses to apply it twice.
func easmToolCall(w http.ResponseWriter, request scheduleCompletionRequest, id, name, args string) {
	writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{
		"index": 0, "id": id, "type": "function",
		"function": map[string]any{"name": name, "arguments": args},
	}}}, "tool_calls")
}

// Real model → Registry tool → EASM producer → MCP HTTP → durable panel. The
// agent pushes two batches of its own findings and asks for the aggregations on
// the second; a refused aggregation must stay inside its panel step so the
// turn still finishes and the graph the agent found is kept.
func TestEASMSourceMergesPushedBatchesThroughActualManagerHTTP(t *testing.T) {
	server := easmFixtureServer(t)
	var receipt string
	var panelID string
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			easmToolCall(w, request, "easm-open", "open_task_panel", `{"plugin_id":"easm-asset-portrait"}`)
		case 2:
			var panel taskpanel.Panel
			if err := json.Unmarshal([]byte(request.Messages[len(request.Messages)-1].Content), &panel); err != nil {
				t.Error(err)
			}
			panelID = panel.ID
			input := map[string]any{
				"subject": "Subject Co",
				"companies": []any{
					map[string]any{"name": "Parent Co", "registry_id": "9", "relation": "investment", "confirmed": true, "source": "relation_tool"},
				},
				"domains": []any{
					map[string]any{"domain": "subject.test", "parent": "Parent Co", "confirmed": true, "source": "org_tool"},
				},
				"certificates": []any{
					map[string]any{"serial": "0a1b", "domain": "subject.test", "issuer": "CA", "subject_cn": "subject.test", "valid": true, "match": true, "source": "org_tool"},
				},
			}
			raw, err := json.Marshal(map[string]any{"panel_id": panel.ID, "input": input})
			if err != nil {
				t.Error(err)
			}
			easmToolCall(w, request, fmt.Sprintf("easm-batch-%d", step), "run_panel_source", string(raw))
		case 3:
			input := map[string]any{
				"subject": "Subject Co",
				"domains": []any{
					map[string]any{"domain": "child.test", "confirmed": false, "source": "branch_tool"},
				},
				"query": easmFixtureQuery,
			}
			raw, err := json.Marshal(map[string]any{"panel_id": panelID, "input": input})
			if err != nil {
				t.Error(err)
			}
			easmToolCall(w, request, fmt.Sprintf("easm-batch-%d", step), "run_panel_source", string(raw))
		default:
			receipt = request.Messages[len(request.Messages)-1].Content
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The portrait is in the panel."}, "stop")
		}
	})
	if err := e.cfg.Update(func(c *config.Config) error {
		c.MCP.Servers = []config.MCPServer{server}
		c.MCP.DirectLoadPercent = 0 // A cold explicit progressive binding needs no saved catalog.
		c.TaskPanels.EASM.ServerID = server.ID
		c.TaskPanels.EASM.Tools = map[string]string{"stats": "stats_fixture", "heat": "heat_fixture", "product_rules": ""}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Portrait fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Build the portrait of the confirmed subject.")
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("turn = %+v", got)
	}
	waitSettled(t, e, thread.ID)
	panels, err := e.store.TaskPanels(t.Context(), thread.ID)
	if err != nil || len(panels) != 1 {
		t.Fatalf("panels=%v err=%v", panels, err)
	}
	var state easm.State
	if err := json.Unmarshal(panels[0].State, &state); err != nil {
		t.Fatal(err)
	}
	if state.Title != "Subject Co" || state.Query != easmFixtureQuery || state.Collection.Status != "done" {
		t.Fatalf("state = %s", panels[0].State)
	}
	// Both batches reached the durable panel, not just the last one.
	for _, id := range []string{"company:reg:9", "domain:subject.test", "domain:child.test", "feature:cert:0a1b@CA"} {
		found := false
		for _, node := range state.Nodes {
			found = found || node.ID == id
		}
		if !found {
			t.Fatalf("%s is missing from %s", id, panels[0].State)
		}
	}
	statuses := map[easm.Step]easm.StepStatus{}
	for _, status := range state.Collection.Steps {
		statuses[status.ID] = status
	}
	if statuses[easm.StepHeat] != (easm.StepStatus{ID: easm.StepHeat, Status: "done"}) || len(state.Heat) != 2 {
		t.Fatalf("heat = %+v %+v", statuses[easm.StepHeat], state.Heat)
	}
	for _, step := range []easm.Step{easm.StepSummary, easm.StepComponents} {
		if got := statuses[step]; got.Status != "failed" || got.Code != "tool_error" {
			t.Fatalf("%s = %+v", step, got)
		}
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal([]byte(receipt), &result) != nil || !result.OK {
		t.Fatalf("receipt = %s", receipt)
	}
}

// Transport outcomes map to step outcomes: a missing tool is unavailable, a
// remote refusal keeps only its code, and no server means unavailable.
func TestEASMCallerMapsTransportOutcomes(t *testing.T) {
	server := easmFixtureServer(t)
	manager := mcpclient.NewManager()
	defer manager.Close()
	servers := map[string]config.MCPServer{"absent_tool": server, "stats_fixture": server, "heat_fixture": server}
	caller := &easmCaller{manager: manager, servers: servers, workspace: t.TempDir()}
	if _, err := caller.Call(t.Context(), "absent_tool", nil); err != easm.ErrUnavailable {
		t.Fatalf("missing tool err=%v", err)
	}
	_, err := caller.Call(t.Context(), "stats_fixture", nil)
	if failure, ok := err.(*easm.ToolError); !ok || failure.Code != "tool_error" {
		t.Fatalf("refusal err=%v", err)
	}
	if body, err := caller.Call(t.Context(), "heat_fixture", map[string]any{"query": easmFixtureQuery}); err != nil || len(body) == 0 {
		t.Fatalf("body=%s err=%v", body, err)
	}
	manager.UseRemoteSwitches(func() []config.MCPServer {
		current := server
		current.DisabledTools = []string{"heat_fixture"}
		return []config.MCPServer{current}
	})
	if _, err := caller.Call(t.Context(), "heat_fixture", nil); err != easm.ErrUnavailable {
		t.Fatalf("live switch did not revoke a mounted binding: %v", err)
	}
	for _, unresolved := range []*easmCaller{
		{manager: manager},
		{servers: servers},
	} {
		if _, err := unresolved.Call(t.Context(), "heat_fixture", nil); err != easm.ErrUnavailable {
			t.Fatalf("unresolved server err=%v", err)
		}
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := caller.Call(cancelled, "heat_fixture", nil); err != context.Canceled {
		t.Fatalf("cancelled err=%v", err)
	}
}

func TestPanelMCPOnlyBindsToolsOnTheRecordedSurface(t *testing.T) {
	one := config.MCPServer{ID: "one", Enabled: true}
	two := config.MCPServer{ID: "two", Enabled: true}
	toolsByServer := []mountedPanelMCP{
		{server: one, tools: []mcpclient.ToolDescriptor{{OriginalName: "stats"}, {Name: "heat"}}},
		{server: two, tools: []mcpclient.ToolDescriptor{{OriginalName: "stats"}}},
	}
	settings := config.EASMPanelConfig{Tools: map[string]string{"stats": "stats", "heat": "heat", "missing": "missing", "off": ""}}
	implicit := panelMCPBindings(settings, toolsByServer)
	if len(implicit) != 1 || implicit["heat"] != (panelMCPBinding{ServerID: "one", Name: "heat", Destination: panelMCPDestination(one)}) {
		t.Fatalf("only a uniquely offered tool may be implicit: %+v", implicit)
	}
	settings.ServerID = "two"
	explicit := panelMCPBindings(settings, toolsByServer)
	if len(explicit) != 1 || explicit["stats"] != (panelMCPBinding{ServerID: "two", Name: "stats", Destination: panelMCPDestination(two)}) {
		t.Fatalf("explicit owner = %+v", explicit)
	}
	_, proxy := mcpclient.ProxyNames("one")
	set := &tools.Set{Names: []string{proxy, mcpclient.DirectToolName("two", "another_tool")}}
	servers := []config.MCPServer{one, two}
	bindPanelMCP(set, implicit, servers)
	if set.PanelMCP["heat"].Server.ID != "one" {
		t.Fatalf("recorded progressive tool missing: %+v", set.PanelMCP)
	}
	bindPanelMCP(set, explicit, servers)
	if len(set.PanelMCP) != 0 {
		t.Fatalf("mounted a recorded binding without its direct tool: %+v", set.PanelMCP)
	}
	set.Names = append(set.Names, mcpclient.DirectToolName("two", "stats"))
	servers[1].Enabled = false
	bindPanelMCP(set, explicit, servers)
	if len(set.PanelMCP) != 0 {
		t.Fatalf("rebound a disabled server: %+v", set.PanelMCP)
	}
}

func TestPanelMCPKeepsInitialSettingsAndDoesNotRestoreToAReplacement(t *testing.T) {
	e := newTestEngine(t)
	var requests atomic.Int32
	localEndpoint := func() string {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(server.Close)
		return server.URL
	}
	one := config.MCPServer{ID: "original", Transport: "streamable_http", URL: localEndpoint(), Enabled: true, DisabledTools: []string{"blocked"}}
	two := config.MCPServer{ID: "late", Transport: "streamable_http", URL: localEndpoint()}
	replacement := localEndpoint()
	if err := e.UpdateConfig(func(c *config.Config) error {
		c.MCP.DirectLoadPercent = 0
		c.MCP.Servers = []config.MCPServer{one, two}
		c.TaskPanels.EASM.ServerID = one.ID
		c.TaskPanels.EASM.Tools = map[string]string{"stats": "original_tool", "heat": "blocked", "product_rules": ""}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Portrait admission before first request", "", "")
	if err != nil {
		t.Fatal(err)
	}
	set, err := e.conversationTools(t.Context(), thread)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.UpdateConfig(func(c *config.Config) error {
		c.MCP.Servers[0].DisabledTools = nil
		c.MCP.Servers[1].Enabled = true
		c.TaskPanels.EASM.ServerID = two.ID
		c.TaskPanels.EASM.Tools["stats"] = "other_tool"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ID: "easm-settings-receipt", ThreadID: thread.ID}
	mounted, record, err := e.resolveMCPSurface(t.Context(), turn, set)
	if err != nil {
		t.Fatal(err)
	}
	_, lateProxy := mcpclient.ProxyNames(two.ID)
	if slices.Contains(mounted.Names, lateProxy) || len(mounted.PanelMCP) != 1 ||
		mounted.PanelMCP["stats"].Name != "original_tool" || mounted.PanelMCP["stats"].Server.ID != one.ID {
		t.Fatalf("initial settings were overwritten: %+v %+v", mounted.Names, record.PanelTools)
	}
	e.storeMCPMount(turn, record)
	loaded, ok := e.loadMCPMount(turn.ID)
	if !ok {
		t.Fatal("missing the recorded mount")
	}
	restored := e.rebuildMCPSurface(set, loaded)
	if len(restored.PanelMCP) != 1 || restored.PanelMCP["stats"].Name != "original_tool" {
		t.Fatalf("restored a different binding: %+v", restored.PanelMCP)
	}
	if err := e.UpdateConfig(func(c *config.Config) error {
		c.MCP.Servers[0].URL = replacement
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := e.rebuildMCPSurface(set, loaded).PanelMCP; len(got) != 0 {
		t.Fatalf("redirected the original panel binding to a replacement: %+v", got)
	}
	if requests.Load() != 0 {
		t.Fatalf("cold progressive admission contacted MCP %d times", requests.Load())
	}
}

// A restricted account's turn mounts no MCP tools. The panel source runs on
// the agent's behalf, so it must not reach the MCP server either: the
// aggregations report unavailable and the server sees no call.
func TestRestrictedAccountPanelSourceMakesNoMCPCall(t *testing.T) {
	var calls atomic.Int32
	remote := mcp.NewServer(&mcp.Implementation{Name: "easm-counted", Version: "test"}, nil)
	for _, name := range []string{"stats_fixture", "heat_fixture"} {
		remote.AddTool(&mcp.Tool{Name: name, InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			calls.Add(1)
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: `{}`}}}, nil
		})
	}
	endpoint := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return remote }, nil))
	t.Cleanup(endpoint.Close)
	server := config.MCPServer{ID: "easm-counted", Transport: "streamable_http", URL: endpoint.URL, Enabled: true, TimeoutSeconds: 3}

	var panelID string
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			easmToolCall(w, request, "easm-open", "open_task_panel", `{"plugin_id":"easm-asset-portrait"}`)
		case 2:
			var panel taskpanel.Panel
			if err := json.Unmarshal([]byte(request.Messages[len(request.Messages)-1].Content), &panel); err != nil {
				t.Error(err)
			}
			panelID = panel.ID
			raw, _ := json.Marshal(map[string]any{"panel_id": panel.ID, "input": map[string]any{"subject": "Subject Co", "query": easmFixtureQuery}})
			easmToolCall(w, request, "easm-final", "run_panel_source", string(raw))
		default:
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "done"}, "stop")
		}
	})
	if err := e.cfg.Update(func(c *config.Config) error {
		c.MCP.Servers = []config.MCPServer{server}
		c.TaskPanels.EASM.ServerID = server.ID
		c.TaskPanels.EASM.Tools = map[string]string{"stats": "stats_fixture", "heat": "heat_fixture", "product_rules": ""}
		// Account profiles apply only outside local model mode.
		c.Product.Mode = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	base := e.cfg.Snapshot().Models.Providers[0].BaseURL
	src, err := provider.NewAccountProfile(accountCatalogID, base, "token", provider.AccountProfile{Label: "Restricted", Tools: []string{"read"}, RestrictTools: true})
	if err != nil {
		t.Fatal(err)
	}
	e.pool.SetAccountSource(src)
	thread, err := e.CreateThread("Restricted portrait", src.ID(), "")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := e.store.GetThread(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if e.turnMountsMCP(stored) {
		t.Fatal("a restricted account still mounts MCP tools")
	}
	turn, err := e.StartTurn(thread.ID, "Build the portrait of the confirmed subject.")
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("turn = %+v", got)
	}
	waitSettled(t, e, thread.ID)
	panel, err := e.store.TaskPanel(t.Context(), thread.ID, panelID)
	if err != nil {
		t.Fatal(err)
	}
	var state easm.State
	if err := json.Unmarshal(panel.State, &state); err != nil {
		t.Fatal(err)
	}
	for _, status := range state.Collection.Steps {
		if status.Status != "unavailable" {
			t.Fatalf("step %s = %+v in %s", status.ID, status, panel.State)
		}
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("restricted turn reached the MCP server %d times", got)
	}
}

// The settings save happens after the model has received this turn's tools.
// The panel and the model must see the same server admission until the next turn.
func TestEASMServerEnabledMidTurnWaitsForTheNextTurn(t *testing.T) {
	var requests, calls atomic.Int32
	const heatTool = "heat_fixture"
	remote := mcp.NewServer(&mcp.Implementation{Name: "easm-admission", Version: "test"}, nil)
	remote.AddTool(&mcp.Tool{Name: heatTool, InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: `{"hot_trend":[]}`}}}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return remote }, nil)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(endpoint.Close)
	server := config.MCPServer{ID: "easm-admission", Transport: "streamable_http", URL: endpoint.URL, TimeoutSeconds: 3}
	var engine *Engine
	var panelID string
	engine, model := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			easmToolCall(w, request, "admission-open", "open_task_panel", `{"plugin_id":"easm-asset-portrait"}`)
		case 2, 4:
			if step == 2 {
				var panel taskpanel.Panel
				if err := json.Unmarshal([]byte(request.Messages[len(request.Messages)-1].Content), &panel); err != nil {
					t.Error(err)
				}
				panelID = panel.ID
				if err := engine.UpdateConfig(func(c *config.Config) error {
					c.MCP.Servers[0].Enabled = true
					return nil
				}); err != nil {
					t.Error(err)
				}
			}
			raw, _ := json.Marshal(map[string]any{"panel_id": panelID, "input": map[string]any{"subject": "Fixture", "query": easmFixtureQuery}})
			easmToolCall(w, request, fmt.Sprintf("admission-collect-%d", step), "run_panel_source", string(raw))
		default:
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "done"}, "stop")
		}
	})
	if err := engine.UpdateConfig(func(c *config.Config) error {
		c.MCP.Servers = []config.MCPServer{server}
		c.TaskPanels.EASM.ServerID = server.ID
		c.TaskPanels.EASM.Tools = map[string]string{"stats": "", "heat": heatTool, "product_rules": ""}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	thread, err := engine.CreateThread("Portrait admission", "", "")
	if err != nil {
		t.Fatal(err)
	}
	run := func(text, wantStatus string) {
		t.Helper()
		turn, err := engine.StartTurn(thread.ID, text)
		if err != nil {
			t.Fatal(err)
		}
		if got := waitForTurn(t, engine, turn.ID); got.Status != store.TurnDone {
			t.Fatalf("turn = %+v", got)
		}
		waitSettled(t, engine, thread.ID)
		panels, err := engine.store.TaskPanels(t.Context(), thread.ID)
		if err != nil || len(panels) != 1 {
			t.Fatalf("panels=%v err=%v", panels, err)
		}
		var state easm.State
		if err := json.Unmarshal(panels[0].State, &state); err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(state.Collection.Steps, func(status easm.StepStatus) bool {
			return status.ID == easm.StepHeat && status.Status == wantStatus
		}) {
			t.Errorf("want heat %s, got %+v", wantStatus, state.Collection.Steps)
		}
	}
	run("Collect the portrait", "unavailable")
	if requests.Load() != 0 || calls.Load() != 0 {
		t.Errorf("first turn reached the late-enabled MCP endpoint: HTTP=%d calls=%d", requests.Load(), calls.Load())
	}

	run("Collect the missing aggregation", "done")
	if calls.Load() != 1 {
		t.Errorf("the next turn reached the MCP aggregation %d times", calls.Load())
	}
	list, call := mcpclient.ProxyNames(server.ID)
	direct := mcpclient.DirectToolName(server.ID, heatTool)
	modelRequests := model.snapshot()
	if len(modelRequests) != 5 {
		t.Fatalf("got %d manager requests", len(modelRequests))
	}
	for i, request := range modelRequests {
		visible := false
		for _, tool := range request.Tools {
			name := tool.Function.Name
			visible = visible || name == list || name == call || name == direct
		}
		if visible != (i >= 3) {
			t.Errorf("manager request %d exposed MCP = %v", i+1, visible)
		}
	}
}

// Plan and read-only turns cannot build a tool set with MCP bindings.
func TestTurnMCPAdmissionFollowsPlanAndReadOnly(t *testing.T) {
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, request scheduleCompletionRequest) {
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "done"}, "stop")
	})
	th := &store.Thread{}
	if !e.turnMountsMCP(th) || e.turnMountsMCP(&store.Thread{PlanMode: true}) || e.turnMountsMCP(nil) {
		t.Fatal("admission does not follow plan mode")
	}
}
