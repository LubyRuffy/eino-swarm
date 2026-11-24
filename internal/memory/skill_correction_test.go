package memory

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

func TestCorrectSkillPreservesMetadataAndPersistsProtection(t *testing.T) {
	s, tools, _ := toolset(t, 2000)
	const name = "tracked-procedure"
	const description = "when checking retained evidence: verify the source"
	const before = "Retain the unsupported replacement."
	const after = "Retain the original evidence.\nVerify all conditions."
	const source = "private-thread:private-turn:123"
	if _, err := s.ImportSkill(name, description, before, SkillOrigin{ProjectID: "source-project", Name: "original-procedure"}); err != nil {
		t.Fatal(err)
	}
	original, err := s.ViewSkill(name)
	if err != nil {
		t.Fatal(err)
	}
	usage := readCorrectionFixture(t, s.skillUsagePath(name))
	change, changed, err := s.CorrectSkill(name, before, "\n"+strings.ReplaceAll(after, "\n", "\r\n")+"\n", source)
	if err != nil || !changed || change.Name != name || change.Target != ToolSkillManage || change.Action != "patch" {
		t.Fatalf("correct=%+v changed=%v err=%v", change, changed, err)
	}
	assertChangeContent(t, change, before, after)
	marked := readCorrectionFixture(t, s.skillPath(name))
	if !slices.Equal(usage, readCorrectionFixture(t, s.skillUsagePath(name))) {
		t.Fatal("correction changed usage sidecar")
	}
	front, _ := splitFrontMatter(string(marked))
	if marker := front[skillCorrectionKey]; len(marker) != 64 || strings.Contains(string(marked), source) {
		t.Fatal("missing opaque source marker")
	}
	reopened := New(s.dir, 2000)
	got, err := reopened.ReadSkill(name)
	if err != nil || got.Body != after || got.Description != original.Description || got.Origin == nil ||
		*got.Origin != *original.Origin || got.Views != original.Views || got.LastViewedAt != original.LastViewedAt {
		t.Fatalf("metadata or correction did not survive reopening: %+v (%v)", got, err)
	}
	if change, changed, err := reopened.CorrectSkill(name, before, after, source); err != nil || changed || change != (Change{}) {
		t.Fatalf("retry=%+v changed=%v err=%v", change, changed, err)
	}
	if !slices.Equal(marked, readCorrectionFixture(t, s.skillPath(name))) {
		t.Fatal("retry changed the stored file")
	}
	view := run(t, tools[ToolSkillView], map[string]any{"name": name})
	if view["success"] != true || view["body"] != after {
		t.Fatalf("agent cannot read corrected body: %v", view)
	}
	manager := Tools(reopened, nil)[2].(tool.InvokableTool)
	refusal := run(t, manager, map[string]any{"action": "patch", "name": name, "old_text": after, "new_text": before})
	if refusal["success"] != false || !slices.Equal(marked, readCorrectionFixture(t, s.skillPath(name))) {
		t.Fatalf("reopened Store let an agent undo the correction: %v", refusal)
	}
	for _, payload := range []any{got, got.SkillInfo, change, view, refusal} {
		raw, err := json.Marshal(payload)
		if err != nil || strings.Contains(string(raw), skillCorrectionKey) || strings.Contains(string(raw), source) || strings.Contains(string(raw), front[skillCorrectionKey]) {
			t.Fatalf("correction marker leaked from %T (%v)", payload, err)
		}
	}
}

