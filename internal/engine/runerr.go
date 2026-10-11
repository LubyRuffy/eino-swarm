package engine

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	openai "github.com/meguminnnnnnnnn/go-openai"
)

const publicRateLimited = "the model service is rate limiting requests"

const publicModelUnavailable = "the model service is unavailable"

const publicModelTransport = "the connection to the model service dropped"

const publicModelResponseInvalid = "the model service returned a response that could not be decoded"

// publicContextOverflow is the transcript line when the endpoint rejects
// the call because input plus the requested output does not fit the window.
// The provider paragraph stays in Trace. This sentence is stable when a
// vendor rewords the 400, and it still says how to make the next call fit.
const publicContextOverflow = "the prompt plus the requested output exceeds this model's context window; automatic compact could not free enough space. Lower Max completion tokens in Settings → Swarm, or shorten the request, then retry"

// publicContextOverflowPaused is the same rejection on a repeating schedule.
// The check is paused so the next tick cannot send the same prompt again.
const publicContextOverflowPaused = publicContextOverflow + ". The scheduled check is paused"

// publicTurnError is what the transcript and the turn row show. eino wraps
// a silent stream in NodeRunError and a graph path; that dump is for a
// trace, not a person.
func publicTurnError(err error) string {
	if err == nil {
		return ""
	}
	var idle provider.IdleTimeoutError
	if errors.As(err, &idle) {
		return fmt.Sprintf("the model sent no data for %s; raise the provider request timeout in Settings, or lower thinking", idle.Idle)
	}
	if isContextOverflow(err) {
		return publicContextOverflow
	}
	if isHeaderWaitTimeout(err) {
		return fmt.Sprintf("the model sent no first byte within %s", config.DefaultFirstByteTimeout)
	}
	// A typed API error keeps its HTTP status and decode cause. A 502 with
	// an empty body is not a broken tool call. String-only failures below
	// have no such cause, so the transcript names the service side.
	if reason, ok := publicModelAPIError(err); ok {
		return reason
	}
	switch modelFailureClass(err) {
	case modelRetryReasonRateLimited:
		return publicRateLimited
	case modelRetryReasonUnavailable:
		return publicModelUnavailable
	case modelRetryReasonTransport:
		return publicModelTransport
	case modelRetryReasonResponse:
		return publicModelResponseInvalid
	}
	if isMaxIterations(err) {
		return "stopped at the tool-round cap"
	}
	return stripGraphDump(err.Error())
}

func isHeaderWaitTimeout(err error) bool {
	return err != nil && strings.Contains(err.Error(), "timeout awaiting response headers")
}

// isInvalidToolJSON classifies a retry. The transcript still shows the
// provider's own text; guessing "invalid tool JSON" hid the HTTP status.
func isInvalidToolJSON(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "unterminated string") || strings.Contains(s, "unexpected end of json") {
		return true
	}
	return strings.Contains(s, "status code: 400") &&
		(strings.Contains(s, "json") || strings.Contains(s, "parse") || strings.Contains(s, "syntax"))
}

// The SDK's RequestError.Error includes the raw response body. Its decoder
// cause describes the failure without copying that untrusted body to the UI.
func publicModelAPIError(err error) (string, bool) {
	var request *openai.RequestError
	if errors.As(err, &request) && request != nil {
		cause := ""
		if request.Err != nil {
			cause = request.Err.Error()
		}
		return formatModelAPIError(request.HTTPStatusCode, cause), true
	}
	var api *einoopenai.APIError
	if errors.As(err, &api) && api != nil {
		return formatModelAPIError(api.HTTPStatusCode, api.Message), true
	}
	var sdkAPI *openai.APIError
	if errors.As(err, &sdkAPI) && sdkAPI != nil {
		return formatModelAPIError(sdkAPI.HTTPStatusCode, sdkAPI.Message), true
	}
	return "", false
}

var publicAPIBearer = regexp.MustCompile(`(?i)\bbearer\s+[^\s,;]+|\bsk-[a-z0-9_-]+`)
var publicAPICredentials = regexp.MustCompile(`(?i)\b(?:api[ _-]?key(?:\s+provided)?|authorization|password|token|cookie)\s*[:=]\s*(?:"[^"]*"|'[^']*'|[^\s,;]+)`)

func formatModelAPIError(status int, cause string) string {
	cause = strings.Join(strings.Fields(stripGraphDump(cause)), " ")
	cause = publicAPIBearer.ReplaceAllString(cause, "[redacted]")
	cause = publicAPICredentials.ReplaceAllString(cause, "[redacted]")
	if runes := []rune(cause); len(runes) > 512 {
		cause = string(runes[:511]) + "…"
	}
	message := "model API request failed"
	if status > 0 {
		message += fmt.Sprintf(": HTTP %d", status)
		if name := http.StatusText(status); name != "" {
			message += " " + name
		}
	}
	if cause != "" {
		message += ": " + cause
	}
	return message
}

func stripGraphDump(s string) string {
	s = strings.TrimSpace(s)
	for {
		next := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(s, "[NodeRunError] "), "[GraphRunError] "))
		if next == s {
			break
		}
		s = next
	}
	// eino prints a dashed rule, then "node path:". Cutting only at the
	// path leaves the rule in the transcript.
	if i := strings.Index(s, "\n--------"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if i := strings.Index(s, "\nnode path:"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}
