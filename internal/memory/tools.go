package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Tool names. They travel in transcripts and in the Trace view, so renaming
// one changes what a stored conversation looks like on replay.
const (
	ToolMemory      = "memory"
	ToolSkillView   = "skill_view"
	ToolSkillManage = "skill_manage"
)

// Change is one thing a tool call did. The engine collects these to report
// what a review changed, which is better than parsing the tool's own JSON back
// out of the transcript.
type Change struct {
	// Target is ToolMemory or ToolSkillManage.
	Target string `json:"target"`
	// Action is add / replace / remove for memory, create / patch / delete for
	// a skill.
	Action string `json:"action"`
	// Name is the skill's name, empty for memory.
	Name string `json:"name,omitempty"`
}

// Tools returns the three memory tools bound to one project's store. onChange
// may be nil; when set it is called once per write that actually landed, and
// must be safe for concurrent use.
func Tools(s *Store, onChange func(Change)) []tool.BaseTool {
	if onChange == nil {
		onChange = func(Change) {}
	}
	return []tool.BaseTool{
		&memoryTool{store: s, onChange: onChange},
		&skillViewTool{store: s},
		&skillManageTool{store: s, onChange: onChange},
	}
}

// Names lists the tools Tools returns, for the system prompt and for tracing.
func Names() []string { return []string{ToolMemory, ToolSkillView, ToolSkillManage} }

// ---------- memory ----------

type memoryTool struct {
	store    *Store
	onChange func(Change)
}

func (t *memoryTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: ToolMemory,
		Desc: "Curate the notes carried into every future conversation in this project. " +
			"Store durable facts about the environment, the human's stated preferences, " +
			"conventions and corrections — not this conversation's working details, and not " +
			"anything that can be looked up again. The store is bounded: when a write does not " +
			"fit, the result lists what is stored so you can consolidate with replace/remove and retry.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"action": {Type: schema.String, Required: true,
				Desc: "add, replace or remove",
				Enum: []string{"add", "replace", "remove"}},
			"content": {Type: schema.String,
				Desc: "the note to store; required for add and replace"},
			"old_text": {Type: schema.String,
				Desc: "a short substring that identifies exactly one existing note; required for replace and remove"},
		}),
	}, nil
}

func (t *memoryTool) InvokableRun(_ context.Context, args string, _ ...tool.Option) (string, error) {
	var a struct {
		Action  string `json:"action"`
		Content string `json:"content"`
		OldText string `json:"old_text"`
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return failure("could not read the arguments: %v", err), nil
	}
	var (
		snap    Snapshot
		err     error
		changed = true
	)
	action := strings.ToLower(strings.TrimSpace(a.Action))
	switch action {
	case "add":
		snap, changed, err = t.store.Add(a.Content)
	case "replace":
		snap, err = t.store.Replace(a.OldText, a.Content)
	case "remove":
		snap, err = t.store.Remove(a.OldText)
	default:
		return failure("unknown action %q; use add, replace or remove", a.Action), nil
	}
	if err != nil {
		return memoryFailure(err), nil
	}
	if changed {
		t.onChange(Change{Target: ToolMemory, Action: action})
	}
	return marshal(map[string]any{
		"success": true,
		"changed": changed,
		"usage":   usage(snap),
		"entries": snap.Entries,
	}), nil
}

// memoryFailure turns a store error into something the model can act on. An
// overflow in particular has to carry the current entries: the agent's next
// move is to consolidate, and it cannot do that blind.
func memoryFailure(err error) string {
	var overflow *OverflowError
	if errors.As(err, &overflow) {
		return marshal(map[string]any{
			"success":         false,
			"error":           overflow.Error(),
			"usage":           fmt.Sprintf("%d/%d", overflow.Usage, overflow.Limit),
			"current_entries": overflow.Entries,
		})
	}
	return failure("%s", err.Error())
}

// ---------- skill_view ----------

type skillViewTool struct{ store *Store }

func (t *skillViewTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: ToolSkillView,
		Desc: "Read one of this project's skills in full. The system prompt lists only each " +
			"skill's name and summary; open the ones that look relevant before starting work " +
			"that a stored procedure already covers.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"name": {Type: schema.String, Required: true, Desc: "the skill's name, as listed in the prompt"},
		}),
	}, nil
}