func TestCorrectSkillRequiresExactCurrentBodyAndDistinctSourceForNewChange(t *testing.T) {
	const name = "recorded-procedure"
	const before = "First stored line.\nPreserve a second condition."
	const after = "Apply the corrected first line.\nPreserve a second condition."
	s := New(t.TempDir(), 2000)
	plantSkill(t, s.dir, name, "when reviewing recorded steps", before)
	for _, tc := range []struct {
		name, skill, before, after, source string
		want                               error
	}{
		{"invalid name", "../escape", before, after, "source", ErrBadName},
		{"missing skill", "absent", before, after, "source", ErrNoMatch},
		{"substring", name, "First stored line.", after, "source", ErrSkillCorrectionConflict},
		{"noncanonical before", name, " " + before, after, "source", ErrSkillCorrectionConflict},
		{"no net change", name, before, before, "source", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := readCorrectionFixture(t, s.skillPath(name))
			change, changed, err := s.CorrectSkill(tc.skill, tc.before, tc.after, tc.source)
			if !errors.Is(err, tc.want) || changed || change != (Change{}) || !slices.Equal(raw, readCorrectionFixture(t, s.skillPath(name))) {
				t.Fatalf("unexpected correction: change=%+v changed=%v err=%v", change, changed, err)
			}
		})
	}
	for _, values := range [][3]string{{"", after, "source"}, {before, "\t", "source"}, {before, after, " "}} {
		if _, changed, err := s.CorrectSkill(name, values[0], values[1], values[2]); err == nil || changed {
			t.Fatalf("incomplete correction accepted: %v", values)
		}
	}
	draft, err := s.BeginNotesDraft()
	if err != nil {
		t.Fatal(err)
	}
	if _, changed, err := draft.Store.CorrectSkill(name, before, after, "source"); err == nil || changed {
		t.Fatal("notes draft wrote an actual correction")
	}
	if _, changed, err := s.CorrectSkill(name, before, after, "source"); err != nil || !changed {
		t.Fatalf("first correction changed=%v err=%v", changed, err)
	}
	if _, changed, err := s.CorrectSkill(name, after, "Conflicting new content.", "source"); !errors.Is(err, ErrSkillCorrectionConflict) || changed {
		t.Fatalf("same source changed approved content: changed=%v err=%v", changed, err)
	}
	if _, changed, err := s.CorrectSkill(name, before, after, "different-source"); !errors.Is(err, ErrSkillCorrectionConflict) || changed {
		t.Fatalf("stale revision with a different source succeeded: changed=%v err=%v", changed, err)
	}
	if _, changed, err := s.CorrectSkill(name, after, "A later explicit correction.", "different-source"); err != nil || !changed {
		t.Fatalf("new source against current revision refused: changed=%v err=%v", changed, err)
	}
}

func TestCorrectSkillDoesNotSaveMarkerWhenBodyWriteFails(t *testing.T) {
	s := New(t.TempDir(), 2000)
	const name = "recorded-procedure"
	const before = "Retain the original line."
	plantSkill(t, s.dir, name, "when checking original lines", before)
	path := s.skillPath(name)
	raw := readCorrectionFixture(t, path)
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if change, changed, err := s.CorrectSkill(name, before, "Apply the correction.", "source"); err == nil || changed || change != (Change{}) {
		t.Fatalf("failed write had a receipt: change=%+v changed=%v err=%v", change, changed, err)
	}
	if !slices.Equal(raw, readCorrectionFixture(t, path)) {
		t.Fatal("failed correction changed the skill")
	}
}

func TestCorrectSkillAndAgentPatchShareARevisionBoundary(t *testing.T) {
	s, tools, _ := toolset(t, 2000)
	const name = "recorded-procedure"
	const before = "Retain the original line."
	const corrected = "Retain the whole supported procedure."
	const agentEdit = "Keep only the newest unsupported line."
	plantSkill(t, s.dir, name, "when checking original lines", before)
	args, err := json.Marshal(map[string]any{"action": "patch", "name": name, "old_text": before, "new_text": agentEdit})
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		changed bool
		err     error
	}
	start := make(chan struct{})
	correction, agent := make(chan result, 1), make(chan result, 1)
	go func() {
		<-start
		_, changed, err := s.CorrectSkill(name, before, corrected, "source")
		correction <- result{changed, err}
	}()
	go func() {
		<-start
		raw, err := tools[ToolSkillManage].InvokableRun(context.Background(), string(args))
		var out struct {
			Success bool `json:"success"`
		}
		if err == nil {
			err = json.Unmarshal([]byte(raw), &out)
		}
		agent <- result{out.Success, err}
	}()
	close(start)
	c, a := <-correction, <-agent
	if a.err != nil || c.changed == a.changed {
		t.Fatalf("exactly one writer must win: correction=%+v agent=%+v", c, a)
	}
	got, err := s.ReadSkill(name)
	if err != nil {
		t.Fatal(err)
	}
	if c.changed && (c.err != nil || got.Body != corrected || !got.corrected()) {
		t.Fatalf("agent changed a committed correction: %+v (%v)", got, c.err)
	}
	if a.changed && (!errors.Is(c.err, ErrSkillCorrectionConflict) || got.Body != agentEdit || got.corrected()) {
		t.Fatalf("correction applied to a stale body: %+v (%v)", got, c.err)
	}
}

