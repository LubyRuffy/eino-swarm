package engine

import (
	"encoding/json"
	"fmt"
	"reflect"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// KindAskRecovered restores an existing human wait, never a new tool execution.
// Its payload is only the original ask arguments, not the private checkpoint.
const KindAskRecovered = "ask_recovered"

func askResultConfirmed(text string, call orphanedToolCall) bool {
	_, args := splitToolCallText(call.Text)
	questions, err := ParseAskArguments(args)
	if err != nil {
		return false
	}
	var result struct {
		Answers AskAnswers `json:"answers"`
	}
	if json.Unmarshal([]byte(text), &result) != nil {
		return false
	}
	_, err = resolveAskAnswers(questions, result.Answers)
	return err == nil
}

// A legacy unknown receipt is recoverable only with an unambiguous durable
// human pause. A confirmed answer is stronger evidence than that placeholder.
func checkpointAskCall(events []store.Event) *orphanedToolCall {
	cp, _ := latestRestartCheckpoint(events)
	if cp == nil || cp.HumanStopped || cp.Pause != "ask" && cp.Answer == nil {
		return nil
	}
	calls := map[string]orphanedToolCall{}
	for _, msg := range cp.ManagerHistory {
		if msg == nil {
			continue
		}
		if msg.Role == schema.Assistant {
			for _, call := range msg.ToolCalls {
				if call.ID != "" && call.Function.Name == ToolAskUser {
					calls[call.ID] = orphanedToolCall{ToolCallID: call.ID, Text: ToolAskUser + "(" + call.Function.Arguments + ")"}
				}
			}
		} else if msg.Role == schema.Tool {
			if call, ok := calls[msg.ToolCallID]; ok && askResultConfirmed(msg.Content, call) {
				delete(calls, msg.ToolCallID)
			}
		}
	}
	if cp.Answer != nil {
		call, ok := calls[cp.Answer.CallID]
		// The answer can be acknowledged before the manager's history hook.
		// Its original durable call still proves the question that was answered.
		if !ok && !historyConfirmedAsk(cp.ManagerHistory, cp.Answer.CallID) {
			for _, ev := range events {
				if ev.Kind == swarm.NotifyToolCall.String() && isManagerAgent(ev.AgentID) && ev.ToolCallID == cp.Answer.CallID && isAskUserToolText(ev.Text) {
					call = orphanedToolCall{ToolCallID: ev.ToolCallID, Text: ev.Text}
					ok = true
					break
				}
			}
		}
		if !ok || !askResultConfirmed(FormatAskResult(cp.Answer.Answers), call) {
			return nil
		}
		calls = map[string]orphanedToolCall{call.ToolCallID: call}
	} else {
		if cp.AskCallID != "" {
			call, ok := calls[cp.AskCallID]
			if !ok {
				return nil
			}
			calls = map[string]orphanedToolCall{call.ToolCallID: call}
		}
		for _, ev := range events {
			if call, ok := calls[ev.ToolCallID]; ok && ev.Kind == swarm.NotifyToolResult.String() && isManagerAgent(ev.AgentID) && ev.Err == "" && askResultConfirmed(ev.Text, call) {
				delete(calls, ev.ToolCallID)
			}
		}
	}
	if len(calls) != 1 {
		return nil
	}
	for id, saved := range calls {
		_, args := splitToolCallText(saved.Text)
		questions, err := ParseAskArguments(args)
		if err != nil {
			return nil
		}
		for _, ev := range events {
			if ev.Kind != swarm.NotifyToolCall.String() || ev.ToolCallID != id || !isManagerAgent(ev.AgentID) || !isAskUserToolText(ev.Text) {
				continue
			}
			_, actualArgs := splitToolCallText(ev.Text)
			actual, err := ParseAskArguments(actualArgs)
			if err == nil && reflect.DeepEqual(questions, actual) {
				return &orphanedToolCall{ToolCallID: id, Text: ev.Text, AgentID: ev.AgentID, Role: ev.Role}
			}
		}
	}
	return nil
}

func historyConfirmedAsk(messages []*schema.Message, callID string) bool {
	var call orphanedToolCall
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		for _, tc := range msg.ToolCalls {
			if msg.Role == schema.Assistant && tc.ID == callID && tc.Function.Name == ToolAskUser {
				call = orphanedToolCall{ToolCallID: callID, Text: ToolAskUser + "(" + tc.Function.Arguments + ")"}
			}
		}
		if msg.Role == schema.Tool && msg.ToolCallID == callID && call.ToolCallID != "" && askResultConfirmed(msg.Content, call) {
			return true
		}
	}
	return false
}

