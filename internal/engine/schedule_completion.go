package engine

import (
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const scheduleCompletionCue = "This turn is a scheduled check. The last response was empty and no tool result or report was recorded. Continue the current check in this same turn and return the result as text, or call report_schedule. Use tools when the task needs them. Empty findings in report_schedule mean nothing to surface."

// Ending a check with text is valid under automatic tool selection. This
// detects a wholly empty response, not whether its claims are externally true.
func scheduledOutputRecorded(events []store.Event, final string) bool {
	if strings.TrimSpace(final) != "" {
		return true
	}
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
