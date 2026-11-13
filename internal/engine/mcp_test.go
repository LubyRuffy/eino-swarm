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
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func checkMCPRouteGuidance(t *testing.T, description string) {
	t.Helper()
	for _, rule := range []string{"query this catalog", "The server title is not evidence", "unless the user specifies another route", "A past failure alone does not justify bypassing"} {
		if !strings.Contains(description, rule) {
			t.Errorf("MCP tool description is missing routing guidance %q", rule)
		}
	}
}

func engineMCPFixture(t *testing.T) (config.MCPServer, *atomic.Int32) {
	t.Helper()
	return engineMCPFixtureWithProgress(t, nil)
}

func engineMCPFixtureWithProgress(t *testing.T, beforeResult func(context.Context, *mcp.CallToolRequest) error) (config.MCPServer, *atomic.Int32) {
	t.Helper()
	calls := new(atomic.Int32)
	s := mcp.NewServer(&mcp.Implementation{Name: "engine-fixture", Version: "test"}, nil)
	s.AddTool(&mcp.Tool{Name: "inspect_record", Description: "Inspect external evidence", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"key": map[string]any{"type": "string", "description": "record identity"}}}}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calls.Add(1)
		if beforeResult != nil {
			if err := beforeResult(ctx, req); err != nil {
				return nil, err
			}
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "mcp-protocol-evidence"}}}, nil
	})
	h := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil))
	t.Cleanup(h.Close)
	return config.MCPServer{ID: "external-records", Name: "External records", Transport: "streamable_http", URL: h.URL, Enabled: true, TimeoutSeconds: 3}, calls
}

