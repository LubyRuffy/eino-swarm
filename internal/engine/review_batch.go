package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	notesRev  string
	sources   map[int64]store.Event
	humanOnly bool
	library   bool
	mu        *sync.Mutex
	outcome   *reviewOutcome
	complete  bool
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
	return &schema.ToolInfo{Name: reviewBatchToolName, Desc: "Submit the complete minimal memory changes and finish this review. Notes are validated in a draft before one revision-checked commit. Use an empty changes array when nothing is worth keeping. A refusal changes no notes and lets you repair the proposal within the existing iteration cap. Notes changed since the model input cause a terminal conflict; this review never silently rebases.", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
		"changes": {Type: schema.Array, Required: true, ElemInfo: op},
	})}, nil
}

func (t *reviewBatchTool) InvokableRun(ctx context.Context, args string, _ ...tool.Option) (string, error) {
	t.mu.Lock()
	done := t.complete
	t.mu.Unlock()
	if done {
		return reviewBatchResult(true, "review already completed"), nil
	}
	var input struct {
		Changes *[]reviewOperation `json:"changes"`
	}
	decoder := json.NewDecoder(strings.NewReader(args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.Changes == nil {
		return t.refuse(-1, "invalid proposal: provide a changes array"), nil
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return t.refuse(-1, "invalid proposal: expected exactly one JSON object"), nil
	}
	operations := *input.Changes
	hasNotes := false
	for _, op := range operations {
		hasNotes = hasNotes || op.Target == memory.ToolMemory
	}
	if hasNotes && t.notesRev != "" {
		current, err := t.store.Read()
		if err != nil {
			return "", err
		}
		if current.Rev != t.notesRev {
			return "", &memory.ConflictError{Current: current}
		}
	}
	evidence := make([]reviewEvidence, 0, len(operations))
	for i, op := range operations {
		if op.Target == memory.ToolMemory && (op.Action == "replace" || op.Action == "add") {
			if op.Content != "" && op.NewText != "" && op.Content != op.NewText {
				return t.refuse(i, "conflicting replacement text fields"), nil
			}
			if op.Content == "" {
				op.Content = op.NewText
			}
			op.NewText = ""
			operations[i] = op
		}
		if err := t.validateOperation(op); err != nil {
			return t.refuse(i, err.Error()), nil
		}
		seqs, quotes, err := validateReviewEvidence(op, t.sources, t.humanOnly)
		if err != nil {
			return t.refuse(i, err.Error()), nil
		}
		if op.Target == memory.ToolMemory && op.Action != "remove" {
			previous := ""
			if op.Action == "replace" {
				snap, readErr := t.store.Read()
				if readErr != nil {
					return t.refuse(i, readErr.Error()), nil
				}
				for _, entry := range snap.Entries {
					if strings.Contains(entry, op.OldText) {
						previous += entry + "\n"
					}
				}
			}
			if err := validateReviewNumbers(op.Content, quotes, previous); err != nil {
				return t.refuse(i, err.Error()), nil
			}
		}
		evidence = append(evidence, reviewEvidence{Target: op.Target, Action: op.Action, Name: op.Name, Sources: seqs})
	}
	// The empty batch is a real decision, with no filesystem mutation.
	if len(operations) == 0 {
		return t.completed(ctx, nil)
	}
	draft, err := t.store.BeginNotesDraft()
	if err != nil {
		return t.refuse(-1, err.Error()), nil
	}
	before, err := draft.Store.Read()
	if err != nil {
		return t.refuse(-1, err.Error()), nil
	}
	if hasNotes && t.notesRev != "" && before.Rev != t.notesRev {
		return "", &memory.ConflictError{Current: before}
	}
	noteTool := memory.Tools(draft.Store, nil)[0].(tool.InvokableTool)
	for i, op := range operations {
		if op.Target != memory.ToolMemory {
			continue
		}
		if err := invokeReviewOperation(ctx, noteTool, op); err != nil {
			return t.refuse(i, err.Error()), nil
		}
	}
	// Skill files keep their existing immediate-write contract. Persist each
	// actual skill change even if a later operation or note commit is refused.
	skillTool := memory.Tools(t.store, func(c memory.Change) { t.mu.Lock(); defer t.mu.Unlock(); collectReviewChange(t.outcome, c) })[2].(tool.InvokableTool)
	for i, op := range operations {
		if op.Target != memory.ToolSkillManage {
			continue
		}
		if err := invokeReviewOperation(ctx, skillTool, op); err != nil {
			return t.refuse(i, err.Error()), nil
		}
	}
	proposed, err := draft.Store.Read()
	if err != nil {
		return t.refuse(-1, err.Error()), nil
	}
	// Rephrasing and then undoing it, or only reordering the same notes, must
	// not mutate the live file or advertise a change.
	if len(reviewNoteDiff(before, proposed)) == 0 {
		return t.completed(ctx, evidence)
	}
	after, changed, err := draft.Commit()
	if err != nil {
		return t.refuse(-1, err.Error()), nil
	}
	if changed {
		t.mu.Lock()
		for _, change := range reviewNoteDiff(before, after) {
			collectReviewChange(t.outcome, change)
		}
		t.mu.Unlock()
	}
	return t.completed(ctx, evidence)
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
		return fmt.Errorf("memory operation refused: %s", decoded.Error)
	}
	return nil
}

func (t *reviewBatchTool) refuse(index int, reason string) string {
	t.mu.Lock()
	t.outcome.Refused = append(t.outcome.Refused, reviewRefusal{Index: index, Reason: reason})
	t.mu.Unlock()
	return reviewBatchResult(false, reason)
}

func (t *reviewBatchTool) completed(ctx context.Context, evidence []reviewEvidence) (string, error) {
	t.mu.Lock()
	t.complete = true
	t.outcome.Evidence = append(t.outcome.Evidence, evidence...)
	t.outcome.Note = "Review finished — nothing new to keep."
	if t.outcome.Changed {
		t.outcome.Note = fmt.Sprintf("Saved %d note changes and %d skill changes.", len(t.outcome.Changes)-len(t.outcome.Skills), len(t.outcome.Skills))
	}
	note := t.outcome.Note
	t.mu.Unlock()
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
		if i < len(added) {
			text = added[i]
		} else {
			action, text = "remove", removed[i]
		}
		if i >= len(removed) {
			action = "add"
		}
		changes = append(changes, memory.Change{Target: memory.ToolMemory, Action: action, Text: clip(text, 160)})
	}
	return changes
}

func finishReviewBatch(ctx context.Context) error {
	return adk.SendToolGenAction(ctx, reviewBatchToolName, adk.NewExitAction())
}
