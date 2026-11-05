package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func engineMCPFixture(t *testing.T) (config.MCPServer, *atomic.Int32) {
	t.Helper()
	calls := new(atomic.Int32)
	s := mcp.NewServer(&mcp.Implementation{Name: "engine-fixture", Version: "test"}, nil)
	s.AddTool(&mcp.Tool{Name: "inspect_record", Description: "Inspect external evidence", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"key": map[string]any{"type": "string", "description": "record identity"}}}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "mcp-protocol-evidence"}}}, nil
	})
	h := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil))
	t.Cleanup(h.Close)
	return config.MCPServer{ID: "external-records", Name: "External records", Transport: "streamable_http", URL: h.URL, Enabled: true, TimeoutSeconds: 3}, calls
}

// Both manager and worker must receive and execute the actual MCP definition
// through Registry, ADK and the model HTTP adapter, not a manual tool hook.
func TestMCPToolsReachManagerAndWorkerAtTheRealModelBoundary(t *testing.T) {
	server, calls := engineMCPFixture(t)
	var managerSteps, workerSteps atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid", 400)
			return
		}
		if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		manager := false
		list, call := "", ""
		for _, offered := range request.Tools {
			manager = manager || offered.Function.Name == "spawn_agent"
			if strings.Contains(offered.Function.Name, "_list_tools_") {
				list = offered.Function.Name
			}
			if strings.Contains(offered.Function.Name, "_call_tool_") {
				call = offered.Function.Name
				if offered.Function.Parameters.Properties["arguments"].Description == "" {
					t.Error("proxy argument schema did not reach model")
				}
			}
		}
		answer := func(text string) { writeContextFlowReply(w, request.Stream, map[string]any{"content": text}, "stop") }
		if list == "" || call == "" {
			if manager {
				t.Error("manager has no MCP proxy tools")
			}
			answer("The available evidence was inspected.")
			return
		}
		checkCatalog := func(id string) {
			receipt := nuwaValidationReceipt(request, id)
			if !strings.Contains(receipt, "record identity") || !strings.Contains(receipt, "input_schema") {
				t.Error("complete remote MCP schema did not reach model through discovery")
			}
		}
		if !manager {
			switch workerSteps.Add(1) {
			case 1:
				nuwaValidationTool(w, request, "worker-list", list, "{}")
			case 2:
				checkCatalog("worker-list")
				nuwaValidationTool(w, request, "worker-mcp", call, `{"name":"inspect_record","arguments":{"key":"record"}}`)
			default:
				if !strings.Contains(nuwaValidationReceipt(request, "worker-mcp"), "mcp-protocol-evidence") {
					t.Error("worker did not receive real MCP result")
				}
				answer("External evidence is available.")
			}
			return
		}
		switch managerSteps.Add(1) {
		case 1:
			nuwaValidationTool(w, request, "manager-list", list, "{}")
		case 2:
			checkCatalog("manager-list")
			nuwaValidationTool(w, request, "manager-mcp", call, `{"name":"inspect_record","arguments":{"key":"record"}}`)
		case 3:
			if !strings.Contains(nuwaValidationReceipt(request, "manager-mcp"), "mcp-protocol-evidence") {
				t.Error("manager did not receive real MCP result")
			}
			nuwaValidationTool(w, request, "spawn-mcp", "spawn_agent", `{"role":"external-reader","task":"Inspect the external record and report the evidence."}`)
		case 4:
			var spawned struct {
				AgentID string `json:"agent_id"`
			}
			if err := json.Unmarshal([]byte(nuwaValidationReceipt(request, "spawn-mcp")), &spawned); err != nil || spawned.AgentID == "" {
				t.Errorf("spawn result: %v", err)
			}
			raw, _ := json.Marshal(map[string]any{"agent_ids": []string{spawned.AgentID}, "timeout_s": 5})
			nuwaValidationTool(w, request, "wait-mcp", "wait_agents", string(raw))
		default:
			if !strings.Contains(nuwaValidationReceipt(request, "wait-mcp"), "External evidence is available.") {
				t.Error("manager did not collect worker evidence")
			}
			answer("The external record was inspected and its evidence collected.")
		}
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].Model = "mcp-fixture"
	e.cfg.MCP.Servers = []config.MCPServer{server}
	e.pool = provider.New(e.cfg)
	thread, err := e.CreateThread("External records", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Inspect the external record and ask a worker to verify the result.")
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, e, turn.ID)
	if finished.Status != store.TurnDone || calls.Load() != 2 || managerSteps.Load() != 5 || workerSteps.Load() != 3 {
		t.Fatalf("MCP flow: status=%s calls=%d manager=%d worker=%d", finished.Status, calls.Load(), managerSteps.Load(), workerSteps.Load())
	}
}

