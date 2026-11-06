package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/components/tool"
)

// The planner's model input and its executor must share the same boundary.
// Exercise real workspace and MCP operations on the selected worker, then
// collect its shared artifact through the manager's coordination tools.
func TestNuwaPlannerDelegatesExecutionToolsAtModelHTTP(t *testing.T) {
	const account, planner, executor = "separation-fixture", "planning-model", "execution-model"
	const input, artifact = "assigned-input.txt", "worker-artifact.txt"
	const task = "Inspect the assigned input and external evidence, then record the resulting artifact."
	value := store.NewID("source_")
	server, remoteCalls := engineMCPFixture(t)
	var managerCalls, workerCalls atomic.Int32
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var wire struct {
			scheduleCompletionRequest
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture input", http.StatusBadRequest)
			return
		}
		request := wire.scheduleCompletionRequest
		if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		answer := func(text string) { writeContextFlowReply(w, request.Stream, map[string]any{"content": text}, "stop") }
		call := func(id, name string, args any) {
			raw, err := json.Marshal(args)
			if err != nil {
				t.Error(err)
				http.Error(w, "invalid tool arguments", http.StatusBadRequest)
				return
			}
			nuwaValidationTool(w, request, id, name, string(raw))
		}
		available := nuwaPlannerOfferedTools(request)
		if wire.Model == executor {
			for _, message := range request.Messages {
				if strings.Contains(message.Content, nuwaPlannerInstruction) {
					t.Errorf("forked executor inherited the planner-only instruction in %s input", message.Role)
				}
			}
			list, remote := nuwaPlannerMCPNames(available)
			for _, name := range []string{"read", "write", "spawn_agent", "wait_agents", "list_models", "read_work", "record_work"} {
				if !available[name] {
					t.Errorf("selected worker lost %s at the actual model boundary", name)
				}
			}
			for _, descriptor := range tools.Catalog() {
				if descriptor.Name != "read" && descriptor.Name != "write" && available[descriptor.Name] {
					t.Errorf("selected worker gained unselected execution tool %q", descriptor.Name)
				}
			}
			if list == "" || remote == "" || !strings.Contains(joinedContent(request.Messages), task) {
				t.Error("selected worker lost its MCP scope or unchanged task")
				answer("The assigned tools were unavailable.")
				return
			}
			switch workerCalls.Add(1) {
			case 1:
				call("separation-worker-read", "read", map[string]string{"file_path": input})
			case 2:
				if !strings.Contains(nuwaValidationReceipt(request, "separation-worker-read"), value) {
					t.Error("worker did not receive actual workspace bytes")
				}
				call("separation-worker-write", "write", map[string]string{"file_path": artifact, "content": value})
			case 3:
				if receipt := nuwaValidationReceipt(request, "separation-worker-write"); receipt == "" || strings.Contains(receipt, `"error"`) {
					t.Errorf("worker did not receive a successful write receipt: %s", receipt)
				}
				call("separation-worker-list", list, struct{}{})
			case 4:
				if !strings.Contains(nuwaValidationReceipt(request, "separation-worker-list"), "input_schema") {
					t.Error("worker did not receive the remote MCP schema")
				}
				call("separation-worker-mcp", remote, map[string]any{"name": "inspect_record", "arguments": map[string]string{"key": "assigned-record"}})
			case 5:
				if !strings.Contains(nuwaValidationReceipt(request, "separation-worker-mcp"), "mcp-protocol-evidence") {
					t.Error("worker did not receive actual MCP evidence")
				}
				call("separation-worker-record", "record_work", map[string]any{
					"kind": "artifact", "title": "Worker deliverable", "summary": value,
					"artifact_path": artifact, "evidence": []any{},
				})
			case 6:
				var receipt struct {
					OK   bool           `json:"ok"`
					Node store.NuwaNode `json:"node"`
				}
				if err := json.Unmarshal([]byte(nuwaValidationReceipt(request, "separation-worker-record")), &receipt); err != nil || !receipt.OK || receipt.Node.ID == "" || receipt.Node.AgentID == swarm.DefaultManagerID {
					t.Errorf("worker did not record an actual shared artifact: %+v err=%v", receipt, err)
				}
				answer(receipt.Node.ID)
			default:
				t.Error("unexpected worker request")
				answer("The worker request was unexpected.")
			}
			return
		}
		if wire.Model != planner {
			t.Errorf("unexpected model route %q", wire.Model)
		}
		assertNuwaPlannerCoordinationSurface(t, request)
		switch managerCalls.Add(1) {
		case 1:
			call("separation-discover", "list_worker_tools", struct{}{})
		case 2:
			call("separation-models", "list_models", struct{}{})
		case 3:
			var discovery struct {
				Tools []struct{ Name string } `json:"tools"`
			}
			if err := json.Unmarshal([]byte(nuwaValidationReceipt(request, "separation-discover")), &discovery); err != nil {
				t.Error(err)
			}
			selectable := make(map[string]bool, len(discovery.Tools))
			for _, candidate := range discovery.Tools {
				selectable[candidate.Name] = true
			}
			list, remote := nuwaPlannerMCPNames(selectable)
			if !selectable["read"] || !selectable["write"] || list == "" || remote == "" {
				t.Errorf("planner isolation damaged worker tool discovery: %v", selectable)
				answer("The worker scope was unavailable.")
				return
			}
			var catalog struct {
				Models []nuwaModel `json:"models"`
			}
			if err := json.Unmarshal([]byte(nuwaValidationReceipt(request, "separation-models")), &catalog); err != nil {
				t.Error(err)
			}
			for _, option := range catalog.Models {
				if option.ProviderID == account && option.Model == executor {
					call("separation-spawn", "spawn_agent", map[string]any{
						"role": "evidence-executor", "task": task, "model_ref": option.Ref,
						"fork_context": true,
						"tool_names":   []string{"read", "write", list, remote},
					})
					return
				}
			}
			t.Error("selected executor was absent from the actual model catalog")
			answer("The model selection was unavailable.")
		case 4:
			var spawned toolScopeWorkerID
			if err := json.Unmarshal([]byte(nuwaValidationReceipt(request, "separation-spawn")), &spawned); err != nil || spawned.AgentID == "" || spawned.Generation != 1 {
				t.Errorf("worker spawn failed: %+v err=%v", spawned, err)
				answer("Delegation failed.")
				return
			}
			call("separation-wait", "wait_agents", map[string]any{"agent_ids": []string{spawned.AgentID}, "timeout_s": 5})
		case 5:
			var collected toolScopeWait
			if err := json.Unmarshal([]byte(nuwaValidationReceipt(request, "separation-wait")), &collected); err != nil || collected.TimedOut || len(collected.Agents) != 1 || collected.Agents[0].Status != "done" || collected.Agents[0].Result == "" {
				t.Errorf("manager did not collect actual worker completion: %+v err=%v", collected, err)
				answer("Collection failed.")
				return
			}
			call("separation-review", "read_work", map[string]string{"id": collected.Agents[0].Result})
		case 6:
			if !strings.Contains(nuwaValidationReceipt(request, "separation-review"), value) {
				t.Error("manager did not inspect the worker's shared result")
			}
			answer("The worker deliverable was collected and reviewed.")
		default:
			t.Error("unexpected manager request")
			answer("The manager request was unexpected.")
		}
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Models = config.ModelsConfig{Default: account, Providers: []config.Provider{{ID: account, BaseURL: endpoint.URL, Model: planner, Catalog: []string{planner, executor}}}}
	e.cfg.MCP.Servers = []config.MCPServer{server}
	e.pool = provider.New(e.cfg)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, input), []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := e.CreateProject("Separated planning and execution", "", workspace, false, true)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Delegated execution", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Delegate the work and review the resulting evidence.")
	if err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if finished.Status != store.TurnDone || managerCalls.Load() != 6 || workerCalls.Load() != 6 || remoteCalls.Load() != 1 {
		t.Fatalf("delegated execution: turn=%+v manager=%d worker=%d remote=%d", finished, managerCalls.Load(), workerCalls.Load(), remoteCalls.Load())
	}
	bytes, err := os.ReadFile(filepath.Join(workspace, artifact))
	if err != nil || string(bytes) != value {
		t.Fatalf("selected worker did not create the actual artifact: bytes=%q err=%v", bytes, err)
	}
	view, err := e.ProjectNuwaSnapshot(t.Context(), project.ID)
	if err != nil || len(view.Workers) != 1 || len(view.Nodes) != 1 {
		t.Fatalf("missing durable worker result: %+v err=%v", view, err)
	}
	worker, node := view.Workers[0], view.Nodes[0]
	if worker.Status != "done" || worker.ModelBinding == nil || worker.ModelBinding.Ref != provider.ChoiceID(account, executor) || node.AgentID != worker.AgentID || node.Generation != worker.Generation || node.ArtifactPath != artifact {
		t.Fatalf("artifact lost worker execution identity: worker=%+v node=%+v", worker, node)
	}
}

