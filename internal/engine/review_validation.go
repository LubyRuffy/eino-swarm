package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/cloudwego/eino/components/tool"
)

type reviewValidationIssue struct {
	index int
	code  string
	err   error
}

// Preflight never invokes a skill writer. Notes are simulated only in the
// disposable draft, including operations with bad evidence, so independent
// capacity and matching errors are not hidden behind citation repair.
func (t *reviewBatchTool) validateProposal(ctx context.Context, operations []reviewOperation, draft *memory.NotesDraft) ([]reviewEvidence, []reviewValidationIssue) {
	var issues []reviewValidationIssue
	evidence := make([]reviewEvidence, 0, len(operations))
	noteTool := memory.Tools(draft.Store, nil)[0].(tool.InvokableTool)
	for i, op := range operations {
		conflictingText := false
		if op.Target == memory.ToolMemory && (op.Action == "replace" || op.Action == "add") {
			if op.Content != "" && op.NewText != "" && op.Content != op.NewText {
				issues = append(issues, reviewValidationIssue{i, "validation", errors.New("conflicting replacement text fields")})
				conflictingText = true
			}
			if op.Content == "" {
				op.Content = op.NewText
			}
			op.NewText = ""
			operations[i] = op
		}
		if err := t.validateOperation(op); err != nil {
			issues = append(issues, reviewValidationIssue{i, "validation", err})
			continue
		}
		seqs, quotes, err := validateReviewEvidence(op, t.sources, t.humanOnly)
		if err != nil {
			issues = append(issues, reviewValidationIssue{i, "evidence", err})
		}
		if conflictingText {
			// Citation validity is independent of which text field wins;
			// numeric and draft checks need an unambiguous candidate.
			continue
		}
		if op.Target == memory.ToolMemory {
			current, readErr := draft.Store.Read()
			if readErr != nil {
				issues = append(issues, reviewValidationIssue{i, "storage", readErr})
				continue
			}
			if op.Action != "remove" {
				// Existing notes authorize retained numeric spelling only for
				// the entry this operation actually replaces in the draft.
				// Earlier edits can move a selector onto a different note.
				previous := ""
				if op.Action == "replace" && strings.TrimSpace(op.OldText) != "" {
					matches := 0
					for _, entry := range current.Entries {
						if strings.Contains(entry, strings.TrimSpace(op.OldText)) {
							previous = entry
							matches++
						}
					}
					if matches != 1 {
						previous = ""
					}
				}
				if err := validateReviewNumbers(op.Content, quotes, previous); err != nil {
					issues = append(issues, reviewValidationIssue{i, "numeric", err})
				}
			}
			if err := invokeReviewOperation(ctx, noteTool, op); err != nil {
				issues = append(issues, reviewValidationIssue{i, "storage", err})
			}
		}
		evidence = append(evidence, reviewEvidence{Target: op.Target, Action: op.Action, Name: op.Name, Sources: seqs})
	}
	return evidence, issues
}

func (t *reviewBatchTool) refuseIssues(number int, issues []reviewValidationIssue, operations []reviewOperation) string {
	var result map[string]any
	all := make([]map[string]any, 0, len(issues))
	shownSources := map[int64]bool{}
	for _, issue := range issues {
		var repair *reviewRepairError
		if errors.As(issue.err, &repair) {
			if citations, ok := repair.feedback["invalid_evidence"].([]reviewEvidenceIssue); ok {
				for i := range citations {
					if shownSources[citations[i].Seq] {
						citations[i].SourceText = ""
					} else if citations[i].SourceText != "" {
						shownSources[citations[i].Seq] = true
					}
				}
			}
		}
		fields := t.refusal(number, issue.index, issue.code, issue.err, operations)
		if result == nil {
			// Keep the original first-refusal fields for existing tool clients.
			result = make(map[string]any, len(fields)+2)
			for key, value := range fields {
				result[key] = value
			}
		}
		all = append(all, fields)
	}
	result["issues"] = all
	result["instruction"] = "Repair every listed issue in the complete changes array before resubmitting. No notes or skills from this proposal were saved. Repeated source_text is shown only on its first issue for that seq. Numeric checks use only verified quotes and the uniquely replaced old note. A failed note operation leaves the preview draft unchanged, so repair dependent operations together. On capacity issues automatically shorten or consolidate only related notes, preserving supported facts, scope, conditions, negation and exact numeric spelling. Release related space before adding; never truncate text, delete unrelated notes, raise limits or hide a blocked write with an empty batch."
	raw, _ := json.Marshal(result)
	return string(raw)
}
