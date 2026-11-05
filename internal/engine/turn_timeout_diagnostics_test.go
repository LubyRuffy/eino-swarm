package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"runtime/pprof"
	"strings"
	"testing"
)

// Capture the timed-out state before fixture cleanup can stop its actors.
// Successful waits never collect diagnostics or change the original deadline.
func logTurnTimeout(t *testing.T, e *Engine, turnID string) {
	t.Helper()
	var stack bytes.Buffer
	_ = pprof.Lookup("goroutine").WriteTo(&stack, 2)
	// Bypass testing.T's buffered logs if a later diagnostic read blocks.
	// The go command can still buffer this output until the package exits.
	fmt.Fprintf(os.Stderr, "turn timeout goroutines:\n%s", stack.Bytes())
	fingerprint := func(text string) string {
		if text == "" {
			return ""
		}
		return fmt.Sprintf("bytes=%d sha256=%x", len(text), sha256.Sum256([]byte(text)))
	}
	turn, err := e.Store().GetTurn(turnID)
	t.Logf("turn timeout GetTurn: error_type=%T error_present=%t", err, err != nil)
	if turn != nil {
		t.Logf("turn timeout durable: id=%s thread=%s status=%s provider=%s model=%s started=%s ended=%v duration_ms=%d error=%s", turn.ID, turn.ThreadID, turn.Status, turn.ProviderID, turn.Model, turn.StartedAt, turn.EndedAt, turn.DurationMS, fingerprint(turn.Error))
		t.Logf("turn timeout runtime: %+v", e.Status(turn.ThreadID))
	}
	events, err := e.Store().ListTurnEvents(turnID)
	t.Logf("turn timeout ListTurnEvents: count=%d error_type=%T error_present=%t", len(events), err, err != nil)
	for _, event := range events {
		var data any
		switch event.Kind {
		case KindModelRetry, KindMaxIterations, KindWorkersCollectionResumed, KindWorkersCollectionDeclined, "generation_interrupted", "generation_recovered":
			var state struct {
				Reason, Recovery    string
				Attempt, Cap, Limit int
				ExtendBy            int    `json:"extend_by"`
				WaitID              string `json:"wait_id"`
				AutomaticResume     bool   `json:"automatic_resume"`
				Workers             []struct {
					AgentID    string `json:"agent_id"`
					Generation int
					Running    bool
				}
			}
			if json.Unmarshal([]byte(event.Text), &state) == nil {
				data = state
			}
		}
		name := ""
		if event.Kind == "tool_call" && strings.Contains(event.Text, "(") {
			name = toolCallName(event.Text)
		}
		t.Logf("turn timeout event: seq=%d kind=%s agent=%s generation=%d call=%s message=%s tool=%s data=%+v text_bytes=%d error=%s", event.Seq, event.Kind, event.AgentID, event.WorkerGeneration, event.ToolCallID, event.MessageID, name, data, len(event.Text), fingerprint(event.Err))
	}
	calls, err := e.Store().ListLLMCalls(turnID)
	t.Logf("turn timeout ListLLMCalls: count=%d error_type=%T error_present=%t", len(calls), err, err != nil)
	for _, call := range calls {
		t.Logf("turn timeout LLM: id=%d agent=%s provider=%s model=%s created=%s duration_ms=%d input_messages=%d input_chars=%d output_chars=%d error=%s", call.ID, call.AgentID, call.ProviderID, call.Model, call.CreatedAt, call.DurationMS, call.InputMsgs, call.InputChars, call.OutputChars, fingerprint(call.Err))
	}
}
