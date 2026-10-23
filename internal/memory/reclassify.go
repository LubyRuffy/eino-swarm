package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// ToolMemoryReclassify is available only during a user-triggered project tidy.
const ToolMemoryReclassify = "memory_reclassify"

var (
	ErrReclassifyCoverage = errors.New("memory: the destination must preserve the source facts without adding unsupported facts")
	ErrSkillProcedure     = errors.New("memory: only a short declarative skill without steps can become a note")
	procedureStepLine     = regexp.MustCompile(`(?m)^\s*(?:[-*+]\s+|[0-9]+[.)]\s+)`)
)

// ReclassifyResult includes writes that landed before an error. Complete moves
// have one promote/demote receipt; an interrupted move reports only its saved
// create/add destination, leaving the source available for a retry.
type ReclassifyResult struct {
	Changed bool     `json:"changed"`
	Changes []Change `json:"changes"`
}

// ReclassifyTools adds movement of existing memory to a project tidy. Neither
// foreground tools nor the evidence-bound post-turn review receive this set.
func ReclassifyTools(s *Store, onChange func(Change)) []tool.BaseTool {
	if s == nil {
		return nil
	}
	return []tool.BaseTool{&reclassifyTool{store: s, onChange: onChange}}
}

func ReclassifyNames() []string { return []string{ToolMemoryReclassify} }

type reclassifyTool struct {
	store    *Store
	onChange func(Change)
}

func (*reclassifyTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: ToolMemoryReclassify,
		Desc: "Move existing project memory between notes and skills during this requested tidy. " +
			memoryBoundary +
			"Use promote for a note that belongs in an on-demand skill; its body must equal the complete original note after whitespace normalization. " +
			"Use demote only for a short declarative skill without steps; the note must equal its complete body after whitespace normalization. " +
			"Do not invent new knowledge or overwrite an existing different destination. " +
			"The destination is saved before the source is removed. A failure may leave both copies; retry the same operation.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"action":      {Type: schema.String, Required: true, Enum: []string{"promote", "demote"}, Desc: "promote a note or demote a skill"},
			"old_text":    {Type: schema.String, Desc: "for promote: a substring identifying exactly one current note"},
			"name":        {Type: schema.String, Required: true, Desc: "destination skill name for promote; source skill name for demote"},
			"description": {Type: schema.String, Desc: "for promote: when to use the skill"},
			"content":     {Type: schema.String, Required: true, Desc: "destination skill body for promote; destination note for demote"},
		}),
	}, nil
}

