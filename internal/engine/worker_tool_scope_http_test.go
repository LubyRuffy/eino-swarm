package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type workerToolScopeInfoFixture struct {
	info *schema.ToolInfo
	err  error
}

func (fixture workerToolScopeInfoFixture) Info(context.Context) (*schema.ToolInfo, error) {
	return fixture.info, fixture.err
}

func TestWorkerToolScopeHostMetadataFailureDisablesSelection(t *testing.T) {
	if names := selectableWorkerToolNames(nil); names == nil || len(names) != 0 {
		t.Fatalf("empty host surface must remain an enabled empty list: %v", names)
	}
	for _, tc := range []struct {
		name string
		tool tool.BaseTool
	}{{"missing tool", nil}, {"metadata error", workerToolScopeInfoFixture{err: errors.New("unavailable")}},
		{"missing metadata", workerToolScopeInfoFixture{}}, {"unnamed tool", workerToolScopeInfoFixture{info: &schema.ToolInfo{}}}} {
		t.Run(tc.name, func(t *testing.T) {
			known := workerToolScopeInfoFixture{info: &schema.ToolInfo{Name: "read"}}
			if names := selectableWorkerToolNames([]tool.BaseTool{known, tc.tool}); names != nil {
				t.Fatalf("unknown workspace tool must disable selection, got %v", names)
			}
		})
	}
}

func TestWorkerToolScopeDiscoveryIsNuwaOnlyAtModelHTTP(t *testing.T) {
	for _, mode := range []struct {
		name string
		nuwa bool
	}{{"default", false}, {"nuwa", true}} {
		t.Run(mode.name, func(t *testing.T) {
			e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, request scheduleCompletionRequest) {
				discovery := false
				for _, definition := range request.Tools {
					switch definition.Function.Name {
					case "list_worker_tools":
						discovery = true
						assertWorkspaceSelectionDescription(t, definition.Function.Description)
					case "spawn_agent":
						if mode.nuwa {
							assertWorkspaceSelectionDescription(t, definition.Function.Parameters.Properties["tool_names"].Description)
						}
					}
				}
				if discovery != mode.nuwa {
					t.Errorf("list_worker_tools advertised=%v, want %v", discovery, mode.nuwa)
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The available work surface was inspected."}, "stop")
			})
			project, err := e.CreateProject("Tool scope discovery", "", t.TempDir(), false, mode.nuwa)
			if err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("Inspect tool discovery", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(thread.ID, "Inspect the available work surface.")
			if err != nil {
				t.Fatal(err)
			}
			result := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			if result.Status != store.TurnDone {
				t.Fatalf("turn did not finish: %+v", result)
			}
		})
	}
}

func assertWorkspaceSelectionDescription(t *testing.T, description string) {
	t.Helper()
	text := strings.ToLower(description)
	if !strings.Contains(text, "workspace") ||
		!(strings.Contains(text, "already") || strings.Contains(text, "automatic")) {
		t.Errorf("selection description must distinguish workspace tools from tools workers already receive: %q", description)
	}
}

func TestNuwaSpawnToolFeedbackPreservesWorkerTaskAtModelHTTP(t *testing.T) {
	for _, tc := range []struct {
		name       string
		tool       string
		planning   bool
		canCorrect bool
	}{
		{name: "mounted receipt tool", tool: "record_work", canCorrect: true},
		{name: "unknown name", tool: "no_such_workspace_tool"},
		{name: "receipt tool unavailable while planning", tool: "record_work", planning: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const task = "Read the source, then use record_work to cite the exact source receipt."
			const rejectedID = "selection-rejected"
			const correctedID = "selection-corrected"
			const waitID = "selection-collected"
			var workerRequests atomic.Int32
			e, _ := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, _ int, request scheduleCompletionRequest) {
				manager := len(request.Messages) != 0 && strings.Contains(request.Messages[0].Content, "You are the manager of a team")
				if !manager {
					workerRequests.Add(1)
					if !strings.Contains(joinedContent(request.Messages), task) {
						t.Error("worker did not receive the unchanged evidence instructions")
					}
					available := map[string]bool{}
					for _, definition := range request.Tools {
						available[definition.Function.Name] = true
					}
					if !available["record_work"] || !available["read"] || available["write"] {
						t.Errorf("corrected selection must retain the receipt tool and narrow workspace tools: %v", available)
					}
					assertNuwaRecordWorkSchema(t, request, "worker")
					writeContextFlowReply(w, request.Stream, map[string]any{"content": "The source receipt was inspected."}, "stop")
					return
				}

				rejected := nuwaValidationReceipt(request, rejectedID)
				if rejected == "" {
					args := fmt.Sprintf(`{"role":"source-observer","task":%q,"tool_names":["read",%q]}`, task, tc.tool)
					nuwaValidationTool(w, request, rejectedID, "spawn_agent", args)
					return
				}
				corrected := nuwaValidationReceipt(request, correctedID)
				if corrected == "" {
					var refusal struct {
						Error   string
						AgentID string `json:"agent_id"`
					}
					if err := json.Unmarshal([]byte(rejected), &refusal); err != nil || refusal.Error == "" || refusal.AgentID != "" {
						t.Errorf("invalid selection must return a typed refusal without a worker ID: %q (%v)", rejected, err)
					}
					if tc.canCorrect {
						for _, text := range []string{tc.tool, "already available", "omit it from tool_names", "keep instructions for using it in task"} {
							if !strings.Contains(strings.ToLower(refusal.Error), text) {
								t.Errorf("mounted-tool refusal %q must contain %q", refusal.Error, text)
							}
						}
						args := fmt.Sprintf(`{"role":"source-observer","task":%q,"tool_names":["read"]}`, task)
						nuwaValidationTool(w, request, correctedID, "spawn_agent", args)
						return
					}
					want := fmt.Sprintf("spawn_agent: unknown or unavailable worker tool %q; use list_worker_tools", tc.tool)
					if refusal.Error != want {
						t.Errorf("unknown or unavailable tool refusal = %q, want %q", refusal.Error, want)
					}
				} else if nuwaValidationReceipt(request, waitID) == "" {
					var worker toolScopeWorkerID
					if err := json.Unmarshal([]byte(corrected), &worker); err != nil || worker.AgentID == "" || worker.Generation != 1 {
						t.Errorf("corrected selection did not spawn a fresh worker: %q (%v)", corrected, err)
					} else {
						nuwaValidationTool(w, request, waitID, "wait_agents", fmt.Sprintf(`{"agent_ids":[%q],"timeout_s":5}`, worker.AgentID))
						return
					}
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The worker selection was handled."}, "stop")
			})
			project, err := e.CreateProject("Tool scope feedback", "", t.TempDir(), false, true)
			if err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("Inspect tool scope feedback", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tc.planning {
				if err := e.SetPlanMode(thread.ID, true); err != nil {
					t.Fatal(err)
				}
			}
			turn, err := e.StartTurn(thread.ID, "Use the available project work surface.")
			if err != nil {
				t.Fatal(err)
			}
			result := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			if result.Status != store.TurnDone {
				t.Fatalf("typed refusal interrupted the manager: %+v", result)
			}
			want := int32(0)
			if tc.canCorrect {
				want = 1
			}
			if got := workerRequests.Load(); got != want {
				t.Fatalf("actual worker model calls = %d, want %d after selection handling", got, want)
			}
		})
	}
}
