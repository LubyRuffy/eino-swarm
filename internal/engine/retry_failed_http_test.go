package engine

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// A failed step keeps the tool call and result already stored on that turn.
// Retry continues the same turn; it does not rewind to the user message.
func TestRetryFailedTurnContinuesFromTheStoredToolResult(t *testing.T) {
	const callID = "retry-effect-call"
	const userText = "Record one workspace effect, then continue from that result."
	const answer = "The existing workspace effect is verified."
	e, wire := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			args, _ := json.Marshal(map[string]any{"command": "printf 'effect-once\\n' >> committed-effects; printf 'committed receipt\\n'"})
			writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": callID, "type": "function", "function": map[string]any{"name": "exec", "arguments": string(args)},
			}}}, "tool_calls")
		case 2:
			http.Error(w, `{"error":{"message":"request refused","type":"invalid_request_error"}}`, http.StatusUnauthorized)
		case 3:
			users, results := 0, 0
			for _, message := range request.Messages {
				if message == nil {
					continue
				}
				if message.Role == schema.User && strings.Contains(message.Content, userText) {
					users++
				}
				if message.Role == schema.User && (message.Content == resumeCue || message.Content == failureContinuedNotice) {
					t.Errorf("continuation invented a user cue: %q", message.Content)
				}
				if message.Role == schema.Tool && message.ToolCallID == callID && strings.Contains(message.Content, "committed receipt") {
					results++
				}
			}
			if users != 1 || results != 1 {
				t.Errorf("continuation lost the failed step: users=%d results=%d", users, results)
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": answer}, "stop")
		default:
			t.Errorf("unexpected manager request %d", step)
			writeContextFlowReply(w, request.Stream, map[string]any{"content": answer}, "stop")
		}
	})
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Swarm.ComposerPredictions = false
	e.cfg.Swarm.ManagerMaxIterations = 100
	e.cfg.Swarm.MaxCompletionTokens = 16384
	thread, err := e.CreateThread("Failure continuation", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, userText)
	if err != nil {
		t.Fatal(err)
	}
	failed := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if failed.Status != store.TurnError || len(wire.snapshot()) != 2 {
		t.Fatalf("the tool step did not fail on the next model call: status=%s requests=%d err=%q", failed.Status, len(wire.snapshot()), failed.Error)
	}
	before, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	userSeq := int64(0)
	for _, event := range before {
		if event.Kind == KindUser {
			userSeq = event.Seq
		}
		if event.Kind == KindRewound {
			t.Fatal("the failure itself rewound the turn")
		}
	}
	if userSeq <= 0 {
		t.Fatal("the failed turn has no user message")
	}
	e.cfg.Swarm.MaxCompletionTokens = 8192

	continued, err := e.RetryFailedTurn(thread.ID, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if continued.ID != turn.ID || continued.Status != store.TurnRunning || continued.EndedAt != nil {
		t.Fatalf("retry did not reopen the same turn: %+v", continued)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	requests := wire.snapshot()
	effect, readErr := os.ReadFile(filepath.Join(e.WorkspaceDir(thread.ID), "committed-effects"))
	if readErr != nil || string(effect) != "effect-once\n" {
		t.Fatalf("workspace effect was lost or repeated: %q, %v", effect, readErr)
	}
	if len(requests) != 3 || got.Status != store.TurnDone || got.Final != answer || got.ID != turn.ID {
		t.Fatalf("continuation did not finish the same turn: requests=%d turn=%+v", len(requests), got)
	}
	if requests[0].MaxTokens != 16384 || requests[1].MaxTokens != 16384 || requests[2].MaxTokens != 8192 {
		t.Fatalf("continuation did not keep the earlier cap and then use the current one: %+v", []int{requests[0].MaxTokens, requests[1].MaxTokens, requests[2].MaxTokens})
	}
	turns, err := e.Store().ListTurns(thread.ID)
	if err != nil || len(turns) != 1 || turns[0].ID != turn.ID {
		t.Fatalf("retry replaced the turn: %+v, %v", turns, err)
	}
	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	users, continuedNotices, rewinds := 0, 0, 0
	for _, event := range events {
		switch event.Kind {
		case KindUser:
			users++
			if event.Seq != userSeq {
				t.Fatalf("user message seq changed from %d to %d", userSeq, event.Seq)
			}
		case KindFailureContinued:
			continuedNotices++
			if event.Text != failureContinuedNotice {
				t.Fatalf("continuation notice = %q", event.Text)
			}
		case KindRewound:
			rewinds++
		}
	}
	if users != 1 || continuedNotices != 1 || rewinds != 0 {
		t.Fatalf("events users=%d continued=%d rewound=%d", users, continuedNotices, rewinds)
	}
	rows, err := e.Store().ListMessages(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	storedUsers, storedResults := 0, 0
	for _, row := range rows {
		if row.TurnID != turn.ID {
			continue
		}
		if row.Role == string(schema.User) && strings.Contains(row.Content, userText) {
			storedUsers++
		}
		if row.Role == string(schema.Tool) && row.ToolCallID == callID && strings.Contains(row.Content, "committed receipt") {
			storedResults++
		}
	}
	if storedUsers != 1 || storedResults != 1 {
		t.Fatalf("stored progress changed: users=%d results=%d", storedUsers, storedResults)
	}
}
