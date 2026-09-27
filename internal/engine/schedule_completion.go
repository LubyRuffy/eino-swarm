package engine

import (
	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const scheduleCompletionCue = "This turn is a scheduled check. The last response ended before any tool executed or report_schedule was called. Continue the current check in this same turn, then call report_schedule with the result. An opening acknowledgement is not a completed check."

// Prose cannot distinguish a completed check from a promise to start one.
// An explicit report (including empty findings) or an actual tool result
// preserves the existing completion fallback without parsing the answer.
func scheduledWorkRecorded(events []store.Event) bool {
	for _, ev := range events {
		if ev.Kind == KindScheduleReport {
			return true
		}
		if ev.Kind == swarm.NotifyToolResult.String() && (ev.Text != modelRetryToolResult || ev.Err != modelRetryToolResult) {
			return true
		}
	}
	return false
}