func readCorrectionFixture(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func plantCorrectedSkill(t *testing.T, s *Store, name, body string) []byte {
	t.Helper()
	plantSkill(t, s.dir, name, "when checking recorded procedures", body)
	path := s.skillPath(name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	marked := []byte(strings.Replace(string(raw), "\n---\n", "\ndream_correction: fixture-source-digest\n---\n", 1))
	if err := os.WriteFile(path, marked, 0o600); err != nil {
		t.Fatal(err)
	}
	return marked
}

func TestSkillManageCannotChangeCorrectedSkill(t *testing.T) {
	const name = "recorded-procedure"
	const body = "Retain the original supported procedure."
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"upsert", map[string]any{"action": "create", "name": name, "description": "when checking", "content": "An unsupported replacement."}},
		{"patch", map[string]any{"action": "patch", "name": name, "old_text": body, "new_text": "An unsupported replacement."}},
		{"delete", map[string]any{"action": "delete", "name": name}},
		{"merge keeper", map[string]any{"action": "merge", "name": name, "sources": []string{"other-procedure"}}},
		{"merge source", map[string]any{"action": "merge", "name": "other-procedure", "sources": []string{name}}},
		{"merge into new keeper", map[string]any{"action": "merge", "name": "fresh-procedure", "sources": []string{"other-procedure", name}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, tools, changes := toolset(t, 2000)
			marked := plantCorrectedSkill(t, s, name, body)
			plantSkill(t, s.dir, "other-procedure", "when reading a separate log", "Read the captured log before reporting.")
			out := run(t, tools[ToolSkillManage], tc.args)
			reason, _ := out["error"].(string)
			if out["success"] != false || !strings.Contains(reason, "protected") || len(*changes) != 0 {
				t.Fatalf("correction protection failed: out=%v changes=%+v", out, *changes)
			}
			if raw, err := os.ReadFile(s.skillPath(name)); err != nil || !slices.Equal(raw, marked) {
				t.Fatalf("protected skill changed: %q (%v)", raw, err)
			}
			if names := skillNamesOf(t, s); !slices.Equal(names, []string{"other-procedure", name}) {
				t.Fatalf("failed operation changed catalog: %v", names)
			}
		})
	}
}

func TestAgentTidyCannotDemoteACorrectedSkill(t *testing.T) {
	s := New(t.TempDir(), 2000)
	const name = "recorded-fact"
	const body = "The corrected source remains authoritative."
	marked := plantCorrectedSkill(t, s, name, body)
	var changes []Change
	tool := ReclassifyTools(s, func(change Change) { changes = append(changes, change) })[0].(tool.InvokableTool)
	out := run(t, tool, map[string]any{"action": "demote", "name": name, "content": body})
	if out["success"] != false || out["changed"] != false || len(changes) != 0 {
		t.Fatalf("tidy demoted corrected skill: out=%v changes=%+v", out, changes)
	}
	if notes, err := s.Read(); err != nil || len(notes.Entries) != 0 || !slices.Equal(marked, readCorrectionFixture(t, s.skillPath(name))) {
		t.Fatalf("tidy mutated protected memory: %+v (%v)", notes, err)
	}
	if result, err := s.DemoteSkill(name, body); err != nil || !result.Changed {
		t.Fatalf("explicit host demotion refused: %+v (%v)", result, err)
	}
}

