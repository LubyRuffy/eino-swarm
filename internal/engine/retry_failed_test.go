package engine

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func failedHumanTurn(t *testing.T, e *Engine, threadID string, patch func(*store.Turn)) *store.Turn {
	t.Helper()
	turn := &store.Turn{ThreadID: threadID, UserText: "Inspect the assigned work.", Status: store.TurnRunning}
	if patch != nil {
		patch(turn)
	}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().FinishTurn(turn.ID, store.TurnError, "", "the service refused"); err != nil {
		t.Fatal(err)
	}
	return turn
}

func TestRetryFailedTurnRefusesAnythingButTheLatestHumanFailure(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("retry refusal", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.RetryFailedTurn(th.ID, "tn_missing"); !errors.Is(err, ErrNoFailedTurn) {
		t.Fatalf("empty conversation: %v", err)
	}
	if _, err := e.RetryFailedTurn("missing", "tn_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing conversation: %v", err)
	}

	failed := failedHumanTurn(t, e, th.ID, nil)
	if _, err := e.RetryFailedTurn(th.ID, "tn_other"); !errors.Is(err, ErrNoFailedTurn) {
		t.Fatalf("wrong turn: %v", err)
	}
	blank := failedHumanTurn(t, e, th.ID, func(turn *store.Turn) { turn.UserText = "   " })
	if _, err := e.RetryFailedTurn(th.ID, blank.ID); !errors.Is(err, ErrNoFailedTurn) {
		t.Fatalf("failure without stored input: %v", err)
	}
	if got, err := e.Store().GetTurn(blank.ID); err != nil || got.Status != store.TurnError {
		t.Fatalf("refusal reopened the turn: %+v, %v", got, err)
	}

	for _, patch := range []func(*store.Turn){
		func(turn *store.Turn) { turn.GoalContinue = true },
		func(turn *store.Turn) { turn.ScheduleContinue = true },
		func(turn *store.Turn) { turn.ScheduleRunID = "run" },
	} {
		automatic := failedHumanTurn(t, e, th.ID, patch)
		if _, err := e.RetryFailedTurn(th.ID, automatic.ID); !errors.Is(err, ErrNoFailedTurn) {
			t.Fatalf("automatic failure was continuable: %v", err)
		}
	}

	cancelled := &store.Turn{ThreadID: th.ID, UserText: "Inspect the assigned work.", Status: store.TurnRunning}
	if err := e.Store().CreateTurn(cancelled); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().FinishTurn(cancelled.ID, store.TurnCancelled, "", "interrupted"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.RetryFailedTurn(th.ID, cancelled.ID); !errors.Is(err, ErrNoFailedTurn) {
		t.Fatalf("cancelled turn: %v", err)
	}

	done := &store.Turn{ThreadID: th.ID, UserText: "A later request.", Status: store.TurnRunning}
	if err := e.Store().CreateTurn(done); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().FinishTurn(done.ID, store.TurnDone, "answered", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.RetryFailedTurn(th.ID, failed.ID); !errors.Is(err, ErrNoFailedTurn) {
		t.Fatalf("older failure after a later turn: %v", err)
	}
	if got, err := e.Store().GetTurn(failed.ID); err != nil || got.Status != store.TurnError {
		t.Fatalf("older failure changed: %+v, %v", got, err)
	}
}

func TestRetryFailedTurnStaysFailedWhileTheConversationIsBusy(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("retry busy", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := failedHumanTurn(t, e, th.ID, nil)
	if err := e.Store().AppendMessages(th.ID, turn.ID, []store.Message{
		{Role: string(schema.User), Content: turn.UserText},
	}); err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(th.ID)
	rt.mu.Lock()
	rt.running = true
	rt.mu.Unlock()

	if _, err := e.RetryFailedTurn(th.ID, turn.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy conversation: %v", err)
	}
	got, err := e.Store().GetTurn(turn.ID)
	if err != nil || got.Status != store.TurnError || got.EndedAt == nil {
		t.Fatalf("busy retry reopened the failure: %+v, %v", got, err)
	}
}

func TestFailureRetryKeepsStoredToolProgressAndDropsAnUnpairedCall(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("retry context", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := failedHumanTurn(t, e, th.ID, nil)
	done := toolCallJSON(t, "done-call", "exec", `{"command":"printf ok"}`)
	orphan := toolCallJSON(t, "orphan-call", "exec", `{"command":"printf again"}`)
	if err := e.Store().AppendMessages(th.ID, turn.ID, []store.Message{
		{Role: string(schema.User), Content: turn.UserText},
		{Role: string(schema.Assistant), ToolCalls: done},
		{Role: string(schema.Tool), Content: "committed receipt", ToolCallID: "done-call"},
		{Role: string(schema.Assistant), ToolCalls: orphan},
	}); err != nil {
		t.Fatal(err)
	}
	messages, err := e.failureRetryMessages(turn)
	if err != nil {
		t.Fatal(err)
	}
	if got := userContents(messages); len(got) != 1 || got[0] != turn.UserText {
		t.Fatalf("user text = %#v", got)
	}
	if !failureRetryHasResult(messages, "done-call", "committed receipt") || hasToolID(messages, "orphan-call") {
		t.Fatalf("tool progress was not preserved: %#v", describeMessages(messages))
	}
	for _, message := range messages {
		if message != nil && message.Role == schema.User && (message.Content == resumeCue || message.Content == failureContinuedNotice) {
			t.Fatalf("continuation invented a user cue: %q", message.Content)
		}
	}

	fallback := failedHumanTurn(t, e, th.ID, func(row *store.Turn) { row.UserText = "Stored only on the turn." })
	messages, err = e.failureRetryMessages(fallback)
	if err != nil {
		t.Fatal(err)
	}
	if got := userContents(messages); len(got) != 2 || got[0] != turn.UserText || got[1] != "Stored only on the turn." {
		t.Fatalf("missing user row was not restored once: %#v", got)
	}
}

func toolCallJSON(t *testing.T, id, name, args string) string {
	t.Helper()
	raw, err := json.Marshal([]schema.ToolCall{{
		ID: id, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func userContents(messages []adk.Message) []string {
	var out []string
	for _, message := range messages {
		if message != nil && message.Role == schema.User {
			out = append(out, message.Content)
		}
	}
	return out
}

func failureRetryHasResult(messages []adk.Message, id, text string) bool {
	for _, message := range messages {
		if message != nil && message.Role == schema.Tool && message.ToolCallID == id && strings.Contains(message.Content, text) {
			return true
		}
	}
	return false
}

func hasToolID(messages []adk.Message, id string) bool {
	for _, message := range messages {
		if message == nil {
			continue
		}
		if message.ToolCallID == id {
			return true
		}
		for _, call := range message.ToolCalls {
			if call.ID == id {
				return true
			}
		}
	}
	return false
}

func describeMessages(messages []adk.Message) []string {
	out := make([]string, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			out = append(out, "<nil>")
			continue
		}
		out = append(out, string(message.Role)+":"+message.Content+":"+message.ToolCallID)
	}
	return out
}
