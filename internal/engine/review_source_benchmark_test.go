package engine

import (
	"fmt"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

var reviewSourceBenchmarkOutput string

func reviewSourceBenchmarkEvents(pairs int) []store.Event {
	events := []store.Event{{Seq: 1, Kind: KindUser, AgentID: "manager", Text: "inspect the available fixture"}}
	for i := 0; i < pairs; i++ {
		id := fmt.Sprintf("probe-%d", i)
		events = append(events, store.Event{Seq: int64(len(events) + 1), Kind: swarm.NotifyToolCall.String(), AgentID: "worker", ToolCallID: id, Text: "inspect_fixture({})"})
		events = append(events, store.Event{Seq: int64(len(events) + 1), Kind: swarm.NotifyToolResult.String(), AgentID: "worker", ToolCallID: id, Text: strings.Repeat("fixture observation 界 ", 300)})
	}
	return append(events, store.Event{Seq: int64(len(events) + 1), Kind: KindSteer, AgentID: "manager", Text: "late-human-marker"})
}

// The legacy copy is frozen from the pre-change implementation. Its lower
// work on long input can also mean it never visits the late human message.
func BenchmarkReviewSourceRender(b *testing.B) {
	for _, fixture := range []struct {
		name  string
		pairs int
	}{{"small", 1}, {"long_tools", 32}, {"10000_events", 4999}} {
		events := reviewSourceBenchmarkEvents(fixture.pairs)
		for _, implementation := range []struct {
			name   string
			render func([]store.Event, string, string) string
		}{{"original", renderLegacyReviewSourceBenchmark}, {"selected", renderReviewFromEvents}} {
			b.Run(fixture.name+"/"+implementation.name, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					reviewSourceBenchmarkOutput = implementation.render(events, "earlier fixture context", "final observation")
				}
			})
		}
	}
}

func TestReviewSourceBenchmarkFixtureCapturesTheOriginalLostInput(t *testing.T) {
	for _, pairs := range []int{32, 4999} {
		t.Run(fmt.Sprintf("%d_events", pairs*2+2), func(t *testing.T) {
			events := reviewSourceBenchmarkEvents(pairs)
			if len(events) != pairs*2+2 || events[0].Kind != KindUser || events[len(events)-1].Kind != KindSteer {
				t.Fatal("fixture must retain both human inputs and every tool pair")
			}
			if strings.Contains(renderLegacyReviewSourceBenchmark(events, "", ""), "late-human-marker") {
				t.Fatal("legacy fixture no longer reproduces the original loss")
			}
			if !strings.Contains(renderReviewFromEvents(events, "", ""), "late-human-marker") {
				t.Fatal("selected implementation lost the human input")
			}
		})
	}
}

func renderLegacyReviewSourceBenchmark(events []store.Event, sessionMemory, final string) string {
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
	retracted := retractedSteerSeqs(events)
	revised := revisedSteerText(events)
	for _, ev := range events {
		if ev.Kind == KindSteer {
			if _, ok := retracted[ev.Seq]; ok {
				continue
			}
			if text, ok := revised[ev.Seq]; ok {
				ev.Text = text
			}
		}
		role, text, ok := legacyReviewSourceEventLine(ev)
		if !ok {
			continue
		}
		write(role, text)
	}
	write("final answer", final)
	return b.String()
}

func legacyReviewSourceEventLine(ev store.Event) (role, text string, ok bool) {
	text = strings.TrimSpace(ev.Text)
	switch ev.Kind {
	case KindUser, KindGoalContinued, KindSteer:
		if text == "" || isGoalSessionWrapSteer(text) {
			return "", "", false
		}
		return "human", text, true
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
