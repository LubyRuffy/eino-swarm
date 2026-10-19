package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// The tool_call is already visible to clients before its waiter is armed.
// An acknowledged answer in that window must survive an actual store reopen.
func TestRestartReplaysAnAnswerAcceptedBeforeAskWaiterArms(t *testing.T) {
	e, endpoint := newRestartReplayEngine(t)
	thread, err := e.CreateThread("answer replay", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, thread.ID, "continue the requested work")
	args := restartReplayAskArguments(t, "decision")
	history := []*schema.Message{schema.UserMessage(turn.UserText), restartReplayAskMessage("ask-acknowledged", args)}
	restartReplayRuntime(t, e, turn, history, func(ctx context.Context, _ *runtime, _ *swarm.Registry) { <-ctx.Done() })
	recordRestartReplayAsk(e, turn, "ask-acknowledged", args)
	if e.Status(thread.ID).AwaitingAnswer {
		t.Fatal("fixture must answer before the waiter is armed")
	}
	if err := e.AnswerTurn(thread.ID, "ask-acknowledged", AskAnswers{"decision": {Answers: []string{"one"}}}); err != nil {
		t.Fatal(err)
	}
	next := coldRestartReplayEngine(t, e)
	checkpoint := next.checkpointForTurn(turn.ID)
	if checkpoint == nil || checkpoint.Answer == nil || checkpoint.Answer.CallID != "ask-acknowledged" ||
		len(checkpoint.Answer.Answers["decision"].Answers) != 1 || checkpoint.Answer.Answers["decision"].Answers[0] != "one" {
		t.Fatalf("acknowledged answer was not durably saved: %+v", checkpoint)
	}
	if n, err := next.ResumeOrphanedTurns(); err != nil || n != 1 {
		t.Fatalf("resume=%d err=%v", n, err)
	}
	finished := waitRestartReplayTurn(t, next, turn.ID)
	if finished.Status != store.TurnDone || finished.ID != turn.ID || finished.Final != restartReplayFinal {
		t.Fatalf("acknowledged answer did not complete its original turn: %+v", finished)
	}
	assertRestartReplayAskResult(t, next, turn.ID, "ask-acknowledged", "decision", "one")
	request := endpoint.onlyRequest(t)
	assertRestartReplayToolPair(t, request, "ask-acknowledged", "decision", "one")
	if err := next.AnswerTurn(thread.ID, "ask-acknowledged", AskAnswers{"decision": {Answers: []string{"two"}}}); !errors.Is(err, ErrIdle) {
		t.Fatalf("finished question accepted another answer: %v", err)
	}
}

// Returning the answer to the waiter is not a durable tool result. Handoff in
// that gap must retain the answer just as it does before the waiter consumes it.
func TestRestartReplaysAnAcceptedAnswerBeforeToolResultPersistence(t *testing.T) {
	e, endpoint := newRestartReplayEngine(t)
	thread, err := e.CreateThread("answer persistence boundary", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, thread.ID, "continue after the accepted decision")
	args := restartReplayAskArguments(t, "decision")
	recordRestartReplayAsk(e, turn, "ask-consumed", args)
	startWaiter := make(chan struct{})
	consumed := make(chan string, 1)
	restartReplayRuntime(t, e, turn, []*schema.Message{schema.UserMessage(turn.UserText), restartReplayAskMessage("ask-consumed", args)},
		func(ctx context.Context, rt *runtime, _ *swarm.Registry) {
			select {
			case <-startWaiter:
			case <-ctx.Done():
				return
			}
			result, runErr := AskUserTool(rt.waitAsk).(tool.InvokableTool).InvokableRun(ctx, args)
			if runErr != nil {
				t.Errorf("consuming accepted answer: %v", runErr)
				return
			}
			consumed <- result
			<-ctx.Done()
		})
	if err := e.AnswerTurn(thread.ID, "ask-consumed", AskAnswers{"decision": {Answers: []string{"two"}}}); err != nil {
		t.Fatal(err)
	}
	close(startWaiter)
	select {
	case result := <-consumed:
		assertRestartReplayAnswer(t, result, "decision", "two")
	case <-time.After(5 * time.Second):
		t.Fatal("waiter did not consume the early acknowledged answer")
	}
	next := coldRestartReplayEngine(t, e)
	if n, err := next.ResumeOrphanedTurns(); err != nil || n != 1 {
		t.Fatalf("resume=%d err=%v", n, err)
	}
	if finished := waitRestartReplayTurn(t, next, turn.ID); finished.Status != store.TurnDone || finished.Final != restartReplayFinal {
		t.Fatalf("consuming the answer lost it before durable persistence: %+v", finished)
	}
	assertRestartReplayAskResult(t, next, turn.ID, "ask-consumed", "decision", "two")
	assertRestartReplayToolPair(t, endpoint.onlyRequest(t), "ask-consumed", "decision", "two")
}

