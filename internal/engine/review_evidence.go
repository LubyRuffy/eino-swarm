package engine

import (
	"encoding/json"
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
		return nil, "", reviewEvidenceRepair([]reviewEvidenceIssue{{Index: -1, Reason: "missing_evidence"}}, sources, humanOnly)
	}
	var seqs []int64
	var quotes strings.Builder
	seen := map[int64]bool{}
	var issues []reviewEvidenceIssue
	for index, ref := range op.Evidence {
		ev, ok := sources[ref.Seq]
		issue := reviewEvidenceIssue{Index: index, Seq: ref.Seq}
		switch {
		case !ok:
			issue.Reason = "source_unavailable"
		case humanOnly && ev.Kind != KindUser && ev.Kind != KindSteer:
			issue.Reason = "human_only"
		case strings.TrimSpace(ref.Quote) == "":
			issue.Reason, issue.SourceText = "empty_quote", ev.Text
		case !strings.Contains(ev.Text, ref.Quote):
			issue.Reason, issue.SourceText = "quote_mismatch", ev.Text
		}
		if issue.Reason != "" {
			issues = append(issues, issue)
			continue
		}
		if !seen[ref.Seq] {
			seqs = append(seqs, ref.Seq)
			seen[ref.Seq] = true
		}
		quotes.WriteString(ref.Quote)
		quotes.WriteByte('\n')
	}
	if len(issues) > 0 {
		return nil, "", reviewEvidenceRepair(issues, sources, humanOnly)
	}
	return seqs, quotes.String(), nil
}

// Preserve numeric spelling from the evidence or the retained old note.
// This catches fabricated dates/versions/counts; it is not an entailment test.
func validateReviewNumbers(content, evidence, previous string) error {
	values := reviewNumberTokens(evidence + "\n" + previous)
	supported := map[string]bool{}
	for _, value := range values {
		supported[value] = true
	}
	var unsupported []string
	for _, value := range reviewNumberTokens(content) {
		if !supported[value] {
			unsupported = append(unsupported, value)
		}
	}
	if len(unsupported) > 0 {
		return &reviewRepairError{
			message: "new numeric detail lacks cited support: " + strings.Join(unsupported, ", "),
			feedback: map[string]any{
				"instruction":         "Repair every unsupported_numbers item in this operation. supported_numbers comes only from this operation's exact quotes and replaced old note. If an admissible visible source supports a missing value, add its exact quote; otherwise omit the unsupported detail. Preserve numeric spelling, units, dates, versions and identifiers; do not convert digits to words to evade validation. Source sequence numbers and display line numbers are not evidence. Resubmit the complete changes array; no notes from this proposal were saved.",
				"unsupported_numbers": unsupported,
				"supported_numbers":   values,
			},
		}
	}
	return nil
}
