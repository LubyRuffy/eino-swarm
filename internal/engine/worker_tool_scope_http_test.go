package engine

import (
	"context"
	"errors"
	"net/http"
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
					discovery = discovery || definition.Function.Name == "list_worker_tools"
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
