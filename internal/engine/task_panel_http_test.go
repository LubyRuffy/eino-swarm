package engine

import (
	"encoding/json"
	"fmt"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"net/http"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/taskpanel"
)

// The actual Registry and HTTP model choose a plugin and publish state. This
// catches missing tool wiring and missing runtime owner injection together.
func TestTaskPanelUpdatesThroughActualManagerHTTP(t *testing.T) {
	var panelID string
	e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			for _, name := range []string{"open_task_panel", "read_panel_state", "publish_panel_update", "run_panel_source"} {
				found := false
				for _, tool := range request.Tools {
					found = found || tool.Function.Name == name
				}
				if !found {
					t.Errorf("missing %s", name)
				}
			}
			writeScheduleCompletionTool(w, request, "open_task_panel", `{"plugin_id":"fofa-world-map"}`)
		case 2:
			var panel taskpanel.Panel
			if err := json.Unmarshal([]byte(request.Messages[len(request.Messages)-1].Content), &panel); err != nil || panel.ID == "" {
				t.Errorf("open receipt=%s err=%v", request.Messages[len(request.Messages)-1].Content, err)
			}
			panelID = panel.ID
			writeScheduleCompletionTool(w, request, "publish_panel_update", fmt.Sprintf(`{"protocol_version":1,"panel_id":%q,"operation_id":"observed-change","base_revision":%d,"kind":"patch","operations":[{"op":"replace","path":"/collection/phase","value":"finished"}]}`, panel.ID, panel.Revision))
		case 3:
			writeScheduleCompletionTool(w, request, "read_panel_state", fmt.Sprintf(`{"panel_id":%q}`, panelID))
		default:
			if !strings.Contains(request.Messages[len(request.Messages)-1].Content, `"phase":"finished"`) {
				t.Error("published state did not return to actual model")
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The observations are available in the panel."}, "stop")
		}
	})
	thread, err := e.CreateThread("Panel boundary fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Inspect the supplied structured observations.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || len(fixture.snapshot()) != 4 {
		t.Fatalf("turn=%+v calls=%d", got, len(fixture.snapshot()))
	}
	panel, err := e.store.TaskPanel(t.Context(), thread.ID, panelID)
	if err != nil || panel.Revision != 2 || panel.Source.AgentID != "manager" || panel.Source.ToolCallID == "" {
		t.Fatalf("panel=%+v err=%v", panel, err)
	}
}

func TestTaskPanelPlanningAndDisabledSurfaces(t *testing.T) {
	e := newTestEngine(t)
	thread, err := e.CreateThread("Panel planning", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: thread.ID}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if got := toolNames(t, e.taskPanelTools(turn, nil, true)); len(got) != 1 || !got["read_panel_state"] {
		t.Fatalf("planning tools=%v", got)
	}
	if _, err := e.panelSource(t.Context(), turn); err == nil {
		t.Fatal("unowned publisher accepted")
	}
	if e.taskPanelTools(nil, nil, false) != nil {
		t.Fatal("nil turn mounted tools")
	}
	if err := e.cfg.Update(func(c *config.Config) error { off := false; c.TaskPanels.Enabled = &off; return nil }); err != nil {
		t.Fatal(err)
	}
	if e.taskPanelInstruction() != "" || e.taskPanelTools(turn, nil, false) != nil || e.toolOfferAllowed("open_task_panel") {
		t.Fatal("disabled panel tools still offered")
	}
}
