package engine

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// An operational task may consult history for leads. That read must not turn
// its deliverable into a source report: the manager's ordinary answer is
// published as written, with no correction round and no report protocol in
// its System prompt.
func TestIncidentalHistoryReadKeepsTheOrdinaryAnswerAtTheActualManagerBoundary(t *testing.T) {
	const answer = "Service map: api runs from repository one; worker runs from repository two."
	e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		if strings.Contains(joinedContent(request.Messages), historyReportPrompt()) {
			t.Error("an ordinary task was told to answer in the history report protocol")
		}
		if step == 1 {
			writeScheduleCompletionTool(w, request, ToolQueryHistory, `{}`)
			return
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": answer}, "stop")
	})
	project, err := e.CreateProject("Operational task", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Map running services", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Map each running service to its repository.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || got.Final != answer {
		t.Fatalf("incidental history read rewrote the ordinary answer: status=%q final=%q error=%q", got.Status, got.Final, got.Error)
	}
	if requests := fixture.snapshot(); len(requests) != 2 {
		t.Fatalf("manager requests=%d, want the read and one answer without correction", len(requests))
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	started := 0
	for _, event := range events {
		if event.Kind == KindHistoryReadStarted {
			started++
		}
		if event.Kind == KindModelRetry {
			t.Fatalf("ordinary answer needed a history correction: %s", event.Text)
		}
	}
	if started != 1 {
		t.Fatalf("history read receipts=%d, the read must stay traceable", started)
	}
}

