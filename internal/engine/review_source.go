package engine

import (
	"fmt"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// renderReviewFromEvents is what the post-turn reviewer reads.
//
// The source of truth is the event log (and the rolling session briefing),
// never the manager's compacted ADK transcript. Compact is a view for the
// next Generate; memory extraction that read that view would learn the
// briefing instead of the work.
//
// Session briefing first, then this turn's events, so a long earlier
// conversation cannot spend the 24k budget before the work being reviewed.
func renderReviewFromEvents(events []store.Event, sessionMemory, final string) string {
	var b strings.Builder
	budget := reviewMaxChars
	wrote := false

	write := func(role, text string) {
		text = strings.TrimSpace(text)
		if text == "" || budget <= 0 {
			return
		}
		if !wrote {
			b.WriteString("Conversation to review:\n\n")
			wrote = true
		}
		text = clip(text, reviewMaxCharsPerMessage)
		if len(text) > budget {
			text = clip(text, budget)
		}
		budget -= len(text)
		fmt.Fprintf(&b, "%s: %s\n\n", role, text)
	}

	if mem := strings.TrimSpace(sessionMemory); mem != "" {
		// Bound the briefing so a long /goal cannot spend the review budget
		// before this turn's tool calls — those are what become skills.
		write("session briefing", clip(mem, compactSummaryMaxRunes))
	}
	for _, ev := range events {
		role, text, ok := reviewEventLine(ev)
		if !ok {
			continue
		}
		write(role, text)
	}
	write("final answer", final)
	return b.String()
}

func reviewEventLine(ev store.Event) (role, text string, ok bool) {
	text = strings.TrimSpace(ev.Text)
	switch ev.Kind {
	case KindUser, KindGoalContinued, KindSteer:
		if text == "" || isGoalSessionWrapSteer(text) {
			return "", "", false
		}
		return "human", text, true
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
				text = text + "\n" + ev.Err
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

// managerWroteMemory reports that the manager already curated the project
// store during this turn. The automatic reviewer then stays out of the way,
// the same way Claude Code skips extract-memories when the main agent wrote.
func managerWroteMemory(events []store.Event) bool {
	wrote := map[string]struct{}{}
	for _, ev := range events {
		if ev.Kind != swarm.NotifyToolCall.String() || !isManagerAgent(ev.AgentID) {
			continue
		}
		if !memoryWriteTool(toolCallName(ev.Text)) {
			continue
		}
		if id := strings.TrimSpace(ev.ToolCallID); id != "" {
			wrote[id] = struct{}{}
			continue
		}
		// A call with no id still counts: the pairing is a convenience, not
		// a requirement for "the manager already handled this".
		return true
	}
	if len(wrote) == 0 {
		return false
	}
	for _, ev := range events {
		if ev.Kind != swarm.NotifyToolResult.String() {
			continue
		}
		id := strings.TrimSpace(ev.ToolCallID)
		if _, ok := wrote[id]; !ok {
			continue
		}
		if ev.Err != "" {
			continue
		}
		return true
	}
	return false
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
