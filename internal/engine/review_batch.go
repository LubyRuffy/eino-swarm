package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const reviewBatchToolName = memory.ToolApplyReview

type reviewOperation struct {
	Target      string           `json:"target"`
	Action      string           `json:"action"`
	Content     string           `json:"content,omitempty"`
	OldText     string           `json:"old_text,omitempty"`
	NewText     string           `json:"new_text,omitempty"`
	Name        string           `json:"name,omitempty"`
	Description string           `json:"description,omitempty"`
	Sources     []string         `json:"sources,omitempty"`
	Evidence    []reviewCitation `json:"evidence"`
}

type reviewBatchTool struct {
	store *memory.Store
	// Fixed before the first model call; a repair must not silently rebase
	// onto notes the reviewer has never seen.
	notesRev           string
	sources            map[int64]store.Event
	humanOnly          bool
	library            bool
	mu                 *sync.Mutex
	outcome            *reviewOutcome
	complete           bool
	dream              *dreamReview
	dreamStatus        string
	dreamInvalidReason string
	dreamRefs          []dreamEventRef
	// Only changes the model-facing representation of aggregated refusals.
	deduplicateFeedback bool
	// A successful batch ends the ADK agent without a billed closing sentence.
	finish func(context.Context) error
}

func (t *reviewBatchTool) Info(context.Context) (*schema.ToolInfo, error) {
	citation := &schema.ParameterInfo{Type: schema.Object, SubParams: map[string]*schema.ParameterInfo{
		"seq":   {Type: schema.Integer, Required: true, Desc: "The source event sequence shown in the input."},
		"quote": {Type: schema.String, Required: true, Desc: "An exact unchanged excerpt from that source event."},
	}}
	op := &schema.ParameterInfo{Type: schema.Object, SubParams: map[string]*schema.ParameterInfo{
		"target":      {Type: schema.String, Required: true, Enum: []string{memory.ToolMemory, memory.ToolSkillManage}},
		"action":      {Type: schema.String, Required: true, Enum: []string{"add", "replace", "remove", "create", "patch", "merge", "delete"}},
		"content":     {Type: schema.String, Desc: "The note, or skill body for create/merge. No unstated details."},
		"old_text":    {Type: schema.String, Desc: "Unique existing text for note replace/remove or skill patch."},
		"new_text":    {Type: schema.String, Desc: "Replacement text for a skill patch, or an alias for note content on add/replace; conflicting text fields are refused."},
		"name":        {Type: schema.String, Desc: "Existing skill name, or a new subject name for create."},
		"description": {Type: schema.String, Desc: "One-line skill description for create/merge."},
		"sources":     {Type: schema.Array, ElemInfo: &schema.ParameterInfo{Type: schema.String}, Desc: "Other existing skill names for merge."},
		"evidence":    {Type: schema.Array, Required: true, ElemInfo: citation, Desc: "Exact source quotes supporting this change, including any replacement or removal."},
	}}
	params := map[string]*schema.ParameterInfo{
		"changes": {Type: schema.Array, Required: true, ElemInfo: op},
	}
	if t.dream != nil {
		decisions := []string{"keep", "uncertain", "defer"}
		for _, choice := range dreamRevertChoices(t.dream.ids) {
			decisions = append(decisions, choice.decision)
		}
		params["changes"].Desc = "Exclude the audited skill."
		params["dream"] = &schema.ParameterInfo{
			Type: schema.Object, Required: true, Desc: "Decision for the supplied Dream audit.",
			SubParams: map[string]*schema.ParameterInfo{
				"decision": {Type: schema.String, Required: true, Enum: decisions},
				"quote":    {Type: schema.String},
				"sources":  {Type: schema.Array, ElemInfo: &schema.ParameterInfo{Type: schema.String, Enum: t.dream.ids}},
			},
		}
	}
	return &schema.ToolInfo{Name: reviewBatchToolName, Desc: "Submit the complete minimal memory changes and finish this review. Notes are validated in a draft before one revision-checked commit. Use an empty changes array when nothing is worth keeping. A refusal changes no notes and lets you repair the proposal within the existing iteration cap. Notes changed since the model input cause a terminal conflict; this review never silently rebases.", ParamsOneOf: schema.NewParamsOneOfByParams(params)}, nil
}

