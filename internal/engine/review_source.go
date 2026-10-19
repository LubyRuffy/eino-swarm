package engine

import (
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// renderReviewFromEvents reads the durable event log rather than the manager's
// compacted transcript. Selection protects human input before tool output;
// rendering then restores source order so evidence keeps its original context.
func renderReviewFromEvents(events []store.Event, sessionMemory, final string) string {
	return renderSelectedReviewSources(events, sessionMemory, final)
}

func reviewEventLine(ev store.Event) (role, text string, ok bool) {
	text = strings.TrimSpace(ev.Text)
	switch ev.Kind {
	case KindUser, KindSteer:
		if text == "" || isGoalSessionWrapSteer(text) {
			return "", "", false
		}
		return "human", text, true
	case KindGoalContinued:
		return "runtime", text, text != ""
	case KindSteerRetracted, KindSteerPreempted, KindSteerRevised:
		return "", "", false
	case swarm.NotifyAgentMessage.String():
		if text == "" {
			return "", "", false
		}
		role = strings.TrimSpace(ev.AgentID)
		if role == "" {
			role = "assistant"
		}
		return role, text, true
	case swarm.NotifyToolCall.String():
		if text == "" {
			return "", "", false
		}
		role = strings.TrimSpace(ev.AgentID)
		if role == "" {
			role = "assistant"
		}
		return role, "[called " + text + "]", true
	case swarm.NotifyToolResult.String():
		if text == "" && ev.Err == "" {
			return "", "", false
		}
		if ev.Err != "" {
			if text == "" {
				text = ev.Err
			} else {
				text = ev.Err + "\n" + text
			}
		}
		return "tool", text, true
	case swarm.NotifySpawned.String():
		name := strings.TrimSpace(ev.AgentID)
		if name == "" {
			name = strings.TrimSpace(ev.Role)
		}
		if name == "" {
			return "", "", false
		}
		return "spawn", name, true
	case swarm.NotifyFinished.String():
		name := strings.TrimSpace(ev.AgentID)
		if name == "" {
			name = "worker"
		}
		if text == "" {
			text = "finished"
		}
		return name, text, true
	default:
		return "", "", false
	}
}

func memoryWriteTool(name string) bool {
	switch strings.TrimSpace(name) {
	case memory.ToolMemory, memory.ToolSkillManage:
		return true
	default:
		return false
	}
}

func toolCallName(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexByte(text, '('); i > 0 {
		return strings.TrimSpace(text[:i])
	}
	return text
}