// Omitting a schema is insufficient if an unadvertised call can still reach
// the manager's executor. The ordinary-mode controls prove these exact calls
// are otherwise valid and have observable effects.
func TestNuwaPlannerCannotExecuteModelAuthoredWorkAtHTTP(t *testing.T) {
	for _, nuwa := range []bool{true, false} {
		for _, operation := range []string{"workspace write", "MCP call"} {
			t.Run(fmt.Sprintf("nuwa=%v/%s", nuwa, operation), func(t *testing.T) {
				server, remoteCalls := engineMCPFixture(t)
				var requests atomic.Int32
				var executionName, executionArgs string
				endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var request scheduleCompletionRequest
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
						http.Error(w, "invalid fixture input", http.StatusBadRequest)
						return
					}
					if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
						return
					}
					if advertised := nuwaPlannerOfferedTools(request)[executionName]; advertised == nuwa {
						t.Errorf("execution tool %s advertised=%v for Nuwa=%v", executionName, advertised, nuwa)
					}
					if requests.Add(1) == 1 {
						nuwaValidationTool(w, request, "separation-forged", executionName, executionArgs)
						return
					}
					if !nuwa {
						receipt := nuwaValidationReceipt(request, "separation-forged")
						if receipt == "" || strings.Contains(receipt, `"error"`) || operation == "MCP call" && !strings.Contains(receipt, "mcp-protocol-evidence") {
							t.Errorf("ordinary manager did not receive its execution result: %s", receipt)
						}
					}
					writeContextFlowReply(w, request.Stream, map[string]any{"content": "The operation result was inspected."}, "stop")
				}))
				t.Cleanup(endpoint.Close)
				e := newTestEngine(t)
				e.cfg.Swarm.AutoTitle = false
				e.cfg.Models.Providers[0].BaseURL = endpoint.URL
				e.cfg.Models.Providers[0].Model = "separation-control"
				e.cfg.MCP.Servers = []config.MCPServer{server}
				e.pool = provider.New(e.cfg)
				workspace := t.TempDir()
				project, err := e.CreateProject("Execution boundary", "", workspace, false, nuwa)
				if err != nil {
					t.Fatal(err)
				}
				thread, err := e.CreateThread("Execution admission", "", project.ID)
				if err != nil {
					t.Fatal(err)
				}
				executionName, executionArgs = "write", `{"file_path":"manager-write.txt","content":"manager-write-observed"}`
				if operation == "MCP call" {
					set, err := e.conversationTools(t.Context(), thread)
					if err != nil {
						t.Fatal(err)
					}
					names := make(map[string]bool, len(set.Names))
					for _, name := range set.Names {
						names[name] = true
					}
					_, executionName = nuwaPlannerMCPNames(names)
					if executionName == "" {
						t.Fatal("configured MCP execution control was not mounted")
					}
					executionArgs = `{"name":"inspect_record","arguments":{"key":"assigned-record"}}`
				}
				turn, err := e.StartTurn(thread.ID, "Inspect the available operation result.")
				if err != nil {
					t.Fatal(err)
				}
				finished := waitForTurn(t, e, turn.ID)
				waitSettled(t, e, thread.ID)
				body, readErr := os.ReadFile(filepath.Join(workspace, "manager-write.txt"))
				if nuwa {
					if finished.Status != store.TurnError || finished.Error == "" || requests.Load() != 1 || !os.IsNotExist(readErr) || remoteCalls.Load() != 0 {
						t.Fatalf("planner executed or silently accepted a forbidden call: turn=%+v requests=%d file=%q file_error=%v remote=%d", finished, requests.Load(), body, readErr, remoteCalls.Load())
					}
					return
				}
				if finished.Status != store.TurnDone || requests.Load() != 2 {
					t.Fatalf("ordinary-mode execution changed: turn=%+v requests=%d", finished, requests.Load())
				}
				if operation == "workspace write" {
					if readErr != nil || string(body) != "manager-write-observed" || remoteCalls.Load() != 0 {
						t.Fatalf("ordinary workspace control did not execute: bytes=%q err=%v remote=%d", body, readErr, remoteCalls.Load())
					}
				} else if remoteCalls.Load() != 1 || !os.IsNotExist(readErr) {
					t.Fatalf("ordinary MCP control did not execute: remote=%d file_error=%v", remoteCalls.Load(), readErr)
				}
			})
		}
	}
}