func (t *skillViewTool) InvokableRun(_ context.Context, args string, _ ...tool.Option) (string, error) {
	var a struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return failure("could not read the arguments: %v", err), nil
	}
	skill, err := t.store.ReadSkill(strings.TrimSpace(a.Name))
	if err != nil {
		if errors.Is(err, ErrNoMatch) {
			names, _ := t.store.ListSkills()
			return marshal(map[string]any{
				"success":   false,
				"error":     fmt.Sprintf("no skill named %q", a.Name),
				"available": skillNames(names),
			}), nil
		}
		return failure("%s", err.Error()), nil
	}
	return marshal(map[string]any{
		"success":     true,
		"name":        skill.Name,
		"description": skill.Description,
		"body":        skill.Body,
	}), nil
}

// ---------- skill_manage ----------

type skillManageTool struct {
	store    *Store
	onChange func(Change)
}

func (t *skillManageTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: ToolSkillManage,
		Desc: "Record a reusable procedure as one of this project's skills, so a later " +
			"conversation can follow it instead of working it out again. Worth recording: a " +
			"multi-step workflow that succeeded, a recovery from a failure, a correction the " +
			"human made. Not worth recording: a single tool call, or anything specific to one " +
			"request. Patch an existing skill rather than adding a second one on the same subject.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"action": {Type: schema.String, Required: true,
				Desc: "create, patch or delete",
				Enum: []string{"create", "patch", "delete"}},
			"name": {Type: schema.String, Required: true,
				Desc: "lower-case letters, digits and hyphens; naming the procedure, not the request"},
			"description": {Type: schema.String,
				Desc: "one line saying when to use this skill; required for create"},
			"content": {Type: schema.String,
				Desc: "the procedure in Markdown: the steps and the tool calls to make; required for create"},
			"old_text": {Type: schema.String, Desc: "for patch: the text to replace, unique in the body"},
			"new_text": {Type: schema.String, Desc: "for patch: what to put in its place"},
		}),
	}, nil
}

func (t *skillManageTool) InvokableRun(_ context.Context, args string, _ ...tool.Option) (string, error) {
	var a struct {
		Action      string `json:"action"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Content     string `json:"content"`
		OldText     string `json:"old_text"`
		NewText     string `json:"new_text"`
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return failure("could not read the arguments: %v", err), nil
	}
	action := strings.ToLower(strings.TrimSpace(a.Action))
	// A model asked for a hyphenated name often sends a title anyway. Fixing
	// it here is better than losing the skill to a rejected name.
	name := strings.TrimSpace(a.Name)
	if ValidSkillName(name) != nil {
		name = SafeSkillName(name)
	}

	switch action {
	case "create":
		skill, err := t.store.WriteSkill(name, a.Description, a.Content)
		if err != nil {
			return failure("%s", err.Error()), nil
		}
		t.onChange(Change{Target: ToolSkillManage, Action: action, Name: skill.Name})
		return marshal(map[string]any{"success": true, "name": skill.Name}), nil
	case "patch":
		skill, err := t.store.PatchSkill(name, a.OldText, a.NewText)
		if err != nil {
			return failure("%s", err.Error()), nil
		}
		t.onChange(Change{Target: ToolSkillManage, Action: action, Name: skill.Name})
		return marshal(map[string]any{"success": true, "name": skill.Name}), nil
	case "delete":
		if err := t.store.DeleteSkill(name); err != nil {
			return failure("%s", err.Error()), nil
		}
		t.onChange(Change{Target: ToolSkillManage, Action: action, Name: name})
		return marshal(map[string]any{"success": true, "name": name}), nil
	default:
		return failure("unknown action %q; use create, patch or delete", a.Action), nil
	}
}

// ---------- helpers ----------

// failure reports a refusal as a tool result rather than a Go error. A Go
// error ends the agent's turn; a result the model can read lets it correct
// itself and try again, which is the whole point of a self-curating store.
func failure(format string, args ...any) string {
	return marshal(map[string]any{"success": false, "error": fmt.Sprintf(format, args...)})
}

func usage(s Snapshot) string { return fmt.Sprintf("%d/%d", s.Chars, s.Limit) }

func skillNames(list []SkillInfo) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		out = append(out, s.Name)
	}
	return out
}

func marshal(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return `{"success":false,"error":"could not encode the result"}`
	}
	return string(raw)
}
