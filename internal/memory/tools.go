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
	// Text previews what was written or dropped, clipped.
	//
	// "Memory updated" tells a user something happened; it does not tell them
	// whether it is something they want stored. Only the text does, and the
	// transcript is where they are already looking.
	Text string `json:"text,omitempty"`
}

// changeTextMax bounds the preview. Long enough for a note, short enough that
// one review cannot push an answer off the screen.
const changeTextMax = 160

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
			"conventions and corrections — one or two sentences, not a procedure, not this " +
			"conversation's working details, not a remaining count or other status that will " +
			"change again, and not anything that can be looked up again. A note that restates " +
			"a recorded skill is refused. The store is bounded by a total and by a per-note " +
			"cap: a write that would grow it past either is refused, including replace with a " +
			"longer note. The result includes over_by or entry_max and the current notes. " +
			"Do not retry the same content; shorten, drop notes, or record a procedure as a skill.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"action": {Type: schema.String, Required: true,
				Desc: "add, replace or remove",
				Enum: []string{"add", "replace", "remove"}},
			"content": {Type: schema.String,
				Desc: "the note to store; required for add and replace"},
			"old_text": {Type: schema.String,
				Desc: "a short substring that identifies exactly one existing note; required for replace and remove. If several notes match, the result lists them — pass a longer unique substring."},
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
		// What the change did, for the line the user reads.
		text = a.Content
	)
	action := strings.ToLower(strings.TrimSpace(a.Action))
	switch action {
	case "add":
		snap, changed, err = t.store.Add(a.Content)
	case "replace":
		snap, err = t.store.Replace(a.OldText, a.Content)
	case "remove":
		snap, text, err = t.store.Remove(a.OldText)
	default:
		return failure("unknown action %q; use add, replace or remove", a.Action), nil
	}
	if err != nil {
		return memoryFailure(err), nil
	}
	if changed {
		t.onChange(Change{Target: ToolMemory, Action: action, Text: clipText(text)})
	}
	return marshal(map[string]any{
		"success": true,
		"changed": changed,
		"usage":   usage(snap),
		"entries": snap.Entries,
	}), nil
}

// memoryFailure turns a store error into something the model can act on. An
// overflow in particular has to carry the current entries and how far over
// the limit the write landed: "consolidate with replace" without those numbers
// is how a model retries the same longer note until the turn budget runs out.
func memoryFailure(err error) string {
	var overflow *OverflowError
	if errors.As(err, &overflow) {
		out := map[string]any{
			"success":         false,
			"error":           overflow.Error(),
			"usage":           fmt.Sprintf("%d/%d", overflow.Usage, overflow.Limit),
			"over_by":         overflow.OverBy(),
			"current_entries": overflow.Entries,
		}
		if overflow.Matched != "" {
			out["matched"] = overflow.Matched
		}
		return marshal(out)
	}
	var tooLongErr *EntryTooLongError
	if errors.As(err, &tooLongErr) {
		return marshal(map[string]any{
			"success":   false,
			"error":     tooLongErr.Error(),
			"chars":     tooLongErr.Chars,
			"entry_max": tooLongErr.Max,
		})
	}
	var restates *NoteSkillOverlapError
	if errors.As(err, &restates) {
		return marshal(map[string]any{
			"success":              false,
			"error":                restates.Error(),
			"existing":             restates.Name,
			"existing_description": restates.Description,
		})
	}
	var match *MatchError
	if errors.As(err, &match) {
		out := map[string]any{
			"success":         false,
			"error":           match.Error(),
			"current_entries": match.Entries,
		}
		if len(match.Matching) > 0 {
			out["matching"] = match.Matching
		}
		return marshal(out)
	}
	return failure("%s", err.Error())
}

// ---------- skill_view ----------

type skillViewTool struct{ store *Store }

func (t *skillViewTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: ToolSkillView,
		Desc: "Read one recorded project skill in full. The system prompt lists only each " +
			"skill's name and summary; open a listed name before starting work that procedure " +
			"already covers. Names not in that index cannot be opened here — a procedure in " +
			"the workspace is a file.",
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
				"success": false,
				"error": fmt.Sprintf(
					"no skill named %q. %s only opens skills recorded in this project's memory (the names in the system prompt), not files in the workspace",
					a.Name, ToolSkillView),
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
			"request. One subject is one skill. Call skill_view on any index entry that might " +
			"already cover the subject; a create that collides is refused and names the " +
			"existing skill — patch that one (or delete it first).",
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
			var dup *DuplicateSkillError
			if errors.As(err, &dup) {
				names, _ := t.store.ListSkills()
				return marshal(map[string]any{
					"success":              false,
					"error":                dup.Error(),
					"existing":             dup.Name,
					"existing_description": dup.Description,
					"available":            skillNames(names),
				}), nil
			}
			return failure("%s", err.Error()), nil
		}
		t.onChange(Change{Target: ToolSkillManage, Action: action, Name: skill.Name,
			Text: clipText(skill.Description)})
		return marshal(map[string]any{"success": true, "name": skill.Name}), nil
	case "patch":
		skill, err := t.store.PatchSkill(name, a.OldText, a.NewText)
		if err != nil {
			return failure("%s", err.Error()), nil
		}
		t.onChange(Change{Target: ToolSkillManage, Action: action, Name: skill.Name,
			Text: clipText(a.NewText)})
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

// clipText flattens a written note to one bounded line for the preview. The
// stored entry keeps its line breaks; only what is shown is squashed.
func clipText(s string) string {
	flat := strings.Join(strings.Fields(s), " ")
	r := []rune(flat)
	if len(r) <= changeTextMax {
		return flat
	}
	return strings.TrimSpace(string(r[:changeTextMax])) + "…"
}

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