// Tool names are model-visible labels, not authority to retain a workspace
// tool on the planner. Preserve only the actual engine-owned history reader
// and project controls, without sharing their mutable slice backing arrays.
func TestNuwaPlannerToolsKeepTrustedHistoryAndProjectControls(t *testing.T) {
	history := &historyQueryTool{}
	fakeHistory := &nuwaTool{name: ToolQueryHistory}
	write := &nuwaTool{name: "write"}
	memory := &nuwaTool{name: "memory"}
	skills := &nuwaTool{name: "skill_manage"}
	setSentinel, projectSentinel := &nuwaTool{name: "set-tail"}, &nuwaTool{name: "project-tail"}
	setBacking := []tool.BaseTool{write, fakeHistory, nil, history, setSentinel}
	projectBacking := []tool.BaseTool{memory, skills, projectSentinel}
	set := &tools.Set{Tools: setBacking[:4]}
	pc := &projectContext{tools: projectBacking[:2]}
	for _, tc := range []struct {
		name string
		set  *tools.Set
		pc   *projectContext
		want []tool.BaseTool
	}{
		{name: "nil inputs"},
		{name: "empty inputs", set: &tools.Set{}, pc: &projectContext{}},
		{name: "history without project", set: set, want: []tool.BaseTool{history}},
		{name: "project without workspace", pc: pc, want: []tool.BaseTool{memory, skills}},
		{name: "history and project controls", set: set, pc: pc, want: []tool.BaseTool{history, memory, skills}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setBefore := append([]tool.BaseTool(nil), setBacking...)
			projectBefore := append([]tool.BaseTool(nil), projectBacking...)
			got := nuwaPlannerTools(tc.set, tc.pc)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("planner retained execution tools or lost trusted controls: got=%v want=%v", got, tc.want)
			}
			got = append(got, &nuwaTool{name: "later-manager-control"})
			for i := range got {
				got[i] = nil
			}
			if !slices.Equal(setBacking, setBefore) || !slices.Equal(projectBacking, projectBefore) {
				t.Fatal("appending or changing manager tools modified workspace or project tool backing storage")
			}
		})
	}
}