// Acceptance belongs to one call, even when the same turn asks another question.
// A restart after that second waiter arms must still wait for its own answer.
func TestRestartDoesNotApplyAnEarlierAcceptedAnswerToANewQuestion(t *testing.T) {
	e, endpoint := newRestartReplayEngine(t)
	thread, err := e.CreateThread("successive questions", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, thread.ID, "continue after both decisions")
	firstArgs, secondArgs := restartReplayAskArguments(t, "first"), restartReplayAskArguments(t, "second")
	firstHistory := []*schema.Message{schema.UserMessage(turn.UserText), restartReplayAskMessage("ask-first", firstArgs)}
	firstResult := make(chan string, 1)
	startSecond := make(chan struct{})
	recordRestartReplayAsk(e, turn, "ask-first", firstArgs)
	restartReplayRuntime(t, e, turn, firstHistory, func(ctx context.Context, rt *runtime, reg *swarm.Registry) {
		result, runErr := AskUserTool(rt.waitAsk).(tool.InvokableTool).InvokableRun(ctx, firstArgs)
		if runErr != nil {
			t.Errorf("first ask: %v", runErr)
			return
		}
		e.record(store.Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: swarm.NotifyToolResult.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "ask-first", Text: result})
		firstResult <- result
		select {
		case <-startSecond:
		case <-ctx.Done():
			return
		}
		history := append(append([]*schema.Message(nil), firstHistory...), schema.ToolMessage(result, "ask-first"), restartReplayAskMessage("ask-second", secondArgs))
		reg.SetHistory(history)
		recordRestartReplayAsk(e, turn, "ask-second", secondArgs)
		result, runErr = AskUserTool(rt.waitAsk).(tool.InvokableTool).InvokableRun(ctx, secondArgs)
		if runErr != nil {
			t.Errorf("second ask: %v", runErr)
			return
		}
		e.record(store.Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: swarm.NotifyToolResult.String(), AgentID: swarm.DefaultManagerID, ToolCallID: "ask-second", Text: result})
	})
	waitRestartState(t, func() bool { return e.Status(thread.ID).AwaitingAnswer })
	if err := e.AnswerTurn(thread.ID, "ask-first", AskAnswers{"first": {Answers: []string{"one"}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-firstResult:
		if !strings.Contains(result, `"one"`) {
			t.Fatalf("first waiter did not receive the accepted answer: %s", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first waiter did not return")
	}
	close(startSecond)
	waitRestartState(t, func() bool {
		return e.Status(thread.ID).AwaitingAnswer && e.runtimeFor(thread.ID).pendingAskQuestions("ask-second") != nil
	})
	if err := e.AnswerTurn(thread.ID, "ask-first", AskAnswers{"first": {Answers: []string{"two"}}}); !errors.Is(err, ErrAskMismatch) {
		t.Fatalf("old call accepted an answer intended for the new question: %v", err)
	}
	next := coldRestartReplayEngine(t, e)
	if checkpoint := next.checkpointForTurn(turn.ID); checkpoint == nil || checkpoint.Answer != nil || checkpoint.Pause != "ask" {
		t.Fatalf("a previous answer contaminated the next question's checkpoint: %+v", checkpoint)
	}
	if n, err := next.ResumeOrphanedTurns(); err != nil || n != 1 {
		t.Fatalf("resume=%d err=%v", n, err)
	}
	waitRestartState(t, func() bool { return next.Status(thread.ID).AwaitingAnswer || !next.Status(thread.ID).Running })
	if !next.Status(thread.ID).AwaitingAnswer || len(endpoint.requests()) != 0 {
		t.Fatal("the second question continued without its own human answer")
	}
	if err := next.AnswerTurn(thread.ID, "ask-first", AskAnswers{"first": {Answers: []string{"two"}}}); !errors.Is(err, ErrAskMismatch) {
		t.Fatalf("recovered question accepted a stale answer: %v", err)
	}
	if err := next.AnswerTurn(thread.ID, "ask-second", AskAnswers{"second": {Answers: []string{"two"}}}); err != nil {
		t.Fatal(err)
	}
	if finished := waitRestartReplayTurn(t, next, turn.ID); finished.Status != store.TurnDone || finished.Final != restartReplayFinal {
		t.Fatalf("the second answer did not complete the recovered turn: %+v", finished)
	}
	assertRestartReplayAskResult(t, next, turn.ID, "ask-first", "first", "one")
	assertRestartReplayAskResult(t, next, turn.ID, "ask-second", "second", "two")
	request := endpoint.onlyRequest(t)
	assertRestartReplayToolPair(t, request, "ask-first", "first", "one")
	assertRestartReplayToolPair(t, request, "ask-second", "second", "two")
}

const restartReplayFinal = "The recovered decisions and unread messages were applied."

type restartReplayMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content"`
	ToolCallID string            `json:"tool_call_id"`
	ToolCalls  []schema.ToolCall `json:"tool_calls"`
}

type restartReplayEndpoint struct {
	mu    sync.Mutex
	calls [][]restartReplayMessage
}

func (f *restartReplayEndpoint) requests() [][]restartReplayMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]restartReplayMessage(nil), f.calls...)
}

func (f *restartReplayEndpoint) onlyRequest(t *testing.T) []restartReplayMessage {
	t.Helper()
	calls := f.requests()
	if len(calls) != 1 {
		t.Fatalf("manager HTTP requests=%d, want one recovery request", len(calls))
	}
	return calls[0]
}

