package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// KindModelRetry is recorded when a pursuing (or any) manager run hits a
// recoverable ChatModel failure — truncated tool JSON, 429, a dropped
// stream — and the runtime re-enters the same turn instead of blocking.
const KindModelRetry = "model_retry"

// modelErrorRetries is how many times a recoverable model error may re-enter
// this turn. Past that the turn fails. An open /goal auto-continues instead
// of blocking; goal_max_auto_turns and goal_idle still stop a loop.
const modelErrorRetries = 2

const modelRetryNoticeText = "Retrying after a model error."

// modelRetryCue is what the model reads after a dropped generate. The failed
// tool call is stripped from the next request: sending truncated arguments
// back is how a 400 Unterminated string repeats.
const modelRetryCue = "The last model call failed before a tool could finish. If a tool call used invalid JSON arguments, retry with a smaller valid payload. Do not repeat the failed arguments."

// modelRetryToolResult un-pends the transcript row. It is not fed back as a
// tool message: that would keep the invalid arguments in the next request.
const modelRetryToolResult = "the model call failed before this tool finished"

func modelRetryNotice(string) string {
	return modelRetryNoticeText
}

func isRetryableModelError(err error) bool {
	if err == nil || isLimitStop(err) || isMaxIterations(err) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var idle provider.IdleTimeoutError
	if errors.As(err, &idle) {
		return true
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "context canceled") {
		return false
	}
	if strings.Contains(s, "status code: 401") || strings.Contains(s, "status code: 403") {
		return false
	}
	if strings.Contains(s, "invalid api key") || strings.Contains(s, "incorrect api key") {
		return false
	}
	switch {
	case strings.Contains(s, "unterminated string"),
		strings.Contains(s, "unexpected end of json"),
		strings.Contains(s, "invalid character"),
		strings.Contains(s, "unexpected eof"),
		strings.Contains(s, "connection reset"),
		strings.Contains(s, "status code: 429"),
		strings.Contains(s, "status code: 500"),
		strings.Contains(s, "status code: 502"),
		strings.Contains(s, "status code: 503"),
		strings.Contains(s, "status code: 504"),
		strings.Contains(s, "status code: 529"):
		return true
	}
	if strings.Contains(s, "status code: 400") &&
		(strings.Contains(s, "json") || strings.Contains(s, "parse") || strings.Contains(s, "syntax")) {
		return true
	}
	return false
}

func appendModelRetryCue(msgs []adk.Message) []adk.Message {
	if n := len(msgs); n > 0 {
		last := msgs[n-1]
		if last != nil && last.Role == schema.User && last.Content == modelRetryCue {
			return msgs
		}
	}
	return append(msgs, schema.UserMessage(modelRetryCue))
}

func recoverManagerMessages(res swarm.RunResult, fallback []adk.Message, extra []*schema.Message, results []store.Event) []adk.Message {
	msgs := stitchManagerToolResults(nextManagerMessages(res, fallback, extra), results)
	msgs = dropTrailingIncompleteToolCalls(msgs)
	msgs = dropInvalidTrailingToolJSON(msgs)
	return appendModelRetryCue(msgs)
}

// dropInvalidTrailingToolJSON removes a trailing assistant tool call whose
// arguments are not valid JSON, even when a tool_result already exists.
// GLM re-parses arguments on the next request; sending the truncated
// payload back is how a 400 Unterminated string repeats.
func dropInvalidTrailingToolJSON(msgs []adk.Message) []adk.Message {
	lastAsst := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i] == nil || isSteerUser(msgs[i]) {
			continue
		}
		if msgs[i].Role == schema.Assistant && len(msgs[i].ToolCalls) > 0 {
			lastAsst = i
			break
		}
		if msgs[i].Role == schema.User || (msgs[i].Role == schema.Assistant && len(msgs[i].ToolCalls) == 0) {
			return msgs
		}
	}
	if lastAsst < 0 {
		return msgs
	}
	valid := true
	for _, tc := range msgs[lastAsst].ToolCalls {
		args := strings.TrimSpace(tc.Function.Arguments)
		if args == "" {
			continue
		}
		if !json.Valid([]byte(args)) {
			valid = false
			break
		}
	}
	if valid {
		return msgs
	}
	out := append([]adk.Message{}, msgs[:lastAsst]...)
	for _, m := range msgs[lastAsst+1:] {
		if isSteerUser(m) {
			out = append(out, m)
		}
	}
	return out
}

func (e *Engine) closeDanglingManagerTools(turn *store.Turn) {
	if e == nil || turn == nil {
		return
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		return
	}
	for _, c := range orphanedToolCalls(events) {
		if isAskUserToolText(c.Text) {
			continue
		}
		agent := strings.TrimSpace(c.AgentID)
		if agent != "" && agent != swarm.DefaultManagerID {
			continue
		}
		if agent == "" {
			agent = swarm.DefaultManagerID
		}
		e.record(store.Event{
			ThreadID:   turn.ThreadID,
			TurnID:     turn.ID,
			Kind:       swarm.NotifyToolResult.String(),
			AgentID:    agent,
			Role:       c.Role,
			ToolCallID: c.ToolCallID,
			Text:       modelRetryToolResult,
			Err:        modelRetryToolResult,
		})
	}
}

func (rt *runtime) retryManagerRun(turn *store.Turn, res swarm.RunResult, fallback []adk.Message, reg *swarm.Registry) []adk.Message {
	msgs := recoverManagerMessages(res, fallback, reg.TakePendingSteerMessages(), rt.engine.managerToolResults(turn.ID))
	rt.engine.closeDanglingManagerTools(turn)
	return msgs
}

func (rt *runtime) recordModelRetry(turn *store.Turn, attempt int) {
	rt.engine.record(store.Event{
		ThreadID: rt.threadID, TurnID: turn.ID,
		Kind: KindModelRetry, AgentID: swarm.DefaultManagerID,
		Text: fmt.Sprintf(`{"attempt":%d,"cap":%d}`, attempt, modelErrorRetries),
	})
}