// The default manager and its worker execute MCP directly. A Nuwa manager only
// discovers the selectable definitions and delegates execution to its worker.
// Both paths cross Registry, ADK and the model HTTP adapter.
func TestMCPToolsReachManagerAndWorkerAtTheRealModelBoundary(t *testing.T) {
	for _, tc := range []struct {
		name string
		nuwa bool
	}{{"default", false}, {"nuwa-selected-model-and-tools", true}} {
		t.Run(tc.name, func(t *testing.T) {
			const managerModel, workerModel, workerProvider = "mcp-fixture", "mcp-selected-worker", "mcp-worker-fixture"
			const phase = "Checking the external record"
			const staleSkill = "earlier-service-workaround"
			const staleProcedure = "Use the shell because the connector timed out in an earlier turn."
			var selectedRef string
			var selectedTools atomic.Value // []string, published before the real spawn response.
			release := make(chan struct{}, 2)
			defer close(release)
			server, calls := engineMCPFixtureWithProgress(t, func(ctx context.Context, req *mcp.CallToolRequest) error {
				token := req.Params.GetProgressToken()
				if token == nil {
					return errors.New("model-driven call did not request progress")
				}
				if err := req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{ProgressToken: token, Progress: 1, Message: phase}); err != nil {
					return err
				}
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
			var managerSteps, workerSteps atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var wire struct {
					scheduleCompletionRequest
					Model string `json:"model"`
				}
				if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
					t.Error(err)
					http.Error(w, "invalid", 400)
					return
				}
				request := wire.scheduleCompletionRequest
				if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
					return
				}
				manager := false
				list, call := "", ""
				for _, offered := range request.Tools {
					manager = manager || offered.Function.Name == "spawn_agent"
					if strings.Contains(offered.Function.Name, "_list_tools_") {
						list = offered.Function.Name
						checkMCPRouteGuidance(t, offered.Function.Description)
					}
					if strings.Contains(offered.Function.Name, "_call_tool_") {
						call = offered.Function.Name
						checkMCPRouteGuidance(t, offered.Function.Description)
						if offered.Function.Parameters.Properties["arguments"].Description == "" {
							t.Error("proxy argument schema did not reach model")
						}
					}
				}
				if tc.nuwa {
					// Nuwa workers also receive spawn_agent for delegation; the selected
					// HTTP model, rather than that tool's presence, identifies this actor.
					manager = wire.Model == managerModel
					expectedPath := "/worker/v1/chat/completions"
					if manager {
						expectedPath = "/manager/v1/chat/completions"
					}
					if (!manager && wire.Model != workerModel) || r.URL.Path != expectedPath {
						t.Errorf("selected model route changed: model=%q path=%q", wire.Model, r.URL.Path)
					}
					if !manager {
						selected, _ := selectedTools.Load().([]string)
						if len(selected) != 2 || list != selected[0] || call != selected[1] {
							t.Errorf("selected MCP definitions did not reach the worker: list=%q call=%q selected=%v", list, call, selected)
						}
						for _, offered := range request.Tools {
							switch offered.Function.Name {
							case "read", "write", "exec", ToolQueryHistory:
								t.Errorf("worker gained an unselected workspace tool: %s", offered.Function.Name)
							}
						}
					} else {
						for _, offered := range request.Tools {
							if strings.HasPrefix(offered.Function.Name, "mcp_") {
								t.Errorf("Nuwa manager gained a direct MCP tool: %s", offered.Function.Name)
							}
						}
					}
				}

				answer := func(text string) { writeContextFlowReply(w, request.Stream, map[string]any{"content": text}, "stop") }
				var managerStep int32
				if manager {
					managerStep = managerSteps.Add(1)
				}
				if !tc.nuwa && manager {
					if managerStep == 1 {
						if !strings.Contains(request.Messages[0].Content, staleSkill) {
							t.Error("the manager did not see the earlier skill in its library index")
						}
						nuwaValidationTool(w, request, "earlier-skill", memory.ToolSkillView, `{"name":"`+staleSkill+`"}`)
						return
					}
					managerStep--
					if managerStep == 1 && !strings.Contains(nuwaValidationReceipt(request, "earlier-skill"), staleProcedure) {
						t.Error("the manager did not receive the saved shell fallback alongside the MCP tools")
					}
				}
				if tc.nuwa && manager {
					if managerStep == 1 {
						nuwaValidationTool(w, request, "manager-discover", "list_worker_tools", "{}")
						return
					}
					if managerStep == 2 {
						var discovery struct {
							Tools []struct{ Name, Description string } `json:"tools"`
						}
						if err := json.Unmarshal([]byte(nuwaValidationReceipt(request, "manager-discover")), &discovery); err != nil {
							t.Errorf("decode actual worker tool catalog: %v", err)
						}
						for _, offered := range discovery.Tools {
							if strings.Contains(offered.Name, "_list_tools_") {
								list = offered.Name
								checkMCPRouteGuidance(t, offered.Description)
							}
							if strings.Contains(offered.Name, "_call_tool_") {
								call = offered.Name
								checkMCPRouteGuidance(t, offered.Description)
							}
						}
					} else {
						selected, _ := selectedTools.Load().([]string)
						if len(selected) == 2 {
							list, call = selected[0], selected[1]
						}
					}
					// Discovery replaces the default manager's list/call pair; both
					// paths then spawn and collect the same actual worker flow.
					managerStep++
				}
				if list == "" || call == "" {
					if manager {
						t.Error("manager could not find the MCP proxy tools")
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
						nuwaValidationTool(w, request, "worker-list", list, `{"name":"inspect_record"}`)
					case 2:
						checkCatalog("worker-list")
						nuwaValidationTool(w, request, "worker-mcp", call, `{"name":"inspect_record","arguments":{"key":"record"}}`)
					default:
						if receipt := nuwaValidationReceipt(request, "worker-mcp"); !strings.Contains(receipt, "mcp-protocol-evidence") || strings.Contains(receipt, phase) {
							t.Error("worker did not receive real MCP result")
						}
						answer("External evidence is available.")
					}
					return
				}
				switch managerStep {
				case 1:
					nuwaValidationTool(w, request, "manager-list", list, `{"name":"inspect_record"}`)
				case 2:
					checkCatalog("manager-list")
					nuwaValidationTool(w, request, "manager-mcp", call, `{"name":"inspect_record","arguments":{"key":"record"}}`)
				case 3:
					if !tc.nuwa && !strings.Contains(nuwaValidationReceipt(request, "manager-mcp"), "mcp-protocol-evidence") {
						t.Error("manager did not receive real MCP result")
					}
					args := map[string]any{"role": "external-reader", "task": "Inspect the external record and report the evidence."}
					if tc.nuwa {
						args["model_ref"], args["tool_names"] = selectedRef, []string{list, call}
						selectedTools.Store([]string{list, call})
					}
					raw, err := json.Marshal(args)
					if err != nil {
						t.Error(err)
						return
					}
					nuwaValidationTool(w, request, "spawn-mcp", "spawn_agent", string(raw))
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
			e.cfg.Models.Providers[0].Model = managerModel
			if tc.nuwa {
				e.cfg.Models.Providers[0].BaseURL = endpoint.URL + "/manager/v1"
				e.cfg.Models.Providers = append(e.cfg.Models.Providers, config.Provider{ID: workerProvider, BaseURL: endpoint.URL + "/worker/v1", Model: workerModel})
				selectedRef = nuwaModelRef(workerProvider, workerModel)
			}
			e.cfg.MCP.DirectLoadPercent = 0
			e.cfg.MCP.Servers = []config.MCPServer{server}
			e.pool = provider.New(e.cfg)
			projectID := ""
			if tc.nuwa {
				project, err := e.CreateProject("External evidence with a selected worker", "", "", false, true)
				if err != nil {
					t.Fatal(err)
				}
				projectID = project.ID
			} else if _, err := e.LibraryMemory().WriteSkill(staleSkill, "Earlier route for external records", staleProcedure); err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("External records", "", projectID)
			if err != nil {
				t.Fatal(err)
			}
			sub := e.Subscribe(thread.ID)
			defer sub.Close()
			turn, err := e.StartTurn(thread.ID, "Inspect the external record and ask a worker to verify the result.")
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"manager-mcp", "worker-mcp"}
			if tc.nuwa {
				want = want[1:]
			}
			// The external server cannot send a final result until the real
			// registry/engine stream has delivered progress to the right call.
			timer := time.NewTimer(8 * time.Second)
			defer timer.Stop()
			for _, id := range want {
			awaitProgress:
				for {
					select {
					case ev := <-sub.C:
						if ev.ToolCallID != id {
							continue
						}
						if ev.Kind == "tool_result" {
							t.Fatalf("%s completed before progress: %s", id, ev.Text)
						}
						if ev.Kind == "tool_delta" && strings.Contains(ev.Text, phase) {
							if ev.TurnID != turn.ID || ev.Seq != 0 || ev.AgentID == "" || (ev.AgentID == "manager") != (id == "manager-mcp") {
								t.Fatalf("progress ownership: %+v", ev)
							}
							release <- struct{}{}
							break awaitProgress
						}
					case <-timer.C:
						t.Fatalf("%s never published progress", id)
					}
				}
			}
			finished := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			wantMCPCalls, wantManagerSteps := int32(2), int32(6)
			if tc.nuwa {
				wantMCPCalls, wantManagerSteps = 1, 4
			}
			if finished.Status != store.TurnDone || calls.Load() != wantMCPCalls || managerSteps.Load() != wantManagerSteps || workerSteps.Load() != 3 {
				t.Fatalf("MCP flow: status=%s calls=%d manager=%d worker=%d", finished.Status, calls.Load(), managerSteps.Load(), workerSteps.Load())
			}
			if tc.nuwa {
				if !finished.NuwaEnabled {
					t.Error("selected-worker case did not execute a Nuwa turn")
				}
				view, err := e.ProjectNuwaSnapshot(t.Context(), projectID)
				if err != nil || len(view.Workers) != 1 || view.Workers[0].Status != "done" || view.Workers[0].Generation != 1 || view.Workers[0].ModelBinding == nil || view.Workers[0].ModelBinding.ProviderID != workerProvider || view.Workers[0].ModelBinding.Model != workerModel || view.Workers[0].ModelBinding.Ref != provider.ChoiceID(workerProvider, workerModel) {
					t.Errorf("selected MCP worker lost its completed model binding: workers=%+v err=%v", view.Workers, err)
				}
			}
		})
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
		for _, msg := range request.Messages {
			if strings.Contains(msg.Content, "query this catalog") {
				t.Error("planning received routing guidance for an unmounted MCP tool")
			}
		}
		for _, offered := range request.Tools {
			if strings.HasPrefix(offered.Function.Name, "mcp_") || strings.Contains(offered.Function.Description, "query this catalog") {
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
