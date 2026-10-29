package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Seeing a tool in a hand-built list does not prove that the actual manager
// can discover and read another idle conversation through the HTTP adapter.
func TestProjectHistoryQueryReachesTheManagerAndReadsAnIdlePeer(t *testing.T) {
	var projectID, currentID, peerID string
	const source = "The recorded operation requires explicit approval."
	e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		available := false
		for _, tl := range request.Tools {
			available = available || tl.Function.Name == "query_history"
		}
		if !available {
			t.Error("the actual manager HTTP request has no query_history tool")
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "No history query was available."}, "stop")
			return
		}
		switch step {
		case 1:
			if len(request.Messages) == 0 || !strings.Contains(request.Messages[0].Content, projectID) || !strings.Contains(request.Messages[0].Content, currentID) {
				t.Error("the manager did not receive its actual project and conversation identities")
			}
			if strings.Contains(fmt.Sprint(request.Messages), source) {
				t.Error("a peer's original text was injected before the model queried it")
			}
			args, _ := json.Marshal(map[string]any{"sql": "SELECT m.thread_id,m.seq,m.role,m.content FROM messages m JOIN threads t ON t.id=m.thread_id WHERE t.project_id=? AND m.thread_id<>? AND m.role='user' ORDER BY m.thread_id,m.seq", "params": []string{projectID, currentID}})
			writeScheduleCompletionTool(w, request, "query_history", string(args))
		case 2:
			found := false
			for _, message := range request.Messages {
				if message.Role != "tool" {
					continue
				}
				var result struct {
					OK      bool             `json:"ok"`
					Rows    []map[string]any `json:"rows"`
					Context struct {
						ProjectID string `json:"project_id"`
						ThreadID  string `json:"thread_id"`
					} `json:"context"`
				}
				if err := json.Unmarshal([]byte(message.Content), &result); err != nil || !result.OK {
					t.Errorf("actual tool result was not successful JSON: %s (%v)", message.Content, err)
					continue
				}
				if len(result.Rows) != 1 || len(result.Rows[0]) != 4 || result.Rows[0]["thread_id"] != peerID || result.Rows[0]["content"] != source || result.Context.ProjectID != projectID || result.Context.ThreadID != currentID {
					t.Errorf("query did not return the exact original peer record and current ownership: %s", message.Content)
				}
				found = true
			}
			if !found {
				t.Error("the real ADK tool result never reached the next manager request")
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": historyAnswerJSON(peerID)}, "stop")
		default:
			t.Errorf("unexpected manager request %d", step)
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "Unexpected extra request."}, "stop")
		}
	})
	project, err := e.CreateProject("History fixture", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	projectID = project.ID
	peer, err := e.CreateThread("Earlier discussion", "", projectID)
	if err != nil {
		t.Fatal(err)
	}
	peerID = peer.ID
	if err := e.Store().AppendMessages(peer.ID, "", []store.Message{{Role: "user", Source: store.MessageSourceHuman, Content: source}}); err != nil {
		t.Fatal(err)
	}
	current, err := e.CreateThread("Current discussion", "", projectID)
	if err != nil {
		t.Fatal(err)
	}
	currentID = current.ID
	turn, err := e.StartTurn(currentID, "Review the earlier discussions in this project.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, currentID)
	if got.Status != store.TurnDone {
		t.Fatalf("history query turn: %+v", got)
	}
	if requests := fixture.snapshot(); len(requests) != 2 {
		t.Errorf("manager HTTP requests=%d, want query then answer", len(requests))
	}
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	toolCalls, toolResults := 0, 0
	for _, event := range events {
		if event.Kind == swarm.NotifyToolCall.String() && strings.Contains(event.Text, "query_history") {
			toolCalls++
		}
		if event.Kind == swarm.NotifyToolResult.String() && strings.Contains(event.Text, source) {
			toolResults++
		}
	}
	if toolCalls != 1 || toolResults != 1 {
		t.Errorf("Trace history query calls=%d original-result receipts=%d", toolCalls, toolResults)
	}
}
