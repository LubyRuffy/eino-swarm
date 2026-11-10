package engine

import (
	"encoding/json"
	"slices"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type stoppedAskReceipt struct {
	call   orphanedToolCall
	result store.Event
}

// The manager can publish the question before ADK dispatches its tool. Stop
// can also detach an already-running tool before its result reaches ADK.
// Close either gap after the manager's notifications have drained, while
// normal finalization still owns this turn.
func (rt *runtime) stoppedAskReceipts(turn *store.Turn) []stoppedAskReceipt {
	rt.mu.Lock()
	stopped := rt.turnID == turn.ID && rt.userStopped && !rt.abandoned
	accepted := rt.acceptedAsk
	rt.mu.Unlock()
	if !stopped {
		return nil
	}

	e := rt.engine
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	if _, dropped := e.droppedTurns[turn.ID]; dropped {
		return nil
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		e.logPersistFailure("could not read stopped Ask calls", err, "turn", turn.ID)
		return nil
	}
	if !slices.ContainsFunc(events, func(event store.Event) bool { return event.Kind == KindUserStopRequested }) {
		return nil
	}
	var receipts []stoppedAskReceipt
	// Use raw rows here. A restart projection may hide a placeholder from
	// an older execution, but a Stop receipt must never duplicate a row.
	for _, call := range unpairedToolCalls(events) {
		if !isManagerAgent(call.AgentID) || !isAskUserToolText(call.Text) {
			continue
		}
		body := askToolFailure("cancelled")
		if accepted != nil && accepted.CallID == call.ToolCallID {
			if answer := FormatAskResult(accepted.Answers); askResultConfirmed(answer, call) {
				body = answer
			}
		}
		result := store.Event{
			ThreadID: turn.ThreadID, TurnID: turn.ID, AgentID: swarm.DefaultManagerID, Role: call.Role,
			Kind: swarm.NotifyToolResult.String(), ToolCallID: call.ToolCallID, Text: body,
		}
		if err := e.store.AppendEvent(&result); err != nil {
			e.logPersistFailure("could not persist a stopped Ask result", err, "turn", turn.ID, "call", call.ToolCallID)
			continue
		}
		e.broadcast(result)
		receipts = append(receipts, stoppedAskReceipt{call: call, result: result})
	}
	return receipts
}

// Rebuild only the new transcript tail. An ID reused by an older turn cannot
// prove the current call was saved. The durable tool call supplies any call
// the ADK transcript lost before dispatch.
func (e *Engine) stoppedAskTranscript(threadID, turnID string, tail []adk.Message, receipts []stoppedAskReceipt) []adk.Message {
	var storedCalls map[string]bool
	var results []store.Event
	for _, receipt := range receipts {
		callID := receipt.result.ToolCallID
		if !hasAssistantToolCall(tail, callID) {
			if storedCalls == nil {
				storedCalls = e.storedAskCalls(threadID, turnID)
			}
			if !storedCalls[callID] {
				name, args := splitToolCallText(receipt.call.Text)
				tail = append(tail, schema.AssistantMessage("", []schema.ToolCall{{
					ID: callID, Function: schema.FunctionCall{Name: name, Arguments: args},
				}}))
			}
		}
		results = append(results, receipt.result)
	}
	return stitchManagerToolResults(tail, results)
}

func (e *Engine) storedAskCalls(threadID, turnID string) map[string]bool {
	ids := make(map[string]bool)
	messages, err := e.store.ListMessages(threadID)
	if err != nil {
		e.logPersistFailure("could not read stored Ask calls", err, "turn", turnID)
		return ids
	}
	for _, message := range messages {
		if message.TurnID != turnID || message.Role != string(schema.Assistant) || message.ToolCalls == "" {
			continue
		}
		var calls []schema.ToolCall
		if json.Unmarshal([]byte(message.ToolCalls), &calls) != nil {
			continue
		}
		for _, call := range calls {
			if call.Function.Name == ToolAskUser {
				ids[call.ID] = true
			}
		}
	}
	return ids
}