func TestHumanSkillEditsMayReplaceOrDeleteACorrection(t *testing.T) {
	s, tools, _ := toolset(t, 2000)
	const name = "recorded-procedure"
	const body = "The corrected source remains authoritative."
	plantCorrectedSkill(t, s, name, body)
	if _, err := s.WriteSkill(name, "when checking recorded procedures", "A human replacement."); err != nil {
		t.Fatal(err)
	}
	if out := run(t, tools[ToolSkillManage], map[string]any{"action": "patch", "name": name, "old_text": "A human replacement.", "new_text": "A later supported replacement."}); out["success"] != true {
		t.Fatalf("human replacement left the old protection marker: %v", out)
	}
	plantCorrectedSkill(t, s, name, body)
	if err := s.DeleteSkill(name); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadSkill(name); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("human delete did not remove corrected skill: %v", err)
	}
}

func TestAgentSkillCreateCannotSetCorrectionMarker(t *testing.T) {
	s, tools, _ := toolset(t, 2000)
	const name = "agent-procedure"
	body := "---\ndream_correction: agent-claimed\n---\nFollow the observed procedure."
	args := map[string]any{"action": "create", "name": name, "description": "when checking a claimed marker", "content": body, skillCorrectionKey: "agent-claimed"}
	if out := run(t, tools[ToolSkillManage], args); out["success"] != true {
		t.Fatalf("create: %v", out)
	}
	front, _ := splitFrontMatter(string(readCorrectionFixture(t, s.skillPath(name))))
	if front[skillCorrectionKey] != "" {
		t.Fatal("model input created host correction metadata")
	}
	if out := run(t, tools[ToolSkillManage], map[string]any{"action": "delete", "name": name}); out["success"] != true {
		t.Fatalf("model text obtained host protection: %v", out)
	}
}

func TestFoldSkillFamiliesLeavesCorrectedFamilyIntact(t *testing.T) {
	for _, name := range []string{"weekly-rollup", "weekly-rollup-notes"} {
		t.Run(name, func(t *testing.T) {
			s := New(t.TempDir(), 2000)
			for _, fixture := range []string{"weekly-rollup", "weekly-rollup-notes", "weekly-rollup-send", "release-check-build", "release-check-run"} {
				plantSkill(t, s.dir, fixture, "when doing "+fixture, "Follow the procedure for "+fixture+".")
			}
			marked := plantCorrectedSkill(t, s, name, "Retain the original supported procedure.")
			report, err := s.FoldSkillFamiliesReport()
			if err != nil || len(report.Changes) != 1 || report.Changes[0].Name != "release-check" || report.Unchanged != 3 {
				t.Fatalf("fold changed a protected family or skipped an unrelated family: report=%+v err=%v", report, err)
			}
			if raw, err := os.ReadFile(s.skillPath(name)); err != nil || !slices.Equal(raw, marked) {
				t.Fatalf("protected skill changed: %q (%v)", raw, err)
			}
			want := []string{"release-check", "weekly-rollup", "weekly-rollup-notes", "weekly-rollup-send"}
			if names := skillNamesOf(t, s); !slices.Equal(names, want) {
				t.Fatalf("fold changed the protected family: %v", names)
			}
		})
	}
}

func TestFoldDoesNotOverwriteAnExistingProtectedGeneratedKeeper(t *testing.T) {
	s := New(t.TempDir(), 2000)
	const name = "weekly-rollup"
	marked := plantCorrectedSkill(t, s, name, "Retain the original supported procedure.")
	// Exercise the write boundary even when only the chapter snapshots were
	// provided as members and the generated target was omitted from the input.
	family := []Skill{{SkillInfo: SkillInfo{Name: name + "-notes"}}, {SkillInfo: SkillInfo{Name: name + "-send"}}}
	s.mu.Lock()
	_, change, err := s.foldOneFamilyLocked(family)
	s.mu.Unlock()
	if !errors.Is(err, errCorrectedSkill) || change != (Change{}) || !slices.Equal(marked, readCorrectionFixture(t, s.skillPath(name))) {
		t.Fatalf("fold rewrote a protected generated keeper: change=%+v err=%v", change, err)
	}
}
