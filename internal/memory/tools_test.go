package memory

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

func toolset(t *testing.T, limit int) (*Store, map[string]tool.InvokableTool, *[]Change) {
	t.Helper()
	s := New(filepath.Join(t.TempDir(), "memory"), limit)
	var mu sync.Mutex
	changes := &[]Change{}
	byName := map[string]tool.InvokableTool{}
	for _, bt := range Tools(s, func(c Change) {
		mu.Lock()
		defer mu.Unlock()
		*changes = append(*changes, c)
	}) {
		info, err := bt.Info(context.Background())
		if err != nil {
			t.Fatalf("Info: %v", err)
		}
		inv, ok := bt.(tool.InvokableTool)
		if !ok {
			t.Fatalf("%s is not invokable", info.Name)
		}
		byName[info.Name] = inv
	}
	if len(byName) != len(Names()) {
		t.Fatalf("tools=%v want %v", byName, Names())
	}
	return s, byName, changes
}

func run(t *testing.T, tl tool.InvokableTool, args map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	out, err := tl.InvokableRun(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("InvokableRun returned a Go error (%v); a refusal must come back as a result the model can read", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("tool result is not JSON: %q", out)
	}
	return parsed
}

func TestMemoryToolCuratesTheStore(t *testing.T) {
	s, tools, changes := toolset(t, 500)
	mem := tools[ToolMemory]

	res := run(t, mem, map[string]any{"action": "add", "content": "a durable fact"})
	if res["success"] != true || res["changed"] != true {
		t.Fatalf("add=%v", res)
	}
	if res["usage"] == "" {
		t.Fatalf("the result must report how full the store is: %v", res)
	}

	// A duplicate reports success without a change, so an agent re-learning
	// something does not have to handle an error.
	res = run(t, mem, map[string]any{"action": "add", "content": "a durable fact"})
	if res["success"] != true || res["changed"] != false {
		t.Fatalf("duplicate add=%v", res)
	}

	res = run(t, mem, map[string]any{"action": "replace", "old_text": "durable", "content": "a corrected fact"})
	if res["success"] != true {
		t.Fatalf("replace=%v", res)
	}
	res = run(t, mem, map[string]any{"action": "remove", "old_text": "corrected"})
	if res["success"] != true {
		t.Fatalf("remove=%v", res)
	}
	if snap, _ := s.Read(); len(snap.Entries) != 0 {
		t.Fatalf("store=%+v", snap.Entries)
	}

	// One recorded change per write that landed; the duplicate is not one.
	var actions []string
	for _, c := range *changes {
		if c.Target != ToolMemory {
			t.Fatalf("change=%+v", c)
		}
		actions = append(actions, c.Action)
	}
	if strings.Join(actions, ",") != "add,replace,remove" {
		t.Fatalf("recorded changes=%v", actions)
	}
	if (*changes)[0].Text != "a durable fact" || (*changes)[1].Text != "a corrected fact" {
		t.Fatalf("a change without its text cannot be shown: %+v", *changes)
	}
}

// Every refusal has to come back as a readable result rather than a Go error:
// a Go error ends the agent's turn, and the point of a self-curating store is
// that the agent corrects itself and retries.
func TestMemoryToolReportsRefusalsAsResults(t *testing.T) {
	_, tools, changes := toolset(t, 40)
	mem := tools[ToolMemory]

	for name, args := range map[string]map[string]any{
		"unknown action":  {"action": "sing", "content": "x"},
		"no such entry":   {"action": "remove", "old_text": "nothing like this"},
		"missing content": {"action": "add", "content": "   "},
	} {
		res := run(t, mem, args)
		if res["success"] != false || res["error"] == "" {
			t.Fatalf("%s: %v", name, res)
		}
	}

	out, err := mem.InvokableRun(context.Background(), "{not json")
	if err != nil {
		t.Fatalf("malformed arguments must not end the turn: %v", err)
	}
	if !strings.Contains(out, `"success":false`) {
		t.Fatalf("malformed arguments=%q", out)
	}

	// A full store must hand back what is stored: the agent's next move is to
	// consolidate, and it cannot do that blind.
	if res := run(t, mem, map[string]any{"action": "add", "content": strings.Repeat("x", 41)}); res["success"] != false {
		t.Fatalf("an oversized note should not fit: %v", res)
	}
	run(t, mem, map[string]any{"action": "add", "content": "a first note that nearly fills it"})
	res := run(t, mem, map[string]any{"action": "add", "content": "and a second one"})
	if res["success"] != false {
		t.Fatalf("want a refusal: %v", res)
	}
	entries, ok := res["current_entries"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("a full store must list what it holds: %v", res)
	}
	if !strings.Contains(res["error"].(string), "Consolidate") {
		t.Fatalf("the refusal must say what to do next: %v", res["error"])
	}
	if got := len(*changes); got != 1 {
		t.Fatalf("refused writes were recorded as changes: %d", got)
	}
}

func TestSkillToolsRecordViewAndPatchAProcedure(t *testing.T) {
	s, tools, changes := toolset(t, 500)
	manage, view := tools[ToolSkillManage], tools[ToolSkillView]

	res := run(t, manage, map[string]any{
		"action": "create", "name": "Stored Procedure",
		"description": "when a later conversation does the same work",
		"content":     "1. first step\n2. second step",
	})
	// A model that sends a title instead of a hyphenated name must not lose
	// the skill to the name rule.
	if res["success"] != true || res["name"] != "stored-procedure" {
		t.Fatalf("create=%v", res)
	}

	res = run(t, view, map[string]any{"name": "stored-procedure"})
	if res["success"] != true || !strings.Contains(res["body"].(string), "second step") {
		t.Fatalf("view=%v", res)
	}

	res = run(t, manage, map[string]any{
		"action": "patch", "name": "stored-procedure",
		"old_text": "second step", "new_text": "second step, done differently",
	})
	if res["success"] != true {
		t.Fatalf("patch=%v", res)
	}
	skill, err := s.ReadSkill("stored-procedure")
	if err != nil || !strings.Contains(skill.Body, "done differently") {
		t.Fatalf("patch did not land: %+v err=%v", skill, err)
	}

	res = run(t, manage, map[string]any{"action": "delete", "name": "stored-procedure"})
	if res["success"] != true {
		t.Fatalf("delete=%v", res)
	}
	if list, _ := s.ListSkills(); len(list) != 0 {
		t.Fatalf("skills=%v", list)
	}

	var actions []string
	for _, c := range *changes {
		if c.Target != ToolSkillManage || c.Name != "stored-procedure" {
			t.Fatalf("change=%+v", c)
		}
		actions = append(actions, c.Action)
	}
	if strings.Join(actions, ",") != "create,patch,delete" {
		t.Fatalf("recorded changes=%v", actions)
	}
}

// When the agent asks for a skill that is not there, the result must name the
// ones that are: guessing again is the failure mode this prevents.
func TestViewingAMissingSkillListsTheOnesThatExist(t *testing.T) {
	_, tools, _ := toolset(t, 500)
	run(t, tools[ToolSkillManage], map[string]any{
		"action": "create", "name": "present", "description": "d", "content": "b",
	})
	res := run(t, tools[ToolSkillView], map[string]any{"name": "absent"})
	if res["success"] != false {
		t.Fatalf("view=%v", res)
	}
	available, ok := res["available"].([]any)
	if !ok || len(available) != 1 || available[0] != "present" {
		t.Fatalf("available=%v", res)
	}
}

func TestSkillToolsReportRefusalsAsResults(t *testing.T) {
	_, tools, _ := toolset(t, 500)
	manage, view := tools[ToolSkillManage], tools[ToolSkillView]

	for name, args := range map[string]map[string]any{
		"unknown action": {"action": "rewrite", "name": "x"},
		"no description": {"action": "create", "name": "x", "content": "b"},
		"no body":        {"action": "create", "name": "x", "description": "d"},
		"unusable name":  {"action": "create", "name": "!!!", "description": "d", "content": "b"},
		"patch missing":  {"action": "patch", "name": "nothere", "old_text": "a", "new_text": "b"},
		"delete missing": {"action": "delete", "name": "nothere"},
	} {
		if res := run(t, manage, args); res["success"] != false || res["error"] == "" {
			t.Fatalf("%s: %v", name, res)
		}
	}
	for _, tl := range []tool.InvokableTool{manage, view} {
		out, err := tl.InvokableRun(context.Background(), "{not json")
		if err != nil {
			t.Fatalf("malformed arguments must not end the turn: %v", err)
		}
		if !strings.Contains(out, `"success":false`) {
			t.Fatalf("malformed arguments=%q", out)
		}
	}
	if res := run(t, view, map[string]any{"name": "!!!"}); res["success"] != false {
		t.Fatalf("view with an unusable name=%v", res)
	}
}

// Tools built without a change recorder must still work: the foreground
// manager uses them that way, and only the review collects changes.
func TestToolsWorkWithoutAChangeRecorder(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "memory"), 500)
	for _, bt := range Tools(s, nil) {
		info, err := bt.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if info.Name == ToolMemory {
			if res := run(t, bt.(tool.InvokableTool), map[string]any{"action": "add", "content": "x"}); res["success"] != true {
				t.Fatalf("add=%v", res)
			}
		}
	}
	if snap, _ := s.Read(); len(snap.Entries) != 1 {
		t.Fatalf("store=%+v", snap.Entries)
	}
}