func (t *reclassifyTool) InvokableRun(_ context.Context, args string, _ ...tool.Option) (string, error) {
	var a struct {
		Action      string `json:"action"`
		OldText     string `json:"old_text"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Content     string `json:"content"`
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return failure("could not read the arguments: %v", err), nil
	}
	var result ReclassifyResult
	var err error
	switch strings.ToLower(strings.TrimSpace(a.Action)) {
	case "promote":
		result, err = t.store.PromoteNote(a.OldText, strings.TrimSpace(a.Name), a.Description, a.Content)
	case "demote":
		result, err = t.store.DemoteSkill(strings.TrimSpace(a.Name), a.Content)
	default:
		return failure("unknown action %q; use promote or demote", a.Action), nil
	}
	if t.onChange != nil {
		for _, change := range result.Changes {
			t.onChange(change)
		}
	}
	out := map[string]any{"success": true}
	if err != nil {
		_ = json.Unmarshal([]byte(memoryFailure(err)), &out)
	}
	out["changed"], out["changes"] = result.Changed, result.Changes
	return marshal(out), nil
}

// PromoteNote saves a covering skill before removing the uniquely selected
// note. Reusing an identical destination completes an interrupted prior move;
// different existing content is never rewritten as part of a move.
func (s *Store) PromoteNote(oldText, name, description, body string) (ReclassifyResult, error) {
	var result ReclassifyResult
	if err := ValidSkillName(name); err != nil {
		return result, err
	}
	oldText = strings.TrimSpace(oldText)
	description, body = normalizeSkill(description, body)
	if oldText == "" || description == "" || body == "" {
		return result, fmt.Errorf("memory: promote needs old_text, description and content")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draft != nil {
		return result, fmt.Errorf("memory: reclassification is unavailable in a notes draft")
	}
	snap, err := s.read()
	if err != nil {
		return result, err
	}
	target, targetErr := s.readSkill(name)
	idx, err := matchOne(snap.Entries, oldText)
	if err != nil {
		if errors.Is(err, ErrNoMatch) && targetErr == nil && target.Description == description && target.Body == body {
			return result, nil
		}
		return result, err
	}
	note := snap.Entries[idx]
	if !reclassificationCovered(note, body) {
		return result, ErrReclassifyCoverage
	}
	if targetErr == nil {
		if target.Description != description || target.Body != body {
			return result, &DuplicateSkillError{Name: name, Description: target.Description, Reason: "destination has different content"}
		}
	} else {
		if !errors.Is(targetErr, ErrNoMatch) {
			return result, targetErr
		}
		if _, err := s.writeSkillLocked(name, description, body, nil, nil); err != nil {
			return result, err
		}
		result.Changed = true
		result.Changes = []Change{{Target: ToolSkillManage, Action: "create", Name: name, Text: clipText(description)}}
	}
	next := append(append([]string{}, snap.Entries[:idx]...), snap.Entries[idx+1:]...)
	if _, err := s.write(next, 0); err != nil {
		return result, fmt.Errorf("memory: skill %q was saved; its source note remains: %w", name, err)
	}
	result.Changed = true
	result.Changes = []Change{{Target: ToolSkillManage, Action: "promote", Name: name, Text: clipText(note)}}
	return result, nil
}

// DemoteSkill preserves the short skill's factual body in a budgeted note
// before deleting the skill. Repeated identical calls do not duplicate notes.
func (s *Store) DemoteSkill(name, content string) (ReclassifyResult, error) {
	var result ReclassifyResult
	if err := ValidSkillName(name); err != nil {
		return result, err
	}
	content = normalizeEntry(content)
	if content == "" {
		return result, fmt.Errorf("memory: demote needs content")
	}
	if err := tooLong(content, s.entryMax); err != nil {
		return result, err
	}
	if procedureStepLine.MatchString(content) {
		return result, ErrSkillProcedure
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draft != nil {
		return result, fmt.Errorf("memory: reclassification is unavailable in a notes draft")
	}
	snap, err := s.read()
	if err != nil {
		return result, err
	}
	exists := false
	for _, note := range snap.Entries {
		if note == content {
			exists = true
			break
		}
	}
	skill, err := s.readSkill(name)
	if err != nil {
		if errors.Is(err, ErrNoMatch) && exists {
			return result, nil
		}
		return result, err
	}
	max := s.entryMax
	if max <= 0 {
		max = s.limit
	}
	if err := tooLong(skill.Body, max); err != nil {
		return result, err
	}
	if procedureStepLine.MatchString(skill.Body) {
		return result, ErrSkillProcedure
	}
	if preservedText(content) != preservedText(skill.Body) {
		return result, ErrReclassifyCoverage
	}
	if !exists {
		skills, err := s.loadAllSkills()
		if err != nil {
			return result, err
		}
		for _, other := range skills {
			if other.Name != name && noteOverlapsSkill(content, other) &&
				!(noteReferencesSkill(content, other.Name) && !noteCopiesSkillBody(content, other.Body) && !noteCoveredBy(content, other.Body)) {
				return result, &NoteSkillOverlapError{Name: other.Name, Description: other.Description}
			}
		}
		next := append(append([]string{}, snap.Entries...), content)
		if _, err := s.write(next, utf8.RuneCountInString(content)); err != nil {
			return result, err
		}
		result.Changed = true
		result.Changes = []Change{{Target: ToolMemory, Action: "add", Text: clipText(content)}}
	}
	if err := s.deleteSkillLocked(name); err != nil {
		// RemoveAll may unlink the document before a later child refuses
		// cleanup. Report that visible deletion even though the move failed.
		if _, sourceErr := s.readSkill(name); errors.Is(sourceErr, ErrNoMatch) {
			result.Changed = true
			result.Changes = append(result.Changes, Change{Target: ToolSkillManage, Action: "delete", Name: name})
		}
		return result, fmt.Errorf("memory: the note was saved; source skill %q could not be removed: %w", name, err)
	}
	result.Changed = true
	result.Changes = []Change{{Target: ToolSkillManage, Action: "demote", Name: name, Text: clipText(content)}}
	return result, nil
}

func reclassificationCovered(content, source string) bool {
	// Overlap is a useful candidate signal, but losing one negation or number
	// can reverse a fact. Only preserved source text authorizes its removal.
	return preservedText(source) == preservedText(content)
}

func preservedText(content string) string {
	return strings.Join(strings.Fields(content), " ")
}
