package remote

import (
	"encoding/json"
	"strings"
	"unicode"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Inbox rows are what a human reads on a phone. Tool names and JSON
// envelopes are protocol, not that line. Findings / a command / assistant
// prose stay; schedule_wake and memory bookkeeping drop off so the chrome
// can say Waiting or running in the phone's language.

const scheduledCheckPrefix = "This turn is a scheduled check."

var inboxPreferredKeys = []string{
	"findings",
	"command",
	"query",
	"url",
	"file_path",
	"path",
	"pattern",
	"text",
	"code",
	"summary",
}

var inboxSkipKeys = map[string]bool{
	"id":               true,
	"ok":               true,
	"quiet":            true,
	"keep":             true,
	"kind":             true,
	"thread_id":        true,
	"origin_thread":    true,
	"origin_thread_id": true,
	"next_in_s":        true,
	"delay_s":          true,
	"every_s":          true,
	"cron":             true,
	"status":           true,
	"created_at":       true,
	"prompt":           true,
}

var inboxBookkeepingTools = map[string]bool{
	"schedule_wake":   true,
	"schedule_task":   true,
	"cancel_schedule": true,
	"memory":          true,
	"skill_manage":    true,
	"skill_view":      true,
	"close_agent":     true,
	"wait_agents":     true,
	"ask_user":        true,
	"spawn_agent":     true,
	"resume_agent":    true,
}

func runningPreview(evts []store.Event, turnID string) string {
	for i := len(evts) - 1; i >= 0; i-- {
		ev := evts[i]
		if turnID != "" && ev.TurnID != turnID {
			continue
		}
		switch ev.Kind {
		case "agent_message", "delta", "tool_call":
			if s := humanLine(ev.Text); s != "" {
				return s
			}
		}
	}
	return ""
}

func summaryFromTurns(turns []store.Turn, chars int) string {
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].Quiet {
			continue
		}
		if s := humanLine(turns[i].Final); s != "" {
			return clipPreview(s, chars)
		}
		if s := humanLine(turns[i].UserText); s != "" {
			return clipPreview(s, chars)
		}
	}
	return ""
}

func clipPreview(s string, chars int) string {
	if chars <= 0 {
		chars = config.DefaultRemoteSummaryChars
	}
	return truncate(flatten(s), chars)
}

func humanLine(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, scheduledCheckPrefix) {
		return ""
	}
	if looksToolCall(s) {
		return toolPreview(s)
	}
	if looksPacked(s) {
		return jsonFieldPreview(s)
	}
	return flatten(s)
}

func toolPreview(raw string) string {
	name, args := splitToolCall(raw)
	if name == "report_schedule" {
		return jsonFieldPreview(args)
	}
	if inboxBookkeepingTools[name] {
		return ""
	}
	if strings.TrimSpace(args) == "" {
		// A bare verb is still a tool name. The phone localizes running.
		return ""
	}
	if s := jsonFieldPreview(args); s != "" {
		return s
	}
	if looksPacked(args) {
		return ""
	}
	return flatten(args)
}

func jsonFieldPreview(raw string) string {
	obj, ok := parsePreviewObject(raw)
	if !ok {
		return ""
	}
	for _, k := range inboxPreferredKeys {
		if s := previewText(obj[k]); s != "" {
			return flatten(s)
		}
	}
	for k, v := range obj {
		if inboxSkipKeys[k] {
			continue
		}
		if s := previewText(v); s != "" {
			return flatten(s)
		}
	}
	return ""
}

func looksPacked(s string) bool {
	t := strings.TrimSpace(s)
	return strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[")
}

func looksToolCall(s string) bool {
	name, _ := splitToolCall(s)
	return isToolIdent(name) && strings.Contains(s, "(")
}

func isToolIdent(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if !unicode.IsLower(r) && r != '_' {
				return false
			}
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

func splitToolCall(raw string) (name, args string) {
	raw = strings.TrimSpace(raw)
	open := strings.Index(raw, "(")
	if open < 0 {
		return raw, ""
	}
	name = strings.TrimSpace(raw[:open])
	args = raw[open+1:]
	if strings.HasSuffix(args, ")") {
		args = args[:len(args)-1]
	}
	return name, args
}

func parsePreviewObject(raw string) (map[string]any, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "{") {
		return nil, false
	}
	var val any
	if err := json.Unmarshal([]byte(raw), &val); err != nil {
		return nil, false
	}
	obj, ok := val.(map[string]any)
	return obj, ok
}

func previewText(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func flatten(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