func TestPlanningMCPCommandFixture(t *testing.T) {
	if path := os.Getenv("ZWAI_MCP_PLANNING_RECEIPT"); path != "" {
		_ = os.WriteFile(path, []byte("command started"), 0o600)
		os.Exit(0)
	}
}

func TestPlanningAndReadOnlyRestorationDoNotStartOrAdvertiseMCP(t *testing.T) {
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, request scheduleCompletionRequest) {
		for _, offered := range request.Tools {
			if strings.HasPrefix(offered.Function.Name, "mcp_") {
				t.Error("planning exposed external MCP capability")
			}
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "The available read-only evidence was inspected."}, "stop")
	})
	receipt := filepath.Join(t.TempDir(), "unexpected-command")
	e.cfg.MCP.Servers = []config.MCPServer{{ID: "local", Transport: "stdio", Command: os.Args[0], Args: []string{"-test.run=^TestPlanningMCPCommandFixture$"}, Env: map[string]string{"ZWAI_MCP_PLANNING_RECEIPT": receipt}, Enabled: true, TimeoutSeconds: 1}}
	thread, err := e.CreateThread("Planning external evidence", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetPlanMode(thread.ID, true); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Inspect available evidence and propose work.")
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("planning turn %s", got.Status)
	}
	thread, err = e.Store().GetThread(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A restored read-only Nuwa turn may outlive the conversation's plan flag.
	thread.PlanMode = false
	set, err := e.conversationToolsForExecution(t.Context(), thread, config.WebExecutionAuto, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range set.Names {
		if strings.HasPrefix(name, "mcp_") {
			t.Fatal("read-only restoration mounted MCP")
		}
	}
	if _, err := os.Stat(receipt); !os.IsNotExist(err) {
		t.Fatalf("planning started a stdio command: %v", err)
	}
}

func TestMCPInspectionAndConfigReloadKeepTheRuntimeUsable(t *testing.T) {
	server, _ := engineMCPFixture(t)
	e := newTestEngine(t)
	if err := e.UpdateConfig(func(cfg *config.Config) error { cfg.MCP.Servers = []config.MCPServer{server}; return nil }); err != nil {
		t.Fatal(err)
	}
	catalog, err := e.InspectMCP(t.Context(), server)
	if err != nil || len(catalog.Tools) != 1 {
		t.Fatalf("runtime inspect %+v %v", catalog, err)
	}
	thread, err := e.CreateThread("MCP configuration", "", "")
	if err != nil {
		t.Fatal(err)
	}
	set, err := e.conversationTools(t.Context(), thread)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Names) == 0 || !strings.Contains(strings.Join(set.Names, " "), "mcp_") {
		t.Fatal("runtime did not mount configured MCP")
	}
	if err := e.UpdateConfig(func(cfg *config.Config) error { cfg.MCP.Servers = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	set, err = e.conversationTools(t.Context(), thread)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(set.Names, " "), "mcp_") {
		t.Fatal("disabled MCP still mounted")
	}
}

func TestUnresponsiveMCPDiscoveryDoesNotBlockTurnAdmissionOrHumanStop(t *testing.T) {
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer remote.Close()
	defer close(release)
	e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, request scheduleCompletionRequest) {
		for _, offered := range request.Tools {
			if strings.Contains(offered.Function.Name, "_list_tools_") {
				nuwaValidationTool(w, request, "blocked-mcp-list", offered.Function.Name, "{}")
				return
			}
		}
		t.Error("configured MCP discovery proxy is missing")
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "No external tool is available."}, "stop")
	})
	e.cfg.MCP.Servers = []config.MCPServer{{ID: "unresponsive", Transport: "streamable_http", URL: remote.URL, Enabled: true, TimeoutSeconds: 60}}
	thread, err := e.CreateThread("Interrupt external discovery", "", "")
	if err != nil {
		t.Fatal(err)
	}
	type admitted struct {
		turn *store.Turn
		err  error
	}
	admission := make(chan admitted, 1)
	go func() {
		turn, err := e.StartTurn(thread.ID, "Discover the configured external tool catalogue.")
		admission <- admitted{turn, err}
	}()
	var turn *store.Turn
	select {
	case result := <-admission:
		if result.err != nil {
			t.Fatal(result.err)
		}
		turn = result.turn
	case <-time.After(5 * time.Second):
		t.Fatal("MCP discovery held turn admission before a cancellable runtime existed")
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("actual running tool did not attempt MCP handshake")
	}
	if status := e.Status(thread.ID); !status.Running {
		t.Fatal("external discovery has no running turn for Stop")
	}
	stopped := make(chan error, 1)
	go func() { stopped <- e.Interrupt(thread.ID) }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("human Stop waited for the external MCP timeout")
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnCancelled {
		t.Fatalf("Stop changed external discovery into %s", got.Status)
	}
}
