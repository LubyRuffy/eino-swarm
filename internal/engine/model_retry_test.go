package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestIsRetryableModelErrorMatchesATruncatedToolJSON(t *testing.T) {
	err := errors.New("[NodeRunError] error, status code: 400, status: 400 Bad Request, message: Unterminated string starting at: line 1 column 54 (char 53)\nnode path: [node_1, ChatModel]")
	if !isRetryableModelError(err) {
		t.Fatal("truncated tool JSON is a retry, not a blocked objective")
	}
}

func TestIsRetryableModelErrorMatchesTransientProviderFailures(t *testing.T) {
	for _, msg := range []string{
		"error, status code: 429, status: 429 Too Many Requests, message: rate limited",
		"error, status code: 500, status: 500 Internal Server Error, message: upstream",
		"error, status code: 502, status: 502 Bad Gateway, message: proxy",
		"error, status code: 503, status: 503 Service Unavailable, message: busy",
		"error, status code: 529, status: 529, message: overloaded",
		"failed to receive stream chunk: unexpected EOF",
		"read: connection reset by peer",
	} {
		if !isRetryableModelError(errors.New(msg)) {
			t.Fatalf("must retry: %s", msg)
		}
	}
}

func TestIsRetryableModelErrorLeavesARealRefusalAlone(t *testing.T) {
	for _, msg := range []string{
		"chat model refused the request",
		"error, status code: 401, status: 401 Unauthorized, message: invalid api key",
		"error, status code: 403, status: 403 Forbidden, message: no access",
		"error, status code: 400, status: 400 Bad Request, message: maximum context length exceeded",
	} {
		if isRetryableModelError(errors.New(msg)) {
			t.Fatalf("must not retry: %s", msg)
		}
	}
	if isRetryableModelError(nil) || isRetryableModelError(context.Canceled) {
		t.Fatal("nil and cancel are not retries")
	}
	if isRetryableModelError(limitStopError{Used: 200}) {
		t.Fatal("a declined cap is not a model retry")
	}
}

func TestModelRetryCueStaysGeneric(t *testing.T) {
	for _, leak := range []string{
		"edit", "patch", "file_path", "shell_confirm", "Begin Patch",
		"notes.md", "NodeRunError", "ChatModel",
	} {
		if strings.Contains(modelRetryCue, leak) || strings.Contains(modelRetryToolResult, leak) {
			t.Fatalf("%q leaked into the retry protocol", leak)
		}
	}
	if !strings.Contains(strings.ToLower(modelRetryCue), "json") {
		t.Fatal("the model has to be told the last call was invalid JSON")
	}
}

func TestAppendModelRetryCueIsIdempotent(t *testing.T) {
	in := []adk.Message{schema.UserMessage("start the work")}
	once := appendModelRetryCue(in)
	if len(once) != 2 || once[1] == nil || once[1].Content != modelRetryCue {
		t.Fatalf("got %+v", once)
	}
	twice := appendModelRetryCue(once)
	if len(twice) != 2 {
		t.Fatalf("a second cue would look like the human repeated themselves: %+v", twice)
	}
	if appendModelRetryCue(nil)[0].Content != modelRetryCue {
		t.Fatal("empty transcript still needs the cue")
	}
}

func TestCloseDanglingManagerToolsLeavesWorkersAndAskUser(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID,
		Text: "edit({})", ToolCallID: "c-edit",
	})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID,
		Text: ToolAskUser + "({})", ToolCallID: "c-ask",
	})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolCall.String(), AgentID: "worker-1",
		Text: "exec({})", ToolCallID: "c-w",
	})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolCall.String(), AgentID: swarm.DefaultManagerID,
		Text: "read({})", ToolCallID: "c-read",
	})
	e.record(store.Event{
		ThreadID: th.ID, TurnID: turn.ID,
		Kind: swarm.NotifyToolResult.String(), AgentID: swarm.DefaultManagerID,
		Text: "the file body", ToolCallID: "c-read",
	})

	e.closeDanglingManagerTools(turn)
	e.closeDanglingManagerTools(turn)

	events, err := e.Store().ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	results := resultsByCall(events)
	if got := results["c-edit"]; got.Err != modelRetryToolResult || got.Text != modelRetryToolResult {
		t.Fatalf("the truncated manager call must un-pend: %+v", got)
	}
	if _, ok := results["c-ask"]; ok {
		t.Fatal("ask_user is waiting for the human; closing it swallows the question")
	}
	if _, ok := results["c-w"]; ok {
		t.Fatal("workers are still live; a manager retry must not kill their tools")
	}
	if got := results["c-read"]; got.Text != "the file body" || got.Err != "" {
		t.Fatalf("a finished call was rewritten: %+v", got)
	}
	if n := countCallResults(events, "c-edit"); n != 1 {
		t.Fatalf("closing twice duplicated the result: %d", n)
	}
}

