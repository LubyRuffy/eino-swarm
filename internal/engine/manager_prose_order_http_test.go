package engine

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Keep the final model response blocked so the subscriber must see the
// commentary during the turn, in front of the tool that follows it.
func TestManagerCommentaryAppearsBeforeItsToolInLiveAndStoredOrder(t *testing.T) {
	const first = "I will inspect the first artifact."
	const second = "I will compare the second artifact."
	const final = "Both artifacts were inspected."
	type stepFixture struct {
		text, reasoning, toolID string
	}
	steps := []stepFixture{
		{first, "Identify the first source.", "read-first"},
		{second, "Identify the comparison source.", "read-second"},
	}
	workspace := t.TempDir()
	for _, step := range steps {
		if err := os.WriteFile(filepath.Join(workspace, step.toolID), []byte(step.toolID), 0600); err != nil {
			t.Fatal(err)
		}
	}
	finalResponse := make(chan struct{})
	releaseFinal := sync.OnceFunc(func() { close(finalResponse) })
	e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, r *http.Request, n int, request scheduleCompletionRequest) {
		if n <= len(steps) {
			step := steps[n-1]
			args, _ := json.Marshal(map[string]string{"file_path": filepath.Join(workspace, step.toolID)})
			writeContextFlowReply(w, request.Stream, map[string]any{
				"content": step.text, "reasoning_content": step.reasoning,
				"tool_calls": []any{map[string]any{
					"index": 0, "id": step.toolID, "type": "function",
					"function": map[string]any{"name": "read", "arguments": string(args)},
				}},
			}, "tool_calls")
			return
		}
		select {
		case <-finalResponse:
			writeContextFlowReply(w, request.Stream, map[string]any{"content": final}, "stop")
		case <-r.Context().Done():
		}
	})
	defer releaseFinal()
	project, err := e.CreateProject("Artifact comparison", "", workspace, false, false)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("interleaved manager commentary", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(thread.ID)
	defer sub.Close()
	turn, err := e.StartTurn(thread.ID, "Compare the two local artifacts.")
	if err != nil {
		t.Fatal(err)
	}

	eventMarker := func(ev store.Event) string {
		if ev.AgentID != swarm.DefaultManagerID {
			return ""
		}
		switch ev.Kind {
		case "delta", "agent_message":
			for _, step := range steps {
				if ev.Text == step.text {
					return step.text
				}
			}
		case "tool_call":
			return ev.ToolCallID
		case "tool_call_delta":
			return ev.ToolCallID + "-preview"
		}
		return ""
	}
	want := []string{first, steps[0].toolID, second, steps[1].toolID}
	var live []string
	var observed []store.Event
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for !slices.Contains(live, steps[1].toolID) {
		select {
		case ev := <-sub.C:
			if marker := eventMarker(ev); marker != "" && !slices.Contains(live, marker) {
				live = append(live, marker)
				observed = append(observed, ev)
			}
		case <-deadline.C:
			t.Fatalf("manager did not reach the second tool: events %q", live)
		}
	}
	var liveRows []string
	for _, marker := range live {
		if !strings.HasSuffix(marker, "-preview") {
			liveRows = append(liveRows, marker)
		}
	}
	if !reflect.DeepEqual(liveRows, want) {
		t.Errorf("manager commentary during the live turn: got %q, want %q; events %+v", liveRows, want, observed)
	}
	for _, step := range steps {
		preview := slices.Index(live, step.toolID+"-preview")
		if preview >= 0 && (preview < slices.Index(live, step.text) || preview > slices.Index(live, step.toolID)) {
			t.Errorf("tool preview escaped its model round: %q; events %+v", live, observed)
		}
	}

	releaseFinal()
	got := waitForTurn(t, e, turn.ID)
	requests := fixture.snapshot()
	if got.Status != store.TurnDone || got.Final != final || len(requests) != 3 {
		t.Fatalf("fixture did not finish two real tool rounds: turn=%+v requests=%d", got, len(requests))
	}
	for i, step := range steps {
		if !hasToolResult(requests[i+1].Messages, step.toolID) {
			t.Errorf("manager request %d omitted the real tool result %q", i+2, step.toolID)
		}
	}
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	var stored []string
	roundID := map[string]string{}
	for _, ev := range events {
		if marker := eventMarker(ev); marker != "" {
			stored = append(stored, marker)
			roundID[marker] = ev.MessageID
		}
	}
	if !reflect.DeepEqual(stored, want) {
		t.Errorf("manager commentary in durable sequence: got %q, want %q", stored, want)
	}
	for _, step := range steps {
		if roundID[step.text] == "" || roundID[step.text] != roundID[step.toolID] {
			t.Errorf("commentary and tool lost their common model generation: %q, %q", roundID[step.text], roundID[step.toolID])
		}
	}
	if roundID[first] == roundID[second] {
		t.Error("independent model generations shared a commentary identity")
	}
	var paged []string
	var before int64
	for {
		page, more, err := e.Store().ListTailEvents(thread.ID, before, 3)
		if err != nil {
			t.Fatal(err)
		}
		var markers []string
		for _, ev := range page {
			if marker := eventMarker(ev); marker != "" {
				markers = append(markers, marker)
			}
		}
		paged = append(markers, paged...)
		if !more {
			break
		}
		before = page[0].Seq
	}
	if !reflect.DeepEqual(paged, want) {
		t.Errorf("manager commentary in paged replay: got %q, want %q", paged, want)
	}
}
