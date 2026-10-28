package memory

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func assertChangeContent(t *testing.T, change Change, before, after string) {
	t.Helper()
	if change.Before == nil || change.After == nil || *change.Before != before || *change.After != after {
		t.Fatalf("change did not retain exact content: before=%v after=%v want %q -> %q", change.Before, change.After, before, after)
	}
	// The absent side of a creation/deletion must survive JSON as an empty
	// string; omission would make the UI mistake it for a legacy preview.
	raw, err := json.Marshal(change)
	if err != nil {
		t.Fatal(err)
	}
	var replay Change
	if err := json.Unmarshal(raw, &replay); err != nil || replay.Before == nil || replay.After == nil || *replay.Before != before || *replay.After != after {
		t.Fatalf("receipt lost content on replay: %s (%v)", raw, err)
	}
}

func TestMemoryToolDetailsRetainCompleteStoredNotesAndRemovedText(t *testing.T) {
	s, tools, changes := toolset(t, 2000)
	first := strings.Repeat("完整内容", 45) + "\n保留条件。"
	added := "  " + first + "  "
	if out := run(t, tools[ToolMemory], map[string]any{"action": "add", "content": added}); out["success"] != true {
		t.Fatalf("add=%v", out)
	}
	assertChangeContent(t, (*changes)[0], "", first)
	if (*changes)[0].Text == first || !strings.HasSuffix((*changes)[0].Text, "…") {
		t.Fatal("a full detail must not remove the bounded legacy preview")
	}
	second := "修订后的完整规则\n仍保留多行和边界。"
	if out := run(t, tools[ToolMemory], map[string]any{"action": "replace", "old_text": "保留条件", "content": "\n" + second + "\n"}); out["success"] != true {
		t.Fatalf("replace=%v", out)
	}
	assertChangeContent(t, (*changes)[1], first, second)
	if out := run(t, tools[ToolMemory], map[string]any{"action": "remove", "old_text": "完整规则"}); out["success"] != true {
		t.Fatalf("remove=%v", out)
	}
	assertChangeContent(t, (*changes)[2], second, "")
	if _, _, err := s.Add("later edit"); err != nil {
		t.Fatal(err)
	}
	assertChangeContent(t, (*changes)[0], "", first)
	assertChangeContent(t, (*changes)[1], first, second)
	assertChangeContent(t, (*changes)[2], second, "")
}

func TestMemoryToolDetailsDoNotInventWritesForDuplicatesOrRefusals(t *testing.T) {
	_, tools, changes := toolset(t, 200)
	for _, args := range []map[string]any{
		{"action": "add", "content": "existing note"},
		{"action": "add", "content": "existing note"},
		{"action": "replace", "old_text": "missing", "content": "new note"},
		{"action": "remove", "old_text": "missing"},
	} {
		run(t, tools[ToolMemory], args)
	}
	if len(*changes) != 1 {
		t.Fatalf("non-writes generated details: %+v", *changes)
	}
	assertChangeContent(t, (*changes)[0], "", "existing note")
	legacy, err := json.Marshal(Change{Target: ToolMemory, Action: "replace", Text: "preview"})
	if err != nil || strings.Contains(string(legacy), "before") || strings.Contains(string(legacy), "after") {
		t.Fatalf("legacy receipt gained invented content: %s (%v)", legacy, err)
	}
}

func TestSkillToolDetailsRetainExactFilesAcrossCreatePatchAndDelete(t *testing.T) {
	s, tools, changes := toolset(t, 2000)
	body := "Inspect the complete input before accepting the result.\n\n" + strings.Repeat("Keep the original evidence. ", 12)
	if out := run(t, tools[ToolSkillManage], map[string]any{"action": "create", "name": "evidence-check", "description": "When checking preserved evidence", "content": "\n" + body + "\n"}); out["success"] != true {
		t.Fatalf("create=%v", out)
	}
	created, err := os.ReadFile(s.skillPath("evidence-check"))
	if err != nil {
		t.Fatal(err)
	}
	assertChangeContent(t, (*changes)[0], "", string(created))
	if out := run(t, tools[ToolSkillManage], map[string]any{"action": "patch", "name": "evidence-check", "old_text": "complete input", "new_text": "complete source"}); out["success"] != true {
		t.Fatalf("patch=%v", out)
	}
	patched, err := os.ReadFile(s.skillPath("evidence-check"))
	if err != nil {
		t.Fatal(err)
	}
	assertChangeContent(t, (*changes)[1], string(created), string(patched))
	// create also supports overwriting the same skill; that must retain its
	// previous file instead of claiming the old side was absent.
	if out := run(t, tools[ToolSkillManage], map[string]any{"action": "create", "name": "evidence-check", "description": "When checking preserved evidence", "content": "Verify the final retained original."}); out["success"] != true {
		t.Fatalf("rewrite=%v", out)
	}
	rewritten, err := os.ReadFile(s.skillPath("evidence-check"))
	if err != nil {
		t.Fatal(err)
	}
	assertChangeContent(t, (*changes)[2], string(patched), string(rewritten))
	if out := run(t, tools[ToolSkillManage], map[string]any{"action": "delete", "name": "evidence-check"}); out["success"] != true {
		t.Fatalf("delete=%v", out)
	}
	assertChangeContent(t, (*changes)[3], string(rewritten), "")
	if _, err := s.WriteSkill("evidence-check", "When checking preserved evidence", "A later unrelated revision."); err != nil {
		t.Fatal(err)
	}
	assertChangeContent(t, (*changes)[0], "", string(created))
	assertChangeContent(t, (*changes)[3], string(rewritten), "")
}

func TestSkillChangeLeavesUnknownEvidenceAsPreviewWithoutChangingWriteResult(t *testing.T) {
	for _, side := range []string{"before", "after"} {
		t.Run(side, func(t *testing.T) {
			s := New(t.TempDir(), 100)
			path := s.skillPath("proof")
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if side == "after" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			skill, change, err := s.changeSkill("proof", func() (Skill, error) {
				if side == "before" {
					if err := os.Remove(path); err != nil {
						return Skill{}, err
					}
					if err := os.WriteFile(path, []byte("saved content"), 0o600); err != nil {
						return Skill{}, err
					}
				} else if err := os.Mkdir(path, 0o700); err != nil {
					return Skill{}, err
				}
				return Skill{SkillInfo: SkillInfo{Name: "proof"}}, nil
			})
			if err != nil || skill.Name != "proof" || change.Before != nil || change.After != nil {
				t.Fatalf("evidence read changed successful operation: skill=%+v change=%+v err=%v", skill, change, err)
			}
		})
	}
	s := New(t.TempDir(), 100)
	called := false
	if _, _, err := s.changeSkill("../bad", func() (Skill, error) { called = true; return Skill{}, nil }); !errors.Is(err, ErrBadName) || called {
		t.Fatalf("invalid name admitted: called=%v err=%v", called, err)
	}
}
