package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

// Reproduce a reasoning-only length stop through the real chat adapter. The
// recovery only counts if the original turn executes an actual workspace tool
// and sends its result to the model before admitting the final answer.
func TestManagerReasoningOutputLimitRecoveryThroughHTTP(t *testing.T) {
	const (
		task      = "Write the requested workspace receipt and verify the result."
		reasoning = "This interrupted reasoning must remain in Trace only."
		answer    = "The workspace receipt was written."
		filename  = "workspace-receipt.txt"
		fileBody  = "verified 3.13.7"
		writeArgs = `{"file_path":"workspace-receipt.txt","content":"verified 3.13.7"}`
	)
	for _, tc := range []struct {
		name                 string
		enabled              bool
		recoveryEffort       string
		originalEffort       string
		wantRecoveryEffort   string
		repeat, partial      bool
		empty, afterWork     bool
		durableStop          bool
		wantCalls, wantRetry int
		wantWrite, wantDone  bool
	}{
		{name: "disabled", wantCalls: 1},
		{name: "recovery", enabled: true, wantCalls: 3, wantRetry: 1, wantWrite: true, wantDone: true},
		{name: "recovery effort changes exactly one request", enabled: true, recoveryEffort: config.ReasoningLow, wantRecoveryEffort: config.ReasoningLow, wantCalls: 3, wantRetry: 1, wantWrite: true, wantDone: true},
		{name: "explicit original effort stays on each request", enabled: true, recoveryEffort: config.ReasoningLow, originalEffort: config.ReasoningHigh, wantCalls: 3, wantRetry: 1, wantWrite: true, wantDone: true},
		{name: "recovery effort requires recovery switch", recoveryEffort: config.ReasoningLow, wantCalls: 1},
		{name: "second output limit", enabled: true, repeat: true, wantCalls: 2, wantRetry: 1},
		{name: "partial tool arguments", enabled: true, partial: true, wantCalls: 1},
		{name: "no reasoning", enabled: true, empty: true, wantCalls: 1},
		{name: "after a completed tool", enabled: true, afterWork: true, wantCalls: 3, wantRetry: 1, wantWrite: true, wantDone: true},
		{name: "stop recorded before cancellation", enabled: true, durableStop: true, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capReady, releaseCap := make(chan struct{}), make(chan struct{}, 1)
			defer close(releaseCap)
			e, fixture := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, r *http.Request, step int, request scheduleCompletionRequest) {
				if step == 1 && tc.durableStop {
					close(capReady)
					select {
					case <-releaseCap:
					case <-r.Context().Done():
						return
					}
				}
				switch {
				case (step == 1 && tc.afterWork) || (step == 2 && tc.wantDone && !tc.afterWork):
					writeScheduleCompletionTool(w, request, "write", writeArgs)
				case step == 1 || (step == 2 && (tc.repeat || tc.afterWork)):
					delta := map[string]any{}
					if !tc.empty {
						delta["reasoning_content"] = reasoning
					}
					if tc.partial {
						delta["tool_calls"] = []any{map[string]any{
							"index": 0, "id": "partial-write", "type": "function",
							"function": map[string]any{"name": "write", "arguments": `{"file_path":"workspace-receipt.txt","content":"`},
						}}
					}
					if !request.Stream {
						t.Error("manager request did not use streaming")
					}
					writeOutputLimitFrames(w, delta)
				default:
					writeContextFlowReply(w, request.Stream, map[string]any{"content": answer}, "stop")
				}
			})
			if tc.enabled {
				e.cfg.Swarm.RecoverReasoningOutputLimit = true
			}
			e.cfg.Swarm.ReasoningOutputRecoveryEffort = tc.recoveryEffort
			e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
			th, err := e.CreateThread("Output limit recovery fixture", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := e.SetThreadReasoning(th.ID, tc.originalEffort); err != nil {
				t.Fatal(err)
			}
			started, err := e.StartTurn(th.ID, task)
			if err != nil {
				t.Fatal(err)
			}
			if tc.durableStop {
				select {
				case <-capReady:
				case <-time.After(5 * time.Second):
					t.Fatal("first manager request did not reach the fixture")
				}
				// Exercise the gap after the real Stop receipt is written
				// and before interruptTurn has cancelled the runtime.
				target := &restartTarget{rt: e.runtimeFor(th.ID), turnID: started.ID}
				if err := e.writeRestartEvent(target, KindUserStopRequested, "Stop requested by the user."); err != nil {
					t.Fatal(err)
				}
				releaseCap <- struct{}{}
			}
			got := waitForTurn(t, e, started.ID)
			waitSettled(t, e, th.ID)
			requests := fixture.snapshot()
			if len(requests) != tc.wantCalls {
				t.Fatalf("manager requests=%d, want %d, turn=%+v", len(requests), tc.wantCalls, got)
			}
			for i, request := range requests {
				want := tc.originalEffort
				if i == 1 && tc.wantRecoveryEffort != "" {
					want = tc.wantRecoveryEffort
				}
				if want == "" {
					if request.ReasoningEffort != nil {
						t.Fatalf("request %d sent reasoning effort %q, expected omission", i+1, *request.ReasoningEffort)
					}
				} else if request.ReasoningEffort == nil || *request.ReasoningEffort != want {
					t.Fatalf("request %d did not send reasoning effort %q", i+1, want)
				}
				if request.Model == "" || request.Model != requests[0].Model ||
					request.MaxTokens != e.cfg.Swarm.CompletionTokenLimit() {
					t.Fatalf("request %d changed the provider model or output cap: %q, %d", i+1, request.Model, request.MaxTokens)
				}
			}
			if tc.wantDone {
				if got.Status != store.TurnDone || got.Final != answer || got.Error != "" {
					t.Fatalf("the original turn did not recover: %+v", got)
				}
			} else if got.Status != store.TurnError || !strings.Contains(got.Error, "output budget") || got.Final != "" {
				t.Fatalf("the failed generation lost its terminal cause: %+v", got)
			}
			turns, err := e.store.ListTurns(th.ID)
			if err != nil || len(turns) != 1 || turns[0].ID != started.ID {
				t.Fatalf("recovery changed turn identity: %+v, %v", turns, err)
			}
			if tc.wantRetry != 0 {
				var retainedTask, newCue bool
				recoveryRequest := 1
				if tc.afterWork {
					recoveryRequest = 2
				}
				for _, message := range requests[recoveryRequest].Messages {
					if message.Role == schema.User {
						retainedTask = retainedTask || message.Content == task
						cue := strings.ToLower(message.Content)
						newCue = newCue || strings.Contains(cue, "smallest") && strings.Contains(cue, "verifiable")
					}
					if strings.Contains(message.Content, reasoning) || strings.Contains(message.ReasoningContent, reasoning) {
						t.Fatal("interrupted reasoning crossed the provider boundary again")
					}
				}
				if !retainedTask || !newCue {
					t.Fatal("the recovery request did not preserve the task and add actionable guidance")
				}
			}
			if tc.wantDone {
				hasResult := false
				for _, message := range requests[2].Messages {
					hasResult = hasResult || message.Role == schema.Tool && message.ToolCallID == "scheduled-completion-call" && strings.Contains(message.Content, filename)
				}
				if !hasResult {
					t.Fatal("the recovered manager never received its actual write result")
				}
			}
			written, err := os.ReadFile(filepath.Join(e.WorkspaceDir(th.ID), filename))
			if tc.wantWrite {
				if err != nil || string(written) != fileBody {
					t.Fatalf("the write did not execute: %q, %v", written, err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("an interrupted generation executed its write: %q, %v", written, err)
			}
			assertOutputLimitRecoveryEvents(t, e, started.ID, tc.wantRetry, tc.wantWrite, tc.repeat)
		})
	}
}

