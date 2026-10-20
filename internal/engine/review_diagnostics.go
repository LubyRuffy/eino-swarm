package engine

import (
	"encoding/json"
	"errors"
	"regexp"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
)

// Diagnostic bounds are independent of the configured model iteration cap.
// A proposal can contain many operations and quotes; only safe metadata lands
// in the event log, never the rejected content or the repair-only receipt.
const (
	reviewDiagnosticAttempts   = 32
	reviewDiagnosticOperations = 32
	reviewDiagnosticSources    = 16
)

type reviewAttempt struct {
	Number     int                      `json:"number"`
	Operations []reviewAttemptOperation `json:"operations"`
	Status     string                   `json:"status"`
}

type reviewAttemptOperation struct {
	Target  string  `json:"target"`
	Action  string  `json:"action"`
	Sources []int64 `json:"sources,omitempty"`
}

type reviewRefusal struct {
	Index   int     `json:"index"`
	Reason  string  `json:"reason"`
	Code    string  `json:"code,omitempty"`
	Attempt int     `json:"attempt,omitempty"`
	Sources []int64 `json:"sources,omitempty"`
	Usage   string  `json:"usage,omitempty"`
	OverBy  int     `json:"over_by,omitempty"`
}

// The full receipt exists only while repairing the model proposal. Its fields
// may contain current notes, skill names or paths, so it is never marshalled
// as part of reviewOutcome.
type reviewOperationError struct {
	message  string
	feedback map[string]json.RawMessage
}

func (e *reviewOperationError) Error() string { return e.message }

type reviewTerminalError struct{ cause error }

func (e *reviewTerminalError) Error() string {
	if errors.Is(e.cause, memory.ErrConflict) {
		return memory.ErrConflict.Error()
	}
	return reviewRefusalReason("storage")
}

func (e *reviewTerminalError) Unwrap() error { return e.cause }

func (t *reviewBatchTool) terminalError(number int, err error, operations []reviewOperation) error {
	t.refuse(number, -1, "storage", err, operations)
	t.markAttempt(number, "error")
	return &reviewTerminalError{cause: err}
}

func reviewRefusalCode(err error, fallback string) string {
	if errors.Is(err, memory.ErrConflict) {
		return "conflict"
	}
	var receipt *reviewOperationError
	if errors.As(err, &receipt) {
		if receipt.feedback["over_by"] != nil || receipt.feedback["entry_max"] != nil {
			return "capacity"
		}
		return "validation"
	}
	return fallback
}

func reviewRefusalReason(code string) string {
	switch code {
	case "evidence":
		return "The proposal's source references or exact quotes failed evidence validation."
	case "numeric":
		return "A new numeric detail has no supporting source."
	case "capacity":
		return "The proposal exceeds the memory capacity or per-note limit."
	case "conflict":
		return "Memory changed after the review snapshot; this proposal was not committed."
	case "storage":
		return "Memory could not be read or written."
	default:
		return "The proposal is invalid or does not match the current memory."
	}
}

func reviewSafeOperation(op reviewOperation) reviewAttemptOperation {
	target, action := "unknown", "unknown"
	switch op.Target {
	case memory.ToolMemory, memory.ToolSkillManage:
		target = op.Target
	}
	switch op.Action {
	case "add", "replace", "remove", "create", "patch", "merge", "delete":
		action = op.Action
	}
	var sources []int64
	seen := map[int64]bool{}
	for _, ref := range op.Evidence {
		if len(sources) == reviewDiagnosticSources {
			break
		}
		if ref.Seq > 0 && !seen[ref.Seq] {
			sources = append(sources, ref.Seq)
			seen[ref.Seq] = true
		}
	}
	return reviewAttemptOperation{Target: target, Action: action, Sources: sources}
}

func (t *reviewBatchTool) beginAttempt() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.outcome.AttemptCount++
	number := t.outcome.AttemptCount
	if len(t.outcome.Attempts) < reviewDiagnosticAttempts {
		t.outcome.Attempts = append(t.outcome.Attempts, reviewAttempt{Number: number, Operations: []reviewAttemptOperation{}, Status: "error"})
	}
	return number
}

func (t *reviewBatchTool) describeAttempt(number int, operations []reviewOperation) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if number > len(t.outcome.Attempts) {
		return
	}
	for _, op := range operations[:min(len(operations), reviewDiagnosticOperations)] {
		t.outcome.Attempts[number-1].Operations = append(t.outcome.Attempts[number-1].Operations, reviewSafeOperation(op))
	}
}

func (t *reviewBatchTool) markAttempt(number int, status string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if number <= len(t.outcome.Attempts) {
		t.outcome.Attempts[number-1].Status = status
	}
}

var reviewUsage = regexp.MustCompile(`^[0-9]{1,12}/[0-9]{1,12}$`)

func (t *reviewBatchTool) refuse(number, index int, code string, err error, operations []reviewOperation) string {
	code = reviewRefusalCode(err, code)
	refused := reviewRefusal{Index: index, Reason: reviewRefusalReason(code), Code: code, Attempt: number}
	if index >= 0 && index < len(operations) {
		refused.Sources = reviewSafeOperation(operations[index]).Sources
	}
	// Detailed memory feedback belongs to the model result only. Whitelist the
	// same repair fields emitted by the existing memory tool, not arbitrary JSON.
	result := map[string]any{"success": false, "note": err.Error(), "error": err.Error(), "code": code}
	var receipt *reviewOperationError
	if errors.As(err, &receipt) {
		for _, key := range []string{"usage", "over_by", "current_entries", "matched", "matching", "chars", "entry_max", "existing", "existing_description"} {
			if value := receipt.feedback[key]; value != nil {
				result[key] = value
			}
		}
		_ = json.Unmarshal(receipt.feedback["usage"], &refused.Usage)
		if !reviewUsage.MatchString(refused.Usage) {
			refused.Usage = ""
		}
		_ = json.Unmarshal(receipt.feedback["over_by"], &refused.OverBy)
		refused.OverBy = max(0, refused.OverBy)
	}
	t.markAttempt(number, "refused")
	t.mu.Lock()
	if len(t.outcome.Refused) < reviewDiagnosticAttempts {
		t.outcome.Refused = append(t.outcome.Refused, refused)
	}
	t.mu.Unlock()
	raw, _ := json.Marshal(result)
	return string(raw)
}
