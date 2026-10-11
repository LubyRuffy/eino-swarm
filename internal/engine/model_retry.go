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
	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	openai "github.com/meguminnnnnnnnn/go-openai"
)

// KindModelRetry is recorded when a pursuing (or any) manager run hits a
// recoverable ChatModel failure — truncated tool JSON, 429, a dropped
// stream, a mid-stream TCP read timeout — or a scheduled check ends
// without work/report evidence. The runtime re-enters the same turn
// instead of blocking. text is JSON {attempt,cap,reason?}. reason tells
// the transcript whether the model service failed or the model returned
// a tool call that was not valid JSON. Older rows omit it.
const KindModelRetry = "model_retry"

// modelErrorRetries is how many times a recoverable model error may re-enter
// this turn. Past that the turn fails. An open /goal auto-continues instead
// of blocking; goal_max_auto_turns and goal_idle still stop a loop.
const modelErrorRetries = 2

const modelRetryNoticeText = "Retrying after a model error."

// modelRetryReasonContextOverflow is the model_retry reason when the
// endpoint rejected the call for window accounting. One compact follows.
// A second rejection ends the turn; it is not another generic retry.
const modelRetryReasonContextOverflow = "context_overflow"

// These reasons are the wire contract for a recoverable model failure.
// The transcript maps each one to its own sentence. Renaming one leaves
// stored rows on the generic retry line.
const (
	modelRetryReasonInvalidToolJSON = "invalid_tool_json"
	modelRetryReasonRateLimited     = "rate_limited"
	modelRetryReasonUnavailable     = "model_unavailable"
	modelRetryReasonTransport       = "model_transport"
	modelRetryReasonIdle            = "model_idle"
	modelRetryReasonResponse        = "model_response_invalid"
)

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
	return modelFailureClass(err) != ""
}

// modelFailureClass splits a recoverable ChatModel failure into the side
// the person can act on. A tool call that is not valid JSON is the model's
// own output. Rate limits, 5xx, a dropped socket, and silence after the
// first byte are the model service. Empty means this error is not that retry.
func modelFailureClass(err error) string {
	if err == nil {
		return ""
	}
	var idle provider.IdleTimeoutError
	if errors.As(err, &idle) {
		return modelRetryReasonIdle
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "status code: 429"):
		return modelRetryReasonRateLimited
	case strings.Contains(s, "status code: 500"),
		strings.Contains(s, "status code: 502"),
		strings.Contains(s, "status code: 503"),
		strings.Contains(s, "status code: 504"),
		strings.Contains(s, "status code: 529"):
		return modelRetryReasonUnavailable
	}
	var request *openai.RequestError
	var call *provider.CallError
	if errors.As(err, &request) || errors.As(err, &call) {
		// Parsed API rejections may describe invalid tool arguments. A
		// decoder failing inside the model call never reached a tool. The
		// SDK uses both encoding/json and Sonic for response fields.
		var api *einoopenai.APIError
		var sdkAPI *openai.APIError
		var syntax *json.SyntaxError
		var shape *json.UnmarshalTypeError
		if !errors.As(err, &api) && !errors.As(err, &sdkAPI) &&
			(errors.As(err, &syntax) || errors.As(err, &shape) ||
				isInvalidToolJSON(err) || strings.Contains(s, "invalid character") ||
				strings.Contains(s, "mismatch type ") || strings.Contains(s, "json: cannot unmarshal")) {
			return modelRetryReasonResponse
		}
	}
	if isInvalidToolJSON(err) || strings.Contains(s, "invalid character") {
		return modelRetryReasonInvalidToolJSON
	}
	// A live socket that dies mid-body. macOS says "operation timed out";
	// Go's own deadline says "i/o timeout". "unexpected end of json" is a
	// tool payload and is classified above.
	switch {
	case strings.Contains(s, "unexpected eof"),
		strings.Contains(s, "connection reset"),
		strings.Contains(s, "operation timed out"),
		strings.Contains(s, "i/o timeout"):
		return modelRetryReasonTransport
	}
	return ""
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

func modelRetryEventText(attempt int, reason string) string {
	if reason == "" {
		return fmt.Sprintf(`{"attempt":%d,"cap":%d}`, attempt, modelErrorRetries)
	}
	return fmt.Sprintf(`{"attempt":%d,"cap":%d,"reason":%q}`, attempt, modelErrorRetries, reason)
}

func (rt *runtime) recordModelRetry(turn *store.Turn, attempt int, reason string) {
	rt.engine.record(store.Event{
		ThreadID: rt.threadID, TurnID: turn.ID,
		Kind: KindModelRetry, AgentID: swarm.DefaultManagerID,
		Text: modelRetryEventText(attempt, reason),
	})
}
