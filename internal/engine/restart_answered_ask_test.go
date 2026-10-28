package engine

import (
	"encoding/json"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// A rejected tool call never created a human wait. An already answered current
// question must also remain settled when earlier calls have uncertain results.
func TestRestartAnsweredQuestionIsNotConfusedWithAnEarlierFailedAsk(t *testing.T) {
	for _, earlier := range []string{"invalid", "unknown", "cancelled"} {
		for _, identity := range []string{"answer", "ask_call"} {
			t.Run(earlier+"/"+identity, func(t *testing.T) {
				args := restartReplayAskArguments(t, "decision")
				oldArgs, oldResult := args, swarm.HandoffUnknownToolResult
				switch earlier {
				case "invalid":
					oldArgs, oldResult = `{}`, `{"ok":false,"error":"ask_user: questions are required"}`
				case "cancelled":
					oldResult = `{"ok":false,"error":"context canceled"}`
				}
				answer := AskAnswers{"decision": {Answers: []string{"one"}}}
				cp := restartCheckpoint{Version: 1, ManagerHistory: []*schema.Message{
					restartReplayAskMessage("earlier-ask", oldArgs), schema.ToolMessage(oldResult, "earlier-ask"),
					restartReplayAskMessage("answered-ask", args), schema.ToolMessage(FormatAskResult(answer), "answered-ask"),
				}}
				if identity == "answer" {
					cp.Answer = &restartAskAnswer{CallID: "answered-ask", Answers: answer}
				} else {
					cp.Pause, cp.AskCallID = "ask", "answered-ask"
				}
				body, err := json.Marshal(cp)
				if err != nil {
					t.Fatal(err)
				}
				// The filtered startup snapshot may contain only the checkpoint:
				// both calls already have durable results in its manager history.
				events := []store.Event{{Kind: KindRestartCheckpoint, Text: string(body)}}
				if err := restartAskRecoveryError(events); err != nil {
					t.Fatalf("earlier %s call blocked an answered question: %v", earlier, err)
				}
				if !historyConfirmedAsk(cp.ManagerHistory, "answered-ask") || cp.ManagerHistory[1].Content != oldResult {
					t.Fatal("recovery changed the confirmed answer or the earlier failure receipt")
				}
			})
		}
	}
}

// Unrelated confirmed answers cannot repair missing or contradictory checkpoint
// identity. Unknown/cancelled receipts are still not human answers.
func TestRestartAcceptedAnswerCannotBorrowAnotherQuestionsIdentity(t *testing.T) {
	for _, name := range []string{"empty-id", "missing-id", "conflicting-id", "invalid-answer", "unknown-result", "cancelled-result", "valid-accepted"} {
		t.Run(name, func(t *testing.T) {
			args := restartReplayAskArguments(t, "decision")
			answer := AskAnswers{"decision": {Answers: []string{"one"}}}
			cp := restartCheckpoint{Version: 1, Answer: &restartAskAnswer{CallID: "current-ask", Answers: answer}, ManagerHistory: []*schema.Message{
				restartReplayAskMessage("older-answered-ask", args), schema.ToolMessage(FormatAskResult(answer), "older-answered-ask"),
				restartReplayAskMessage("current-ask", args),
			}}
			events := []store.Event{{Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID,
				ToolCallID: "current-ask", Text: ToolAskUser + "(" + args + ")"}}
			switch name {
			case "empty-id":
				cp.Answer.CallID = ""
				cp.ManagerHistory = cp.ManagerHistory[:2]
			case "missing-id":
				cp.Answer.CallID = "absent-ask"
				cp.ManagerHistory = cp.ManagerHistory[:2]
			case "conflicting-id":
				cp.AskCallID = "older-answered-ask"
			case "invalid-answer", "unknown-result", "cancelled-result":
				cp.Answer.Answers = AskAnswers{}
				if name != "invalid-answer" {
					result := swarm.HandoffUnknownToolResult
					if name == "cancelled-result" {
						result = `{"ok":false,"error":"context canceled"}`
					}
					cp.ManagerHistory = append(cp.ManagerHistory, schema.ToolMessage(result, "current-ask"))
				}
			}
			body, err := json.Marshal(cp)
			if err != nil {
				t.Fatal(err)
			}
			events = append(events, store.Event{Kind: KindRestartCheckpoint, Text: string(body)})
			if err := restartAskRecoveryError(events); (err == nil) != (name == "valid-accepted") {
				t.Fatalf("checkpoint identity/answer validation: err=%v", err)
			}
		})
	}
}
