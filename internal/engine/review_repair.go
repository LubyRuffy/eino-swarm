package engine

import (
	"fmt"
	"sort"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Repair details belong only to the reviewer's next tool result. The durable
// outcome still records the controlled refusal category, never source bodies
// or rejected numeric details.
type reviewRepairError struct {
	message  string
	feedback map[string]any
}

func (e *reviewRepairError) Error() string { return e.message }

type reviewEvidenceIssue struct {
	Index      int    `json:"index"`
	Seq        int64  `json:"seq"`
	Reason     string `json:"reason"`
	SourceText string `json:"source_text,omitempty"`
}

func reviewEvidenceRepair(issues []reviewEvidenceIssue, sources map[int64]store.Event, humanOnly bool) error {
	// A repeated bad quote must not multiply the already bounded source input
	// into an unbounded repair response. Keep every issue's position and reason.
	shown := map[int64]bool{}
	for i := range issues {
		if shown[issues[i].Seq] {
			issues[i].SourceText = ""
		} else if issues[i].SourceText != "" {
			shown[issues[i].Seq] = true
		}
	}
	seqs := make([]int64, 0, len(sources))
	for seq, ev := range sources {
		if !humanOnly || ev.Kind == KindUser || ev.Kind == KindSteer {
			seqs = append(seqs, seq)
		}
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	return &reviewRepairError{
		message: fmt.Sprintf("evidence validation failed for %d source references; repair every listed citation", len(issues)),
		feedback: map[string]any{
			"instruction":      "Repair every invalid_evidence item in this operation. Choose only citable_sources; an empty list means no admissible source is available. Copy an exact substring of the visible source_text, preserving JSON escapes and numeric spelling. Tool calls and generated text cannot establish facts. Do not invent quotes or use omitted source text. Resubmit the complete changes array; no notes from this proposal were saved.",
			"invalid_evidence": issues,
			"citable_sources":  seqs,
		},
	}
}

func reviewNumberTokens(text string) []string {
	values := make([]string, 0)
	seen := map[string]bool{}
	for _, value := range reviewNumber.FindAllString(text, -1) {
		if !seen[value] {
			values = append(values, value)
			seen[value] = true
		}
	}
	return values
}
