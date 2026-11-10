package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// Model output can publish the call before ADK submits its next node. Start
// from that durable boundary and exercise the actual Stop and turn finalizer
// with no Ask waiter, tool callback, or manager transcript available.
func TestInterruptFinalizesAPublishedAskBeforeDispatch(t *testing.T) {
	for _, state := range []string{"unanswered", "accepted", "already recorded", "stored call"} {
		t.Run(state, func(t *testing.T) {
			e := newTestEngine(t)
			thread, err := e.CreateThread("Decision", "", "")
			if err != nil {
				t.Fatal(err)
			}
			turn := plantUnfinishedTurn(t, e, thread.ID, "Choose a direction.")
			args := restartReplayAskArguments(t, "decision")
			const callID = "queued-ask"
			answer := AskAnswers{"decision": {Answers: []string{"one"}}}
			want := askToolFailure("cancelled")
			toolset := buildTestToolset(t, e, thread.ID)
			reg := e.newTurnRegistry(func(_, _ string) model.BaseChatModel {
				return &headerWaitModel{msg: schema.AssistantMessage("unexpected model request", nil)}
			}, toolset, nil, turn)
			t.Cleanup(reg.Close)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			idle := make(chan struct{})
			rt := e.runtimeFor(thread.ID)
			if !rt.occupy(reg, cancel, turn.ID, idle) {
				t.Fatal("could not occupy the original turn")
			}
			defer rt.release(cancel, idle, turn.ID)
			recordRestartReplayAsk(e, turn, callID, args)
			if state == "accepted" {
				if err := e.AnswerTurn(thread.ID, callID, answer); err != nil {
					t.Fatalf("answer before dispatch: %v", err)
				}
				want = FormatAskResult(answer)
			}
			if state == "stored call" {
				calls, err := json.Marshal(restartReplayAskMessage(callID, args).ToolCalls)
				if err != nil {
					t.Fatal(err)
				}
				if err := e.Store().AppendMessages(thread.ID, turn.ID, []store.Message{{
					Role: string(schema.Assistant), ToolCalls: string(calls),
				}}); err != nil {
					t.Fatal(err)
				}
			}
			if state == "already recorded" {
				want = FormatAskResult(answer)
				e.record(store.Event{ThreadID: thread.ID, TurnID: turn.ID,
					Kind: swarm.NotifyToolResult.String(), AgentID: swarm.DefaultManagerID, ToolCallID: callID, Text: want})
			}
			if e.Status(thread.ID).AwaitingAnswer {
				t.Fatal("fixture crossed the ADK dispatch boundary")
			}
			if err := e.Interrupt(thread.ID); err != nil {
				t.Fatal(err)
			}
			if state != "already recorded" {
				if err := e.AnswerTurn(thread.ID, callID, answer); !errors.Is(err, ErrIdle) {
					t.Fatalf("answer after Stop: %v", err)
				}
			}
			if state == "accepted" {
				questions, err := ParseAskArguments(args)
				if err != nil {
					t.Fatal(err)
				}
				old := withHistoryReadOwner(ctx, thread.ID, "earlier-turn")
				if _, err := rt.waitAskCall(old, questions, callID); !errors.Is(err, context.Canceled) {
					t.Fatalf("another turn acquired this answer: %v", err)
				}
				owner := withHistoryReadOwner(ctx, thread.ID, turn.ID)
				if got, err := rt.waitAskCall(owner, questions, callID); err != nil || FormatAskResult(got) != want {
					t.Fatalf("original waiter lost its accepted answer: %v, %v", got, err)
				}
			}
			input := []adk.Message{schema.UserMessage(turn.UserText)}
			rt.run(ctx, cancel, idle, turn, reg, toolset, nil, input, len(input), nil, false)
			if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnCancelled {
				t.Fatalf("stopped turn=%+v", got)
			}
			events, err := e.Store().ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			results := 0
			for _, ev := range events {
				if ev.Kind == swarm.NotifyToolResult.String() && ev.ToolCallID == callID {
					if ev.Text != want {
						t.Fatalf("Ask result=%+v, want %s", ev, want)
					}
					results++
				}
			}
			if results != 1 {
				t.Fatalf("got %d receipts for %q", results, callID)
			}
			if state != "already recorded" {
				assertAskTranscript(t, e, turn, callID, want)
			}
		})
	}
}

// Providers can reuse a tool call ID in a later turn. A matching previous
// transcript prefix must not suppress the current assistant call's storage.
func TestStoppedAskTranscriptKeepsACurrentCallWithAnOlderReusedID(t *testing.T) {
	e := newTestEngine(t)
	thread, err := e.CreateThread("Decision", "", "")
	if err != nil {
		t.Fatal(err)
	}
	previous := plantUnfinishedTurn(t, e, thread.ID, "Previous decision.")
	current := plantUnfinishedTurn(t, e, thread.ID, "Current decision.")
	const callID = "reused-ask"
	previousCall := restartReplayAskMessage(callID, restartReplayAskArguments(t, "previous"))
	previousCalls, err := json.Marshal(previousCall.ToolCalls)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendMessages(thread.ID, previous.ID, []store.Message{{
		Role: string(schema.Assistant), ToolCalls: string(previousCalls),
	}}); err != nil {
		t.Fatal(err)
	}
	result := askToolFailure("cancelled")
	transcript := []adk.Message{schema.SystemMessage("Instructions"), previousCall, schema.UserMessage(current.UserText)}
	e.persistTranscript(thread.ID, current.ID, transcript, 2, stoppedAskReceipt{
		call:   orphanedToolCall{Text: ToolAskUser + "(" + restartReplayAskArguments(t, "current") + ")"},
		result: store.Event{ToolCallID: callID, Text: result},
	})
	assertAskTranscript(t, e, current, callID, result)
}