func newRestartReplayEngine(t *testing.T) (*Engine, *restartReplayEndpoint) {
	t.Helper()
	f := new(restartReplayEndpoint)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected fixture request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var request struct {
			Messages []restartReplayMessage `json:"messages"`
			Stream   bool                   `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.calls = append(f.calls, request.Messages)
		f.mu.Unlock()
		writeContextFlowReply(w, request.Stream, map[string]any{"content": restartReplayFinal}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	t.Setenv("ZWAI_MODEL_BASE_URL", "")
	t.Setenv("ZWAI_MODEL_API_KEY", "")
	t.Setenv("ZWAI_MODEL_NAME", "")
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Default = config.DefaultProviderID
	cfg.Models.Providers = []config.Provider{{ID: config.DefaultProviderID, BaseURL: endpoint.URL, Model: "fixture-model", ContextWindow: 128000}}
	cfg.Swarm.AutoTitle = false
	cfg.Memory.Enabled, cfg.Memory.AutoReview = false, false
	return openRestartReplayEngine(t, cfg), f
}

func openRestartReplayEngine(t *testing.T, cfg *config.Config) *Engine {
	t.Helper()
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	e := New(cfg, st, provider.New(cfg), slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(e.Shutdown)
	return e
}

func coldRestartReplayEngine(t *testing.T, previous *Engine) *Engine {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if status, err := previous.PrepareRestart(ctx); err != nil || !status.Ready {
		t.Fatalf("prepare=%+v err=%v", status, err)
	}
	previous.Shutdown()
	if err := previous.Store().Close(); err != nil {
		t.Fatal(err)
	}
	return openRestartReplayEngine(t, previous.cfg)
}

func restartReplayRuntime(t *testing.T, e *Engine, turn *store.Turn, history []*schema.Message, run func(context.Context, *runtime, *swarm.Registry)) *swarm.Registry {
	t.Helper()
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	reg.SetHistory(history)
	ctx, cancel := context.WithCancel(context.Background())
	idle := make(chan struct{})
	rt := e.runtimeFor(turn.ThreadID)
	if !rt.occupy(reg, cancel, turn.ID, idle) {
		cancel()
		t.Fatal("could not claim fixture turn")
	}
	go func() {
		defer rt.release(cancel, idle, turn.ID)
		run(ctx, rt, reg)
	}()
	return reg
}

func restartReplayAskArguments(t *testing.T, id string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"questions": []AskQuestion{{ID: id, Prompt: "Choose the next step", Options: []AskOption{{ID: "one", Label: "One"}, {ID: "two", Label: "Two"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func restartReplayAskMessage(id, args string) *schema.Message {
	return schema.AssistantMessage("", []schema.ToolCall{{ID: id, Function: schema.FunctionCall{Name: ToolAskUser, Arguments: args}}})
}

func recordRestartReplayAsk(e *Engine, turn *store.Turn, id, args string) {
	e.record(store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID, ToolCallID: id, Text: ToolAskUser + "(" + args + ")"})
}

func waitRestartReplayTurn(t *testing.T, e *Engine, id string) *store.Turn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		turn, err := e.Store().GetTurn(id)
		if err != nil {
			t.Fatal(err)
		}
		if turn.Status != store.TurnRunning {
			return turn
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("recovered turn did not finish; this helper never answers a question for the test")
	return nil
}

func assertRestartReplayAskResult(t *testing.T, e *Engine, turnID, callID, questionID, answer string) {
	t.Helper()
	events, err := e.Store().ListTurnEvents(turnID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range events {
		if event.Kind == swarm.NotifyToolResult.String() && event.ToolCallID == callID {
			count++
			assertRestartReplayAnswer(t, event.Text, questionID, answer)
		}
	}
	if count != 1 {
		t.Fatalf("call %s has %d durable tool results, want one", callID, count)
	}
}

func assertRestartReplayToolPair(t *testing.T, request []restartReplayMessage, callID, questionID, answer string) {
	t.Helper()
	calls, results := 0, 0
	for _, message := range request {
		for _, call := range message.ToolCalls {
			if call.ID == callID && call.Function.Name == ToolAskUser {
				calls++
			}
		}
		if message.Role == string(schema.Tool) && message.ToolCallID == callID {
			results++
			assertRestartReplayAnswer(t, message.Content, questionID, answer)
		}
	}
	if calls != 1 || results != 1 {
		t.Fatalf("recovered model input has call/result %d/%d for %s, want 1/1", calls, results, callID)
	}
}

func assertRestartReplayAnswer(t *testing.T, result, questionID, answer string) {
	t.Helper()
	var body struct {
		Answers AskAnswers `json:"answers"`
	}
	if err := json.Unmarshal([]byte(result), &body); err != nil {
		t.Fatal(err)
	}
	got := body.Answers[questionID].Answers
	if len(body.Answers) != 1 || len(got) != 1 || got[0] != answer {
		t.Fatalf("answer for %s=%s, want only %q", questionID, result, answer)
	}
}