// In an explicit report turn a real history read must not be followed by
// freely authored historical conclusions reaching either the final answer or
// the durable public log.
func TestHistoryReadWithholdsUnstructuredConclusionsAtTheActualManagerBoundary(t *testing.T) {
	const unverified = "A freely invented historical decision must never be delivered."
	e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		if !strings.Contains(joinedContent(request.Messages), historyReportPrompt()) {
			t.Error("explicit report turn lost the report protocol")
		}
		if step == 1 {
			writeScheduleCompletionTool(w, request, ToolQueryHistory, `{}`)
			return
		}
		if step > 3 {
			t.Errorf("history formatting refusal exceeded its single correction allowance: %d", step)
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": unverified}, "stop")
	})
	project, err := e.CreateProject("History answer ownership", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Bound history report", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurnInput(thread.ID, UserInput{Text: "Inspect the retained original records and report their sources.", HistoryReport: true})
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnError || got.Final != "" {
		t.Fatalf("unstructured historical conclusions escaped admission: status=%q final=%q error=%q", got.Status, got.Final, got.Error)
	}
	if requests := fixture.snapshot(); len(requests) != 3 {
		t.Fatalf("history refusal requests=%d, want original query, refused answer, one correction", len(requests))
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if isManagerAgent(event.AgentID) && (event.Kind == swarm.NotifyAgentMessage.String() || event.Kind == swarm.NotifyDone.String() || event.Kind == swarm.NotifyDelta.String()) && strings.Contains(event.Text, unverified) {
			t.Fatalf("unverified prose was published as %s", event.Kind)
		}
	}
	messages, err := e.store.ListMessages(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range messages {
		if message.Role == "assistant" && strings.Contains(message.Content, unverified) {
			t.Fatal("unverified history conclusion entered replay")
		}
	}
}

func TestHistoryReportRepairsItsProtocolWithoutRepeatingTheReadAndPublishesOnlyHostText(t *testing.T) {
	for _, invalid := range []string{"free-prose", "forged-authority", "wrong-source"} {
		t.Run(invalid, func(t *testing.T) {
			var peerID string
			const original = "This is an assistant proposal, not implemented.\nDeletion is forbidden unless separately approved."
			e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
				if step == 1 {
					writeScheduleCompletionTool(w, request, ToolQueryHistory, `{}`)
					return
				}
				text := historyAnswerJSON(peerID)
				if step == 2 {
					switch invalid {
					case "free-prose":
						text = "A fictional human approval."
					case "forged-authority":
						text = strings.Replace(text, `"language":"en"`, `"language":"en","role":"user"`, 1)
					case "wrong-source":
						text = historyAnswerJSON("absent-source")
					}
				} else if step == 3 {
					if !strings.Contains(joinedContent(request.Messages), historyAnswerCorrectionCue) {
						t.Error("repair lost the generic same-turn instruction")
					}
				} else {
					t.Errorf("unexpected extra manager request %d", step)
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"content": text}, "stop")
			})
			project, err := e.CreateProject("Repairable report", "", "", false)
			if err != nil {
				t.Fatal(err)
			}
			peer, err := e.CreateThread("Assistant original", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			peerID = peer.ID
			if err := e.store.AppendMessages(peerID, "", []store.Message{{Role: "assistant", Content: original}}); err != nil {
				t.Fatal(err)
			}
			thread, err := e.CreateThread("Current review", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			sub := e.Subscribe(thread.ID)
			defer sub.Close()
			turn, err := e.StartTurnInput(thread.ID, UserInput{Text: "Inspect the original records and propose next work.", HistoryReport: true})
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			if got.Status != store.TurnDone || !strings.Contains(got.Final, original) || !strings.Contains(got.Final, "assistant") || strings.Contains(got.Final, `"version":1`) {
				t.Fatalf("host did not publish the complete attributed source: %+v", got)
			}
			if len(fixture.snapshot()) != 3 {
				t.Fatal("repair repeated actual history reading or model work")
			}
			events, err := e.store.ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			answers, reads, retries := 0, 0, 0
			for _, event := range events {
				switch event.Kind {
				case "agent_message":
					if isManagerAgent(event.AgentID) {
						answers++
						if event.Text != got.Final {
							t.Fatal("published intermediate/raw report")
						}
					}
				case "tool_call":
					if toolCallName(event.Text) == ToolQueryHistory {
						reads++
					}
				case KindModelRetry:
					var body map[string]any
					if json.Unmarshal([]byte(event.Text), &body) == nil && body["reason"] == "answer_unverified" {
						retries++
					}
				}
			}
			if answers != 1 || reads != 1 || retries != 1 {
				t.Fatalf("answer/read/retry=%d/%d/%d", answers, reads, retries)
			}
			for {
				select {
				case event := <-sub.C:
					if isManagerAgent(event.AgentID) && (event.Kind == "agent_message" || event.Kind == "delta" || event.Kind == "done") && event.Text != "" && event.Text != got.Final {
						t.Fatalf("unrendered prose reached live stream: %s", event.Text)
					}
				default:
					goto drained
				}
			}
		drained:
			messages, err := e.store.ListMessages(thread.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, message := range messages {
				if message.Role == "assistant" && message.Content != "" && message.Content != got.Final {
					t.Fatalf("raw report reached replay: %s", message.Content)
				}
			}
		})
	}
}

func TestExplicitHistoryRequestRequiresAnActualReadAtTheHTTPModelBoundary(t *testing.T) {
	var peerID string
	e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		if step == 2 {
			if !strings.Contains(joinedContent(request.Messages), historyAnswerCorrectionCue) {
				t.Error("explicit history request did not receive its one read-required correction")
			}
			writeHistoryTool(w, request, "requested-source-read", ToolQueryHistory, `{}`)
			return
		}
		if step > 3 {
			t.Errorf("explicit history read correction repeated requests: %d", step)
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": historyAnswerJSON(peerID)}, "stop")
	})
	project, err := e.CreateProject("Explicit source report", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := e.CreateThread("Original statement", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	peerID = peer.ID
	if err := e.store.AppendMessages(peer.ID, "", []store.Message{{Role: "assistant", Content: historyRecoveryOriginal}}); err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Explicit request", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurnInput(thread.ID, UserInput{Text: "Inspect the retained records.", HistoryReport: true})
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || !strings.Contains(got.Final, historyRecoveryOriginal) || len(fixture.snapshot()) != 3 {
		t.Fatalf("explicit report skipped real reading or lost bounded correction: %+v requests=%d", got, len(fixture.snapshot()))
	}
	events, _ := e.store.ListTurnEvents(turn.ID)
	requested, reads, retries := 0, 0, 0
	for _, event := range events {
		if event.Kind == "history_report_requested" {
			requested++
		}
		if event.Kind == "tool_call" && toolCallName(event.Text) == ToolQueryHistory {
			reads++
		}
		if event.Kind == KindModelRetry {
			var body map[string]any
			if json.Unmarshal([]byte(event.Text), &body) == nil && body["code"] == "history_read_required" {
				retries++
			}
		}
	}
	if requested != 1 || reads != 1 || retries != 1 {
		t.Fatalf("explicit request/read/repair receipts=%d/%d/%d", requested, reads, retries)
	}
}