func writeOutputLimitFrames(w http.ResponseWriter, delta map[string]any) {
	w.Header().Set("Content-Type", "text/event-stream")
	delta["role"] = "assistant"
	for _, choice := range []map[string]any{
		{"index": 0, "delta": delta, "finish_reason": nil},
		{"index": 0, "delta": map[string]any{}, "finish_reason": "length"},
	} {
		raw, _ := json.Marshal(map[string]any{"id": "fixture", "object": "chat.completion.chunk", "choices": []any{choice}})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

func assertOutputLimitRecoveryEvents(t *testing.T, e *Engine, turnID string, wantRetry int, wantWrite, repeat bool) {
	t.Helper()
	events, err := e.store.ListTurnEvents(turnID)
	if err != nil {
		t.Fatal(err)
	}
	var interruptions []store.Event
	var retry, recovery *store.Event
	var calls, results int
	for _, event := range events {
		switch event.Kind {
		case "generation_interrupted":
			var body swarm.GenerationInterruption
			if err := json.Unmarshal([]byte(event.Text), &body); err != nil || body.Reason != "output_limit" || event.MessageID == "" {
				t.Fatalf("original output-limit cause or identity was lost: %+v, %v", event, err)
			}
			interruptions = append(interruptions, event)
		case KindModelRetry:
			if retry != nil {
				t.Fatal("more than one retry receipt")
			}
			var body struct {
				Attempt int
				Cap     int
				Reason  string
			}
			if err := json.Unmarshal([]byte(event.Text), &body); err != nil || body.Attempt != 1 || body.Cap != 1 || body.Reason != "output_limit" {
				t.Fatalf("unexpected retry receipt: %+v, %v", event, err)
			}
			retry = &event
		case "generation_recovered":
			if recovery != nil {
				t.Fatal("more than one recovery receipt")
			}
			recovery = &event
		case swarm.NotifyToolCall.String():
			calls++
		case swarm.NotifyToolResult.String():
			results++
		}
	}
	wantInterruptions := 1
	if repeat {
		wantInterruptions++
	}
	if len(interruptions) != wantInterruptions {
		t.Fatalf("interruption receipts=%d, want %d", len(interruptions), wantInterruptions)
	}
	if wantRetry == 1 {
		first := interruptions[0]
		if retry == nil || recovery == nil || retry.MessageID != first.MessageID || recovery.MessageID != first.MessageID || first.Seq >= retry.Seq || retry.Seq >= recovery.Seq {
			t.Fatalf("retry did not preserve and link the original interruption: first=%+v, retry=%+v, recovery=%+v", first, retry, recovery)
		}
	} else if retry != nil || recovery != nil {
		t.Fatalf("ineligible generation was retried: retry=%+v, recovery=%+v", retry, recovery)
	}
	wantTools := 0
	if wantWrite {
		wantTools = 1
	}
	if calls != wantTools || results != wantTools {
		t.Fatalf("tool execution was lost or repeated: calls=%d, results=%d, want %d", calls, results, wantTools)
	}
}

// If either durable receipt cannot be written, the first failure remains
// terminal. A loopback request is a stronger witness than a retry return value.
func TestManagerOutputLimitStorageFailureDoesNotSendRecoveryRequest(t *testing.T) {
	for _, failedKind := range []string{"generation_interrupted", KindModelRetry} {
		t.Run(failedKind, func(t *testing.T) {
			var requests, faults atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				writeOutputLimitFrames(w, map[string]any{"reasoning_content": "Checking the workspace evidence."})
			}))
			t.Cleanup(endpoint.Close)
			// A private Store keeps the injected callback until close.
			e := newTestEngineAt(t, t.TempDir())
			e.cfg.Swarm.AutoTitle = false
			e.cfg.Swarm.RecoverReasoningOutputLimit = true
			e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
			e.cfg.Models.Providers[0].BaseURL = endpoint.URL
			e.cfg.Models.Providers[0].APIKey = ""
			e.cfg.Models.Providers[0].Model = "offline-output-limit-storage-failure"
			e.pool = provider.New(e.cfg)
			th, err := e.CreateThread("Output limit storage fixture", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := e.store.DB().Callback().Create().Before("gorm:create").Register("refuse_output_limit_receipt", func(tx *gorm.DB) {
				if event, ok := tx.Statement.Dest.(*store.Event); ok && event.Kind == failedKind {
					faults.Add(1)
					_ = tx.AddError(errors.New("fixture output-limit receipt write failure"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			started, err := e.StartTurn(th.ID, "Inspect the current workspace evidence.")
			if err != nil {
				t.Fatal(err)
			}
			got := waitForTurn(t, e, started.ID)
			waitSettled(t, e, th.ID)
			if faults.Load() != 1 || requests.Load() != 1 || got.Status != store.TurnError ||
				!strings.Contains(got.Error, "output budget") || got.Final != "" {
				t.Fatalf("storage failure allowed a retry or lost the original error: faults=%d requests=%d turn=%+v", faults.Load(), requests.Load(), got)
			}
			events, err := e.store.ListTurnEvents(started.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range events {
				if event.Kind == failedKind || event.Kind == KindModelRetry || event.Kind == "generation_recovered" {
					t.Fatalf("storage refusal created a false receipt: %+v", event)
				}
			}
		})
	}
}

func TestReasoningOnlyInterruptionRequiresAnUnusedDurableAllowance(t *testing.T) {
	const id = "first-generation"
	started := store.Event{Kind: "generation_started", AgentID: swarm.DefaultManagerID, MessageID: id}
	interrupted := store.Event{Kind: "generation_interrupted", AgentID: swarm.DefaultManagerID, MessageID: id,
		Text: `{"reason":"output_limit","recovery":"none"}`}
	if !reasoningOnlyInterruption([]store.Event{started, interrupted}, id) {
		t.Fatal("the first matching interruption did not admit a recovery")
	}
	prior := store.Event{Kind: "generation_started", AgentID: swarm.DefaultManagerID, MessageID: "prior-output"}
	priorDone := store.Event{Kind: "generation_completed", AgentID: swarm.DefaultManagerID, MessageID: "prior-output"}
	for _, before := range [][]store.Event{
		{{Kind: KindSteer}},
		{prior, {Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, MessageID: "prior-output"}, priorDone},
		{{Kind: swarm.NotifyToolResult.String(), AgentID: "worker"}},
	} {
		if !reasoningOnlyInterruption(append(before, started, interrupted), id) {
			t.Fatal("valid prior work prevented recovery of this clean generation")
		}
	}
	for _, tc := range []struct {
		name     string
		messages []store.Event
		match    string
	}{
		{"missing identity", []store.Event{started, interrupted}, ""},
		{"mismatched identity", []store.Event{started, interrupted}, "unrelated-generation"},
		{"interruption without start", []store.Event{interrupted}, id},
		{"start without interruption", []store.Event{started}, id},
		{"duplicate interruption", []store.Event{started, interrupted, interrupted}, id},
		{"prior generation", []store.Event{started, started, interrupted}, id},
		{"malformed receipt", []store.Event{started, {Kind: "generation_interrupted", AgentID: swarm.DefaultManagerID, MessageID: id, Text: "invalid"}}, id},
		{"already pending", []store.Event{started, {Kind: "generation_interrupted", AgentID: swarm.DefaultManagerID, MessageID: id, Text: `{"reason":"output_limit","recovery":"pending"}`}}, id},
		{"existing recovery claim", []store.Event{started, interrupted, {Kind: KindModelRetry, AgentID: swarm.DefaultManagerID, MessageID: id}}, id},
		{"durable stop before cancellation", []store.Event{started, interrupted, {Kind: KindUserStopRequested}}, id},

		{"implemented plan", []store.Event{{Kind: KindPlanImplemented}, started, interrupted}, id},
		{"unrelated auxiliary generation", []store.Event{{Kind: "generation_started", AgentID: TitleAgentID}, started}, id},
		{"current generation tool effect", []store.Event{started, {Kind: swarm.NotifyToolResult.String(), AgentID: swarm.DefaultManagerID, MessageID: id}, interrupted}, id},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if reasoningOnlyInterruption(tc.messages, tc.match) {
				t.Fatal("incomplete or already used receipts admitted a recovery")
			}
		})
	}
}

// Even with a known capped output, recovery needs a live owner and readable
// proof. A provider replacement or unreadable receipt log cannot grant it.
func TestReasoningOutputRecoveryNeedsALiveChatTurnAndReadableProof(t *testing.T) {
	e := newTestEngineAt(t, t.TempDir())
	e.cfg.Swarm.RecoverReasoningOutputLimit = true
	thread, err := e.CreateThread("Recovery admission fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	p := &e.cfg.Models.Providers[0]
	turn := &store.Turn{ThreadID: thread.ID, ProviderID: p.ID}
	rt := e.runtimeFor(thread.ID)
	capped := &swarm.ReasoningOutputLimit{MessageID: "capped-output"}
	if rt.claimReasoningOutputRecovery(context.Background(), turn, thread, capped) {
		t.Fatal("an idle runtime admitted recovery")
	}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	rt.mu.Lock()
	rt.running, rt.turnID = true, turn.ID
	rt.mu.Unlock()
	t.Cleanup(func() {
		rt.mu.Lock()
		rt.running = false
		rt.mu.Unlock()
	})
	p.APIMode = config.APIModeResponses
	if rt.claimReasoningOutputRecovery(context.Background(), turn, thread, capped) {
		t.Fatal("a Responses provider admitted Chat Completions recovery")
	}
	p.APIMode = config.APIModeChatCompletions
	var faults atomic.Int32
	if err := e.store.DB().Callback().Query().Before("gorm:query").Register("refuse_output_limit_receipt_read", func(tx *gorm.DB) {
		if tx.Statement.Table == "events" {
			faults.Add(1)
			_ = tx.AddError(errors.New("fixture output-limit evidence read failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if rt.claimReasoningOutputRecovery(context.Background(), turn, thread, capped) || faults.Load() != 1 {
		t.Fatalf("unreadable proof admitted recovery or missed the query: faults=%d", faults.Load())
	}
}
