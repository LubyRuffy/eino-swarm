package engine

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/workerstate"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// Exercise the real middleware registration, ADK graph and HTTP adapter: calling
// the hook by hand cannot prove that a rejected replay never reaches a gateway.
func TestToolJSONGuardStopsInvalidReplayBeforeHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, args      string
		steer, newHuman bool
		wantHTTP        int
	}{
		{name: "truncated tail", args: `{"path":"`, wantHTTP: 0},
		{name: "valid tail", args: `{"path":"workspace"}`, wantHTTP: 1},
		{name: "truncated tail before steer", args: `{"path":"`, steer: true, wantHTTP: 0},
		{name: "old truncation before new human request", args: `{"path":"`, newHuman: true, wantHTTP: 1},
		{name: "empty arguments remain tool validation", wantHTTP: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, wire := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) { return "The requested check finished.", nil })
			thread, err := e.CreateThread("", "", "")
			if err != nil {
				t.Fatal(err)
			}
			turn := &store.Turn{ThreadID: thread.ID}
			if err := e.store.CreateTurn(turn); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			reg := newToolJSONRegistry(t, e, ctx)
			messages := toolJSONReplay(tc.args)
			if tc.steer {
				messages = append(messages, schema.UserMessage("[steer] Preserve this human correction."))
			}
			if tc.newHuman {
				messages = append(messages, schema.UserMessage("Check a new request."))
			}
			_, runErr := reg.RunWith(ctx, swarm.RunConfig{
				Instruction: "Coordinate the requested work.", Messages: messages, MaxIterations: 3,
				ManagerMiddlewares: e.autoCompactHandlers(thread.ID, turn.ID, thread, false),
			}, nil)
			requests := wire.snapshot()
			t.Logf("HTTP requests=%d, run error=%v", len(requests), runErr)
			if len(requests) != tc.wantHTTP {
				t.Fatalf("invalid replay crossed the ADK/HTTP boundary: requests=%d want=%d err=%v", len(requests), tc.wantHTTP, runErr)
			}
			if tc.wantHTTP == 0 {
				if runErr == nil || !strings.Contains(runErr.Error(), "NodeRunError") || !isRetryableModelError(runErr) || modelFailureClass(runErr) != modelRetryReasonInvalidToolJSON {
					t.Fatalf("ADK refusal lost the bounded retry classification: %v", runErr)
				}
				return
			}
			if runErr != nil {
				t.Fatalf("allowed replay did not finish through the HTTP adapter: %v", runErr)
			}
			var request struct {
				Messages []*schema.Message `json:"messages"`
			}
			if err := json.Unmarshal(requests[0], &request); err != nil {
				t.Fatal(err)
			}
			if !hasToolResult(request.Messages, "replayed-call") {
				t.Fatal("the guard dropped an allowed tool result")
			}
			assertToolJSONRoster(t, request.Messages)
		})
	}
}

func TestToolJSONGuardUsesTheCompactedStateInTheHTTPChain(t *testing.T) {
	e, wire := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) { return "The requested check finished.", nil })
	thread, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	const summary = "The previous inspection is complete."
	if err := e.store.UpdateThread(thread.ID, map[string]any{"session_memory": summary}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reg := newToolJSONRegistry(t, e, ctx)
	messages := append([]adk.Message{schema.UserMessage("old inspection"), schema.AssistantMessage("old-answer-must-fold", nil)}, toolJSONReplay(`{"path":"workspace"}`)...)
	middlewares := e.autoCompactHandlers(thread.ID, "compact-run", thread, true)
	middlewares[0].(*autoCompact).keep = 3
	_, err = reg.RunWith(ctx, swarm.RunConfig{
		Instruction: "Coordinate the requested work.", Messages: messages, MaxIterations: 3,
		ManagerMiddlewares: middlewares,
	}, nil)
	requests := wire.snapshot()
	if err != nil || len(requests) != 1 {
		t.Fatalf("compaction/guard chain requests=%d err=%v", len(requests), err)
	}
	if !strings.Contains(string(requests[0]), summary) || strings.Contains(string(requests[0]), "old-answer-must-fold") {
		t.Fatalf("the next middleware/model received the stale pre-compaction state: %s", requests[0])
	}
}

