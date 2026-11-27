package engine

import (
	"context"
	"encoding/json"
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
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// An account can become restricted after this turn froze its MCP binding.
// Source admission must read that policy again before doing external work.
func TestEASMSourceRechecksAccountRestrictionAfterTheTurnMountedMCP(t *testing.T) {
	const serverID, heatTool = "source-admission", "source_heat"
	const openCall, collectCall = "source-open", "source-collect"
	var requests, toolCalls atomic.Int32
	remote := mcp.NewServer(&mcp.Implementation{Name: serverID, Version: "test"}, nil)
	remote.AddTool(&mcp.Tool{Name: heatTool, InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		toolCalls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: `{"hot_trend":[]}`}}}, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return remote }, nil)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(endpoint.Close)
	var e *Engine
	var panelID string
	e, model := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			easmToolCall(w, request, openCall, "open_task_panel", `{"plugin_id":"easm-asset-portrait"}`)
		case 2:
			// This actual model request follows the successful open tool. Its
			// MCP surface and the durable mount exist before the policy changes.
			_, call := mcpclient.ProxyNames(serverID)
			if !requestHasTool(request, call) {
				t.Error("the unrestricted turn never mounted its MCP call proxy")
			}
			var panel taskpanel.Panel
			if err := json.Unmarshal([]byte(scriptedToolReceipt(request, openCall)), &panel); err != nil || panel.ID == "" {
				t.Errorf("open receipt did not identify the panel: %v", err)
			}
			panelID = panel.ID
			base := e.cfg.Snapshot().Models.Providers[0].BaseURL
			restricted, err := provider.NewAccountProfile(accountCatalogID, base, "fixture-token", provider.AccountProfile{
				Label: "Admission fixture", Tools: []string{"read"}, RestrictTools: true,
			})
			if err != nil {
				t.Error(err)
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "Fixture configuration failed."}, "stop")
				return
			}
			e.pool.SetAccountSource(restricted)
			raw, _ := json.Marshal(map[string]any{"panel_id": panel.ID, "input": map[string]any{
				"subject": "Admission subject", "query": easmFixtureQuery,
			}})
			easmToolCall(w, request, collectCall, "run_panel_source", string(raw))
		default:
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The panel records the unavailable aggregation."}, "stop")
		}
	})
	if err := e.UpdateConfig(func(c *config.Config) error {
		c.Product.Mode = ""
		c.MCP.DirectLoadPercent = 0
		c.MCP.Servers = []config.MCPServer{{ID: serverID, Transport: "streamable_http", URL: endpoint.URL, Enabled: true, TimeoutSeconds: 3}}
		c.TaskPanels.EASM.ServerID = serverID
		c.TaskPanels.EASM.Tools = map[string]string{"stats": "", "heat": heatTool, "product_rules": ""}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	unrestricted, err := provider.NewAccountProfile(accountCatalogID, e.cfg.Snapshot().Models.Providers[0].BaseURL, "fixture-token", provider.AccountProfile{Label: "Admission fixture"})
	if err != nil {
		t.Fatal(err)
	}
	e.pool.SetAccountSource(unrestricted)
	thread, err := e.CreateThread("Source admission fixture", unrestricted.ID(), "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Collect the supplied portrait query.")
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("turn=%+v", got)
	}
	waitSettled(t, e, thread.ID)
	if requests.Load() != 0 || toolCalls.Load() != 0 {
		t.Fatalf("a source admitted before the account restriction reached MCP: HTTP=%d calls=%d", requests.Load(), toolCalls.Load())
	}
	mount, ok := e.loadMCPMount(turn.ID)
	if !ok || mount.PanelTools[easm.RoleHeat].Name != heatTool || mount.PanelTools[easm.RoleHeat].ServerID != serverID {
		t.Fatalf("the turn did not retain its original admitted binding: %+v", mount.PanelTools)
	}
	panel, err := e.store.TaskPanel(t.Context(), thread.ID, panelID)
	if err != nil {
		t.Fatal(err)
	}
	var state easm.State
	if err := json.Unmarshal(panel.State, &state); err != nil {
		t.Fatal(err)
	}
	if panel.SourceStatus != "finished" || !slices.ContainsFunc(state.Collection.Steps, func(step easm.StepStatus) bool {
		return step.ID == easm.StepHeat && step.Status == "unavailable"
	}) || len(model.snapshot()) != 3 {
		t.Fatalf("source status=%s collection=%+v model calls=%d", panel.SourceStatus, state.Collection, len(model.snapshot()))
	}
}