func TestModelRetryNoticeHidesThePayload(t *testing.T) {
	if got := modelRetryNotice(`{"attempt":1,"cap":2}`); got != modelRetryNoticeText {
		t.Fatalf("got %q", got)
	}
	if modelRetryNotice("junk") != modelRetryNoticeText {
		t.Fatal("malformed payload still has to be a readable row")
	}
}

func TestKindModelRetryIsStable(t *testing.T) {
	if KindModelRetry != "model_retry" {
		t.Fatalf("renaming model_retry breaks stored transcripts: %q", KindModelRetry)
	}
	if modelErrorRetries < 1 {
		t.Fatal("zero retries is the bug this file exists to stop")
	}
}

func TestRecoverManagerMessagesDropsATruncatedToolCall(t *testing.T) {
	asst := &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
		ID: "c-edit", Function: schema.FunctionCall{Name: "edit", Arguments: `{"file_path":"`},
	}}}
	in := []adk.Message{
		schema.UserMessage("start the work"),
		asst,
	}
	out := recoverManagerMessages(swarm.RunResult{Transcript: in}, nil, nil, nil)
	for _, m := range out {
		if m == nil {
			continue
		}
		if m.Role == schema.Assistant && len(m.ToolCalls) > 0 {
			t.Fatalf("invalid tool JSON must not go back to the model: %+v", m.ToolCalls)
		}
		if m.Role == schema.Tool {
			t.Fatalf("a synthetic tool result next to invalid arguments 400s again: %+v", m)
		}
	}
	if len(out) == 0 || out[len(out)-1].Content != modelRetryCue {
		t.Fatalf("the model needs the cue: %+v", out)
	}
}

func TestRecoverManagerMessagesDropsInvalidJSONEvenWithAResult(t *testing.T) {
	asst := &schema.Message{Role: schema.Assistant, ToolCalls: []schema.ToolCall{{
		ID: "c-edit", Function: schema.FunctionCall{Name: "edit", Arguments: `{"file_path":"`},
	}}}
	in := []adk.Message{
		schema.UserMessage("start the work"),
		asst,
		schema.ToolMessage("error: could not parse", "c-edit"),
	}
	out := recoverManagerMessages(swarm.RunResult{Transcript: in}, nil, nil, nil)
	for _, m := range out {
		if m != nil && m.Role == schema.Assistant && len(m.ToolCalls) > 0 {
			t.Fatalf("GLM re-parses arguments; truncated JSON must not stay: %+v", m.ToolCalls)
		}
	}
}

func TestARetryableModelErrorRetriesInsteadOfBlockingTheGoal(t *testing.T) {
	unterminated := errors.New("[NodeRunError] error, status code: 400, status: 400 Bad Request, message: Unterminated string starting at: line 1 column 54 (char 53)\nnode path: [node_1, ChatModel]")
	provider.SetMockFailTimes(1, unterminated)
	t.Cleanup(func() { provider.SetMockFailure(nil) })

	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 8
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "start the work")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	if got.Status == store.TurnError {
		t.Fatalf("truncated tool JSON must be retried, status=%s err=%s", got.Status, got.Error)
	}
	waitSettled(t, e, th.ID)
	th, err = e.Store().GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if th.GoalBlocked {
		t.Fatal("a retried model error must not block the objective")
	}
	if !hasKind(t, e, th.ID, KindModelRetry) {
		t.Fatal("the retry must be in the trace")
	}
}

