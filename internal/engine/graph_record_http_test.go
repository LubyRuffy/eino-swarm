package engine

import (
	"net/http"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// A final answer does not keep tool results off the graph. The host withholds
// that answer once, asks for one update_graph, and then keeps whatever follows.
func TestAFinishedAnswerRecordsUnwrittenToolResultsOnce(t *testing.T) {
	e, requests := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			if !requestHasTool(request, "exec") || !requestHasTool(request, ToolUpdateGraph) {
				t.Fatalf("tools: %s", requestToolNames(request))
			}
			panelAdmissionCall(w, request, "look", "exec", `{"command":"printf noted"}`)
		case 2:
			if strings.Contains(requestText(request), graphRecordCue) {
				t.Fatal("the recording pass ran before an answer was offered")
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The result is ready."}, "stop")
		case 3:
			if !strings.Contains(requestText(request), graphRecordCue) {
				t.Fatal("the withheld answer was kept without a recording pass")
			}
			panelAdmissionCall(w, request, "record", ToolUpdateGraph, `{"subjects":[{"ref":"h","kind":"ip","attrs":{"ip":"203.0.113.10"}}]}`)
		case 4:
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Recorded."}, "stop")
		default:
			t.Errorf("recording pass was not one call: step %d", step)
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Recorded."}, "stop")
		}
	})
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Swarm.GraphAutoRecord = true
	thread, err := e.CreateThread("Graph record", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Look and keep what is found.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone {
		t.Fatalf("turn=%+v", got)
	}
	if len(requests.snapshot()) != 4 {
		t.Fatalf("manager requests=%d", len(requests.snapshot()))
	}
	view, err := e.ReadThreadGraph(t.Context(), thread.ID, store.GraphQuery{Limit: -1})
	if err != nil || len(view.Subjects) != 1 || view.Subjects[0].Kind != "ip" {
		t.Fatalf("view=%+v err=%v", view, err)
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil || !graphRecordUsed(events) {
		t.Fatalf("allowance missing: %v", err)
	}
}

func TestAnAnswerWithoutToolResultsIsNotSentBackToTheGraph(t *testing.T) {
	e, requests := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		if step != 1 {
			t.Errorf("a text answer was sent back: step %d", step)
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "Nothing to record."}, "stop")
	})
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Swarm.GraphAutoRecord = true
	thread, err := e.CreateThread("Graph record", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Answer without looking anything up.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || len(requests.snapshot()) != 1 {
		t.Fatalf("status=%s requests=%d", got.Status, len(requests.snapshot()))
	}
	view, err := e.ReadThreadGraph(t.Context(), thread.ID, store.GraphQuery{Limit: -1})
	if err != nil || len(view.Subjects) != 0 {
		t.Fatalf("view=%+v err=%v", view, err)
	}
}

func TestASecondAnswerStandsWhenTheRecordingPassWritesNothing(t *testing.T) {
	e, requests := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			panelAdmissionCall(w, request, "look", "exec", `{"command":"printf noted"}`)
		case 2, 3:
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The result is ready."}, "stop")
		default:
			t.Errorf("the pass was repeated: step %d", step)
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "The result is ready."}, "stop")
		}
	})
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Swarm.GraphAutoRecord = true
	thread, err := e.CreateThread("Graph record", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Look and keep what is found.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || len(requests.snapshot()) != 3 {
		t.Fatalf("status=%s requests=%d err=%s", got.Status, len(requests.snapshot()), got.Error)
	}
	if !strings.Contains(requestText(requests.snapshot()[2]), graphRecordCue) {
		t.Fatal("the second answer did not follow the recording pass")
	}
	view, err := e.ReadThreadGraph(t.Context(), thread.ID, store.GraphQuery{Limit: -1})
	if err != nil || len(view.Subjects) != 0 {
		t.Fatalf("view=%+v err=%v", view, err)
	}
}

// The extra model call is optional. Losing the receipt that spends its
// allowance must leave the original answer eligible to publish.
func TestGraphRecordPublishesTheOriginalAnswerWhenItsAllowanceCannotBeSaved(t *testing.T) {
	const original = "The first answer is ready."
	e, requests := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			panelAdmissionCall(w, request, "look", "exec", `{"command":"printf noted"}`)
		case 2:
			writeContextFlowReply(w, request.Stream, map[string]any{"content": original}, "stop")
		default:
			t.Errorf("an unrecorded allowance authorized another model request: step %d", step)
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Unexpected replacement answer."}, "stop")
		}
	})
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	if err := e.store.DB().Exec(`CREATE TRIGGER reject_graph_record_allowance BEFORE INSERT ON events
		WHEN NEW.kind = 'model_retry' AND instr(NEW.text, '"graph_unrecorded"') > 0
		BEGIN SELECT RAISE(ABORT, 'graph record allowance unavailable'); END`).Error; err != nil {
		t.Fatal(err)
	}
	e.cfg.Swarm.GraphAutoRecord = true
	thread, err := e.CreateThread("Graph record storage fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Look and report what was found.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || got.Final != original || len(requests.snapshot()) != 2 {
		t.Errorf("status=%s final=%q requests=%d err=%s", got.Status, got.Final, len(requests.snapshot()), got.Error)
	}
	if n, err := e.store.GraphUnrecorded(t.Context(), thread.ID, turn.ID); n != 1 || err != nil {
		t.Errorf("unrecorded results=%d, err=%v", n, err)
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil || graphRecordUsed(events) {
		t.Fatalf("failed allowance appeared in the durable event log: %v", err)
	}
	for _, event := range events {
		if event.Kind == "agent_message" && isManagerAgent(event.AgentID) && event.Text == original {
			return
		}
	}
	t.Fatal("the original manager answer did not reach the published event log")
}

func requestText(request scheduleCompletionRequest) string {
	var b strings.Builder
	for _, message := range request.Messages {
		if message != nil && message.Role != schema.System {
			b.WriteString(message.Content)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
