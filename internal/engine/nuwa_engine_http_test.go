package engine

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// The actual manager must be able to share work through its registered tools,
// not a helper invoked directly by a test or a special domain workflow.
func TestNuwaManagerSharesEvidenceThroughActualHTTPModel(t *testing.T) {
	e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		available := map[string]bool{}
		for _, tool := range request.Tools {
			available[tool.Function.Name] = true
		}
		for _, name := range []string{"list_models", "read_work", "record_work"} {
			if !available[name] {
				t.Errorf("actual manager is missing %s", name)
			}
		}
		switch step {
		case 1:
			writeScheduleCompletionTool(w, request, "record_work", `{"kind":"fact","title":"Measured result","summary":"The supplied input was inspected.","evidence":[]}`)
		case 2:
			var receipt struct {
				OK   bool           `json:"ok"`
				Node store.NuwaNode `json:"node"`
			}
			last := request.Messages[len(request.Messages)-1]
			if err := json.Unmarshal([]byte(last.Content), &receipt); err != nil || !receipt.OK || receipt.Node.AgentID != "manager" || receipt.Node.Version != 1 {
				t.Errorf("shared result has no actual manager provenance: %s (decode=%v)", last.Content, err)
			}
			args, err := json.Marshal(map[string]any{"id": receipt.Node.ID, "version": receipt.Node.Version, "summary": "The supplied input was inspected again."})
			if err != nil {
				t.Fatal(err)
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "shared-correction", "type": "function", "function": map[string]any{"name": "record_work", "arguments": string(args)}}}}, "tool_calls")
		case 3:
			var receipt struct {
				OK   bool           `json:"ok"`
				Node store.NuwaNode `json:"node"`
			}
			last := request.Messages[len(request.Messages)-1]
			if err := json.Unmarshal([]byte(last.Content), &receipt); err != nil || !receipt.OK || receipt.Node.Kind != "fact" || receipt.Node.Title != "Measured result" || receipt.Node.Version != 2 {
				t.Errorf("partial correction lost unchanged shared data: %s (decode=%v)", last.Content, err)
			}
			writeScheduleCompletionTool(w, request, "read_work", `{}`)
		default:
			if !strings.Contains(joinedContent(request.Messages), "Measured result") {
				t.Error("shared observation did not reach the model")
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The result is recorded with its provenance."}, "stop")
		}
	})
	project, err := e.CreateProject("Evidence work", "Decide how to work from the supplied evidence.", "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Inspect supplied input", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Inspect the supplied input and record the result.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || len(fixture.snapshot()) != 4 {
		t.Fatalf("shared work failed at the actual manager boundary: %+v, requests=%d", got, len(fixture.snapshot()))
	}
	revision, nodes, err := e.store.NuwaSnapshot(t.Context(), project.ID)
	if err != nil || revision != 2 || len(nodes) != 1 || nodes[0].TurnID != turn.ID || nodes[0].ThreadID != thread.ID {
		t.Fatalf("durable shared result: revision=%d nodes=%+v err=%v", revision, nodes, err)
	}
}
