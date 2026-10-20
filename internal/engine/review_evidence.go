package engine

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

type reviewCitation struct {
	Seq   int64  `json:"seq"`
	Quote string `json:"quote"`
}

type reviewEvidence struct {
	Target  string  `json:"target"`
	Action  string  `json:"action"`
	Name    string  `json:"name,omitempty"`
	Sources []int64 `json:"sources"`
}

var reviewNumber = regexp.MustCompile(`[0-9]+(?:[./:_-][0-9]+)*(?:%|[a-zA-Z]+)?`)

// Evidence keeps the source role and effective steer text. A tool receipt is
// admissible only when paired with an actual call and not explicitly refused.
// It establishes what was observed, not whether a new claim follows from it.
func reviewEvidenceSources(events []store.Event, visible map[int64]store.Event) map[int64]store.Event {
	human := reviewHumanEvents(events)
	sources := make(map[int64]store.Event, len(human))
	for _, ev := range human {
		if selected, ok := visible[ev.Seq]; ok && strings.Contains(ev.Text, selected.Text) {
			sources[ev.Seq] = selected
		}
	}
	calls := map[reviewToolKey]store.Event{}
	for _, ev := range events {
		key := reviewToolKey{agent: ev.AgentID, call: ev.ToolCallID}
		if ev.Kind == swarm.NotifyToolCall.String() && ev.ToolCallID != "" {
			calls[key] = ev
			continue
		}
		selected, resultVisible := visible[ev.Seq]
		if ev.Kind != swarm.NotifyToolResult.String() || ev.ToolCallID == "" || ev.Err != "" || !resultVisible {
			continue
		}
		call, ok := calls[key]
		_, callVisible := visible[call.Seq]
		if !ok || !callVisible || memoryWriteTool(toolCallName(call.Text)) {
			continue
		}
		var body map[string]any
		if json.Unmarshal([]byte(ev.Text), &body) == nil && body["success"] == false {
			continue
		}
		sources[ev.Seq] = selected
	}
	return sources
}

func validateReviewEvidence(op reviewOperation, sources map[int64]store.Event, humanOnly bool) ([]int64, string, error) {
	if len(op.Evidence) == 0 {
		return nil, "", fmt.Errorf("each change needs an exact source quote and sequence")
	}
	var seqs []int64
	var quotes strings.Builder
	seen := map[int64]bool{}
	for _, ref := range op.Evidence {
		ev, ok := sources[ref.Seq]
		if !ok || strings.TrimSpace(ref.Quote) == "" || !strings.Contains(ev.Text, ref.Quote) {
			return nil, "", fmt.Errorf("source %d is absent, revised, untrusted, or does not contain the exact quote", ref.Seq)
		}
		if humanOnly && ev.Kind != KindUser && ev.Kind != KindSteer {
			return nil, "", fmt.Errorf("this review accepts only explicit human requirements")
		}
		if !seen[ref.Seq] {
			seqs = append(seqs, ref.Seq)
			seen[ref.Seq] = true
		}
		quotes.WriteString(ref.Quote)
		quotes.WriteByte('\n')
	}
	return seqs, quotes.String(), nil
}

// Preserve numeric spelling from the evidence or the retained old note.
// This catches fabricated dates/versions/counts; it is not an entailment test.
func validateReviewNumbers(content, evidence, previous string) error {
	supported := map[string]bool{}
	for _, value := range reviewNumber.FindAllString(evidence+"\n"+previous, -1) {
		supported[value] = true
	}
	for _, value := range reviewNumber.FindAllString(content, -1) {
		if !supported[value] {
			return fmt.Errorf("new numeric detail %q has no source; omit it or quote its evidence", value)
		}
	}
	return nil
}