func TestToolJSONGuardRecoveryKeepsSteerAndWorkerState(t *testing.T) {
	e, wire := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) { return "The requested check finished.", nil })
	thread, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, thread.ID, "Continue the requested check.")
	plantFinishedWorker(t, e, thread.ID, turn.ID, "historical-worker", "reader", "verified")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reg := newToolJSONRegistry(t, e, ctx)
	set := buildTestToolset(t, e, thread.ID)
	messages := append(toolJSONReplay(`{"path":"`), schema.UserMessage("[steer] Preserve this human correction."))
	acc := newAccumulator(e, thread.ID, turn.ID, 0)
	_, err = e.runtimeFor(thread.ID).runManager(ctx, turn, reg, acc, set, nil, messages)
	requests := wire.snapshot()
	if err != nil || len(requests) != 1 {
		t.Fatalf("one rejected replay must recover with one clean HTTP call: requests=%d err=%v", len(requests), err)
	}
	var request struct {
		Messages []*schema.Message `json:"messages"`
	}
	if err := json.Unmarshal(requests[0], &request); err != nil {
		t.Fatal(err)
	}
	if hasToolResult(request.Messages, "replayed-call") || hasToolCall(request.Messages, "inspect") || !strings.Contains(string(requests[0]), "Preserve this human correction.") {
		t.Fatalf("recovery kept invalid tool replay or lost human steering: %s", requests[0])
	}
	assertToolJSONRoster(t, request.Messages)
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	retries := 0
	for _, event := range events {
		if event.Kind != KindModelRetry {
			continue
		}
		retries++
		if event.Text != modelRetryEventText(1, modelRetryReasonInvalidToolJSON) {
			t.Fatalf("the guard retry lost its turn-bound diagnostic: %+v", event)
		}
	}
	if retries != 1 || retries > modelErrorRetries {
		t.Fatalf("retry attempts=%d cap=%d", retries, modelErrorRetries)
	}
}

func newToolJSONRegistry(t *testing.T, e *Engine, ctx context.Context) *swarm.Registry {
	t.Helper()
	p := e.cfg.Models.Providers[0]
	builder, err := e.pool.ModelBuilder(ctx, p.ID, p.Model, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	reg := swarm.NewRegistry()
	reg.ModelBuilder = builder
	t.Cleanup(reg.Close)
	return reg
}

func toolJSONReplay(args string) []adk.Message {
	return []adk.Message{
		workerstate.New([]workerstate.Worker{{AgentID: "historical-worker", Role: "reader", Status: workerstate.Done}}),
		schema.UserMessage("Continue the requested work."),
		schema.AssistantMessage("", []schema.ToolCall{{ID: "replayed-call", Type: "function", Function: schema.FunctionCall{Name: "inspect", Arguments: args}}}),
		schema.ToolMessage("The prior inspection returned.", "replayed-call"),
	}
}

func assertToolJSONRoster(t *testing.T, messages []adk.Message) {
	t.Helper()
	count := 0
	for _, message := range messages {
		if roster, ok := workerstate.Parse(message); ok {
			count++
			if !reflect.DeepEqual(roster.Workers, []workerstate.Worker{{AgentID: "historical-worker", Role: "reader", Status: workerstate.Done}}) {
				t.Fatalf("request changed restored worker identity/status: %+v", roster)
			}
		}
	}
	if count != 1 {
		t.Fatalf("restored worker snapshots=%d want=1", count)
	}
}

func TestInvalidTrailingToolCallChecksOnlyTheCurrentTail(t *testing.T) {
	invalid := toolJSONReplay(`{"path":"`)[2]
	valid := toolJSONReplay(`{"path":"workspace"}`)[2]
	for _, tc := range []struct {
		name     string
		messages []adk.Message
		want     int
	}{
		{name: "nil history", want: -1},
		{name: "nil messages", messages: []adk.Message{nil}, want: -1},
		{name: "only system", messages: []adk.Message{schema.SystemMessage("coordinate")}, want: -1},
		{name: "invalid tail", messages: []adk.Message{nil, invalid, schema.ToolMessage("refused", "replayed-call")}, want: 1},
		{name: "steer is not a new task", messages: []adk.Message{invalid, nil, schema.UserMessage("[steer] keep the correction")}, want: 0},
		{name: "new human task", messages: []adk.Message{invalid, schema.UserMessage("new task")}, want: -1},
		{name: "assistant conclusion", messages: []adk.Message{invalid, schema.AssistantMessage("finished", nil)}, want: -1},
		{name: "new valid call", messages: []adk.Message{invalid, valid}, want: -1},
		{name: "empty parameters", messages: toolJSONReplay(" "), want: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := invalidTrailingToolCall(tc.messages); got != tc.want {
				t.Fatalf("tail index=%d want=%d", got, tc.want)
			}
		})
	}
}