func TestShouldPursueAfterTurnKeepsARecoverableFailureMoving(t *testing.T) {
	unterminated := errors.New("[NodeRunError] error, status code: 400, status: 400 Bad Request, message: Unterminated string starting at: line 1 column 66 (char 65)\nnode path: [node_1, ChatModel]")
	if !shouldPursueAfterTurn(store.TurnDone, nil, true) {
		t.Fatal("a clean finish must keep pursuing")
	}
	if !shouldPursueAfterTurn(store.TurnDone, nil, false) {
		t.Fatal("a clean finish without a goal still flushes follow-ups")
	}
	if !shouldPursueAfterTurn(store.TurnError, unterminated, true) {
		t.Fatal("truncated tool JSON must auto-continue, not pin the banner")
	}
	if shouldPursueAfterTurn(store.TurnError, unterminated, false) {
		t.Fatal("without a standing objective a failed turn must not auto-start")
	}
	if shouldPursueAfterTurn(store.TurnError, errors.New("chat model refused the request"), true) {
		t.Fatal("a real refusal must still block")
	}
	if shouldPursueAfterTurn(store.TurnCancelled, unterminated, true) {
		t.Fatal("Stop is a pause, not an auto-continue")
	}
	if shouldPursueAfterTurn(store.TurnError, nil, true) {
		t.Fatal("an error without a recoverable cause must not look like a retry")
	}
}

func TestARetryableModelErrorContinuesTheGoalAfterRetriesAreExhausted(t *testing.T) {
	provider.SetCompleteOpenGoal(false)
	t.Cleanup(func() { provider.SetCompleteOpenGoal(true) })
	unterminated := errors.New("[NodeRunError] error, status code: 400, status: 400 Bad Request, message: Unterminated string starting at: line 1 column 66 (char 65)\nnode path: [node_1, ChatModel]")
	provider.SetMockFailure(unterminated)
	t.Cleanup(func() { provider.SetMockFailure(nil) })

	e := newTestEngine(t)
	e.Config().Swarm.GoalMaxAutoTurns = 8
	th, _ := e.CreateThread("", "", "")
	if err := e.SetThreadGoal(th.ID, "keep going"); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(th.ID, "start the work")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	if got.Status != store.TurnError {
		t.Fatalf("status=%s err=%s", got.Status, got.Error)
	}
	waitSettled(t, e, th.ID)
	th, err = e.Store().GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if th.GoalBlocked {
		t.Fatal("truncated tool JSON is a retry, not a stuck objective")
	}
	for _, leak := range []string{"NodeRunError", "ChatModel", "node path"} {
		if strings.Contains(got.Error, leak) || strings.Contains(th.GoalBlockReason, leak) {
			t.Fatalf("the graph dump leaked into the banner: turn=%q reason=%q", got.Error, th.GoalBlockReason)
		}
	}
	events, err := e.Store().ListEvents(th.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, ev := range events {
		if ev.Kind == KindModelRetry && ev.TurnID == turn.ID {
			n++
		}
	}
	if n != modelErrorRetries {
		t.Fatalf("retry events on the failed turn=%d want %d", n, modelErrorRetries)
	}
	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) < 2 && !th.GoalIdle && !th.GoalCapped {
		t.Fatalf("the objective must auto-continue or hold, turns=%d idle=%v capped=%v", len(turns), th.GoalIdle, th.GoalCapped)
	}
}

func TestARetryableModelErrorWithoutAGoalDoesNotAutoStart(t *testing.T) {
	unterminated := errors.New("[NodeRunError] error, status code: 400, status: 400 Bad Request, message: Unterminated string starting at: line 1 column 66 (char 65)\nnode path: [node_1, ChatModel]")
	provider.SetMockFailure(unterminated)
	t.Cleanup(func() { provider.SetMockFailure(nil) })

	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurn(th.ID, "start the work")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	if got.Status != store.TurnError {
		t.Fatalf("status=%s err=%s", got.Status, got.Error)
	}
	waitSettled(t, e, th.ID)
	turns, err := e.Store().ListTurns(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("a failed turn without a standing objective must not auto-start, got %d", len(turns))
	}
}