func nuwaPlannerOfferedTools(request scheduleCompletionRequest) map[string]bool {
	available := make(map[string]bool, len(request.Tools))
	for _, offered := range request.Tools {
		available[offered.Function.Name] = true
	}
	return available
}

func nuwaPlannerMCPNames(available map[string]bool) (list, call string) {
	for name := range available {
		if strings.HasPrefix(name, "mcp_") && strings.Contains(name, "_list_tools_") {
			list = name
		}
		if strings.HasPrefix(name, "mcp_") && strings.Contains(name, "_call_tool_") {
			call = name
		}
	}
	return list, call
}

func assertNuwaPlannerCoordinationSurface(t *testing.T, request scheduleCompletionRequest) {
	t.Helper()
	var system strings.Builder
	for _, message := range request.Messages {
		if message.Role == "system" {
			system.WriteString(message.Content)
			system.WriteByte('\n')
		}
	}
	prompt := strings.Join(strings.Fields(system.String()), " ")
	for _, obsolete := range []string{"Otherwise do the work yourself.", "Besides the delegation tools you have:", "Read those first."} {
		if strings.Contains(prompt, obsolete) {
			t.Errorf("actual Nuwa System still instructs direct manager execution: %q", obsolete)
		}
	}
	for _, required := range []string{
		"Task operations must be delegated to executors through spawn_agent.",
		"Workspace and external-service tools are available to executors, not to you.",
		"A purely conversational response that needs no task operation does not require spawning a worker.",
	} {
		if !strings.Contains(prompt, required) {
			t.Errorf("actual Nuwa System is missing planner execution boundary: %q", required)
		}
	}
	available := nuwaPlannerOfferedTools(request)
	for _, name := range []string{"spawn_agent", "wait_agents", "list_worker_tools", "list_models", "read_work", "record_work"} {
		if !available[name] {
			t.Errorf("planner lost coordination tool %s at the actual model boundary", name)
		}
	}
	for _, descriptor := range tools.Catalog() {
		if available[descriptor.Name] {
			t.Errorf("Nuwa planner received execution tool %q", descriptor.Name)
		}
	}
	for name := range available {
		if strings.HasPrefix(name, "mcp_") {
			t.Errorf("Nuwa planner received external execution tool %q", name)
		}
	}
}