func (t *reviewBatchTool) InvokableRun(ctx context.Context, args string, _ ...tool.Option) (string, error) {
	t.mu.Lock()
	done := t.complete
	t.mu.Unlock()
	if done {
		return reviewBatchResult(true, "review already completed"), nil
	}
	number := t.beginAttempt()
	// A second batch from the same model response cannot reuse a refused decision.
	offered := t.dream != nil && t.dream.offered.Swap(false)
	var input struct {
		Changes *[]reviewOperation `json:"changes"`
		Dream   dreamVerdictField  `json:"dream"`
	}
	decoder := json.NewDecoder(strings.NewReader(args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.Changes == nil {
		return t.refuse(number, -1, "validation", errors.New("invalid proposal: provide a changes array"), nil), nil
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return t.refuse(number, -1, "validation", errors.New("invalid proposal: expected exactly one JSON object"), nil), nil
	}
	if t.dream == nil && len(input.Dream.raw) != 0 {
		return t.refuse(number, -1, "validation", errors.New("no Dream revision audit was offered"), nil), nil
	}
	var dreamStatus, dreamInvalidReason string
	var dreamRefs []dreamEventRef
	if t.dream != nil {
		dreamStatus = "not_submitted"
		if offered {
			if input.Dream.duplicate {
				dreamStatus, dreamInvalidReason = "invalid", "duplicate_decision"
			} else {
				dreamStatus, dreamRefs, dreamInvalidReason = t.dream.decision(input.Dream.raw)
			}
		}
	}
	operations := *input.Changes
	t.describeAttempt(number, operations)
	if dreamStatus == "defer" {
		if len(operations) != 0 {
			return t.refuse(number, -1, "validation", errors.New("defer requires an empty changes array"), operations), nil
		}
		t.markAttempt(number, "deferred")
		return reviewBatchResult(true, "Continue the ordinary memory review."), nil
	}
	hasNotes := false
	for _, op := range operations {
		hasNotes = hasNotes || op.Target == memory.ToolMemory
	}
	if hasNotes && t.notesRev != "" {
		current, err := t.store.Read()
		if err != nil {
			return "", t.terminalError(number, err, operations)
		}
		if current.Rev != t.notesRev {
			return "", t.terminalError(number, &memory.ConflictError{Current: current}, operations)
		}
	}
	// The empty batch is a real decision, with no filesystem mutation.
	if len(operations) == 0 {
		return t.completed(ctx, number, nil, dreamStatus, dreamRefs, dreamInvalidReason)
	}
	draft, err := t.store.BeginNotesDraft()
	if err != nil {
		return t.refuse(number, -1, "storage", err, operations), nil
	}
	before, err := draft.Store.Read()
	if err != nil {
		return t.refuse(number, -1, "storage", err, operations), nil
	}
	if hasNotes && t.notesRev != "" && before.Rev != t.notesRev {
		return "", t.terminalError(number, &memory.ConflictError{Current: before}, operations)
	}
	evidence, issues := t.validateProposal(ctx, operations, draft)
	if len(issues) != 0 {
		return t.refuseIssues(number, issues, operations), nil
	}
	// Skill files keep their existing immediate-write contract. Persist each
	// actual skill change even if a later operation or note commit is refused.
	skillTool := memory.Tools(t.store, func(c memory.Change) { t.mu.Lock(); defer t.mu.Unlock(); collectReviewChange(t.outcome, c) })[2].(tool.InvokableTool)
	for i, op := range operations {
		if op.Target != memory.ToolSkillManage {
			continue
		}
		if err := invokeReviewOperation(ctx, skillTool, op); err != nil {
			return t.refuse(number, i, "storage", err, operations), nil
		}
	}
	proposed, err := draft.Store.Read()
	if err != nil {
		return t.refuse(number, -1, "storage", err, operations), nil
	}
	// Rephrasing and then undoing it, or only reordering the same notes, must
	// not mutate the live file or advertise a change.
	if len(reviewNoteDiff(before, proposed)) == 0 {
		return t.completed(ctx, number, evidence, dreamStatus, dreamRefs, dreamInvalidReason)
	}
	after, changed, err := draft.Commit()
	if err != nil {
		return t.refuse(number, -1, "storage", err, operations), nil
	}
	if changed {
		t.mu.Lock()
		for _, change := range reviewNoteDiff(before, after) {
			collectReviewChange(t.outcome, change)
		}
		t.mu.Unlock()
	}
	return t.completed(ctx, number, evidence, dreamStatus, dreamRefs, dreamInvalidReason)
}

func (t *reviewBatchTool) validateOperation(op reviewOperation) error {
	if op.Target == memory.ToolMemory {
		if t.library {
			return errors.New("the shared library has no project notes")
		}
		switch op.Action {
		case "add", "replace", "remove":
			return nil
		}
	} else if op.Target == memory.ToolSkillManage {
		if t.dream != nil {
			skill := t.dream.receipt.Skill
			if memory.ResolveSkillToolName(op.Name) == skill ||
				slices.ContainsFunc(op.Sources, func(source string) bool {
					return memory.ResolveSkillToolName(source) == skill
				}) {
				return errors.New("the audited skill must use the Dream decision")
			}
		}
		if t.humanOnly {
			return errors.New("failed turns cannot record or modify skills")
		}
		switch op.Action {
		case "create", "patch", "merge", "delete":
			return nil
		}
	}
	return errors.New("unknown target/action combination")
}

func invokeReviewOperation(ctx context.Context, invoke tool.InvokableTool, op reviewOperation) error {
	raw, err := json.Marshal(op)
	if err != nil {
		return err
	}
	result, err := invoke.InvokableRun(ctx, string(raw))
	if err != nil {
		return err
	}
	var decoded struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		return fmt.Errorf("invalid memory tool receipt: %w", err)
	}
	if !decoded.Success {
		var feedback map[string]json.RawMessage
		_ = json.Unmarshal([]byte(result), &feedback)
		return &reviewOperationError{message: fmt.Sprintf("memory operation refused: %s", decoded.Error), feedback: feedback}
	}
	return nil
}

func (t *reviewBatchTool) completed(ctx context.Context, number int, evidence []reviewEvidence, dreamStatus string, dreamRefs []dreamEventRef, dreamInvalidReason string) (string, error) {
	t.mu.Lock()
	t.complete = true
	t.dreamStatus = dreamStatus
	t.dreamInvalidReason = dreamInvalidReason
	t.dreamRefs = dreamRefs
	t.outcome.StopReason = "completed"
	t.outcome.Evidence = append(t.outcome.Evidence, evidence...)
	t.outcome.Note = "Review finished — nothing new to keep."
	if t.outcome.Changed {
		t.outcome.Note = fmt.Sprintf("Saved %d note changes and %d skill changes.", len(t.outcome.Changes)-len(t.outcome.Skills), len(t.outcome.Skills))
	}
	note := t.outcome.Note
	t.mu.Unlock()
	t.markAttempt(number, "accepted")
	if t.finish != nil {
		if err := t.finish(ctx); err != nil {
			return "", fmt.Errorf("finish memory review: %w", err)
		}
	}
	return reviewBatchResult(true, note), nil
}

func reviewBatchResult(success bool, note string) string {
	raw, _ := json.Marshal(struct {
		Success bool   `json:"success"`
		Note    string `json:"note"`
	}{success, note})
	return string(raw)
}

func reviewNoteDiff(before, after memory.Snapshot) []memory.Change {
	old, newNotes := map[string]bool{}, map[string]bool{}
	for _, entry := range before.Entries {
		old[entry] = true
	}
	for _, entry := range after.Entries {
		newNotes[entry] = true
	}
	var added, removed []string
	for _, entry := range before.Entries {
		if !newNotes[entry] {
			removed = append(removed, entry)
		}
	}
	for _, entry := range after.Entries {
		if !old[entry] {
			added = append(added, entry)
		}
	}
	var changes []memory.Change
	for i := 0; i < max(len(added), len(removed)); i++ {
		action, text := "replace", ""
		beforeText, afterText := "", ""
		if i < len(removed) {
			beforeText = removed[i]
		}
		if i < len(added) {
			text, afterText = added[i], added[i]
		} else {
			action, text = "remove", removed[i]
		}
		if i >= len(removed) {
			action = "add"
		}
		changes = append(changes, memory.Change{
			Target: memory.ToolMemory, Action: action, Text: clip(text, 160),
			Before: &beforeText, After: &afterText,
		})
	}
	return changes
}

func finishReviewBatch(ctx context.Context) error {
	return adk.SendToolGenAction(ctx, reviewBatchToolName, adk.NewExitAction())
}