func restartAskRecoveryError(events []store.Event) error {
	cp, _ := latestRestartCheckpoint(events)
	if cp == nil || cp.HumanStopped || cp.Pause != "ask" && cp.Answer == nil {
		return nil
	}
	callID := cp.AskCallID
	if cp.Answer != nil {
		if cp.Answer.CallID == "" || callID != "" && callID != cp.Answer.CallID {
			return fmt.Errorf("restart checkpoint has no matching human question")
		}
		callID = cp.Answer.CallID
	}
	if checkpointAskCall(events) != nil {
		return nil
	}
	// A consumed answer can remain in the checkpoint after its tool result was
	// saved. Earlier rejected calls do not own that answer or its recovery.
	seen := false
	for _, msg := range cp.ManagerHistory {
		if msg == nil || msg.Role != schema.Assistant {
			continue
		}
		for _, tc := range msg.ToolCalls {
			if tc.Function.Name != ToolAskUser || tc.ID == "" || callID != "" && callID != tc.ID {
				continue
			}
			seen = true
			call := orphanedToolCall{ToolCallID: tc.ID, Text: ToolAskUser + "(" + tc.Function.Arguments + ")"}
			confirmed := historyConfirmedAsk(cp.ManagerHistory, tc.ID)
			for _, ev := range events {
				confirmed = confirmed || ev.Kind == swarm.NotifyToolResult.String() && isManagerAgent(ev.AgentID) && ev.ToolCallID == tc.ID && ev.Err == "" && askResultConfirmed(ev.Text, call)
			}
			if !confirmed {
				return fmt.Errorf("restart checkpoint cannot identify the unanswered human question")
			}
		}
	}
	if !seen {
		return fmt.Errorf("restart checkpoint has no matching human question")
	}
	return nil
}

func restartAskPlaceholder(ev store.Event) bool {
	return ev.Text == swarm.HandoffUnknownToolResult || restartAskCancelled(ev)
}

// This is a replay projection. Historical receipts remain intact in Trace.
func restartAskEvents(events []store.Event) []store.Event {
	proven := checkpointAskCall(events)
	calls := map[string]orphanedToolCall{}
	confirmed := map[string]bool{}
	for _, ev := range events {
		if ev.Kind == swarm.NotifyToolCall.String() && isManagerAgent(ev.AgentID) && isAskUserToolText(ev.Text) {
			calls[ev.ToolCallID] = orphanedToolCall{ToolCallID: ev.ToolCallID, Text: ev.Text}
		}
	}
	for _, ev := range events {
		if call, ok := calls[ev.ToolCallID]; ok && ev.Kind == swarm.NotifyToolResult.String() && isManagerAgent(ev.AgentID) && ev.Err == "" && askResultConfirmed(ev.Text, call) {
			confirmed[ev.ToolCallID] = true
		}
	}
	out := make([]store.Event, 0, len(events))
	for _, ev := range events {
		if ev.Kind == swarm.NotifyToolResult.String() && isManagerAgent(ev.AgentID) && restartAskPlaceholder(ev) &&
			(confirmed[ev.ToolCallID] || proven != nil && proven.ToolCallID == ev.ToolCallID) {
			continue
		}
		out = append(out, ev)
	}
	return out
}

func (e *Engine) recordRecoveredAsk(turn *store.Turn, call *orphanedToolCall) error {
	_, args := splitToolCallText(call.Text)
	var public struct {
		Questions []AskQuestion `json:"questions"`
	}
	if err := json.Unmarshal([]byte(args), &public); err != nil {
		return fmt.Errorf("read recovered human question: %w", err)
	}
	if _, err := NormalizeAskQuestions(public.Questions); err != nil {
		return fmt.Errorf("validate recovered human question: %w", err)
	}
	body, _ := json.Marshal(public)
	ev := store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, AgentID: swarm.DefaultManagerID,
		Role: "manager", Kind: KindAskRecovered, ToolCallID: call.ToolCallID, Text: string(body)}
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	if _, dropped := e.droppedTurns[turn.ID]; dropped {
		return fmt.Errorf("persist recovered human question: turn was removed")
	}
	if err := e.store.AppendEvent(&ev); err != nil {
		return fmt.Errorf("persist recovered human question: %w", err)
	}
	e.broadcast(ev)
	return nil
}
