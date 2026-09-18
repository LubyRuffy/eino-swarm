package engine

import (
	"errors"
	"fmt"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

// publicInvalidToolJSON is the banner/transcript line when a ChatModel 400
// was truncated tool-call JSON. The graph dump is for Trace, not a person.
const publicInvalidToolJSON = "the model request failed because a tool call was not valid JSON"

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
	if isInvalidToolJSON(err) {
		return publicInvalidToolJSON
	}
	return stripGraphDump(err.Error())
}

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

func stripGraphDump(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[NodeRunError] ")
	if i := strings.Index(s, "\nnode path:"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}
