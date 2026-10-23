package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

func reclassifyToolset(t *testing.T, s *Store) (tool.InvokableTool, *[]Change) {
	t.Helper()
	var changes []Change
	tools := ReclassifyTools(s, func(change Change) { changes = append(changes, change) })
	if len(tools) != 1 {
		t.Fatalf("reclassify tools=%d", len(tools))
	}
	info, err := tools[0].Info(context.Background())
	if err != nil || info.Name != ToolMemoryReclassify {
		t.Fatalf("Info=%+v err=%v", info, err)
	}
	return tools[0].(tool.InvokableTool), &changes
}

func TestTidyCanPromoteANoteWithoutLosingItsFacts(t *testing.T) {
	s := NewLimited(t.TempDir(), 1000, 200)
	note := "Verify the source identity before replacing the durable state."
	if _, _, err := s.Add(note); err != nil {
		t.Fatal(err)
	}
	tl, changes := reclassifyToolset(t, s)
	args := map[string]any{"action": "promote", "old_text": "source identity", "name": "source-verification", "description": "when replacing durable state", "content": note}
	out := run(t, tl, args)
	if out["success"] != true || out["changed"] != true {
		t.Fatalf("promote=%v", out)
	}
	assertNotes(t, s)
	sk, err := s.ReadSkill("source-verification")
	if err != nil || !strings.Contains(sk.Body, note) {
		t.Fatalf("source facts lost: %+v %v", sk, err)
	}
	if len(*changes) != 1 || (*changes)[0].Action != "promote" || (*changes)[0].Name != sk.Name {
		t.Fatalf("changes=%+v", *changes)
	}
	if out := run(t, tl, args); out["success"] != true || out["changed"] != false {
		t.Fatalf("retry=%v", out)
	}
	if len(*changes) != 1 {
		t.Fatalf("retry duplicated receipt: %+v", *changes)
	}
}

func TestPromotionCannotAppendUnsupportedFactsOrContradictions(t *testing.T) {
	const source = "Do not remove retained records before their verification is complete."
	for _, extra := range []string{
		"Inspect an unrelated external system before continuing.",
		"Remove retained records immediately without verification.",
		"The required retained record count is 37.",
	} {
		t.Run(extra, func(t *testing.T) {
			s := NewLimited(t.TempDir(), 1000, 200)
			if _, _, err := s.Add(source); err != nil {
				t.Fatal(err)
			}
			result, err := s.PromoteNote(source, "recorded-policy", "when enforcing retained record policy", source+"\n"+extra)
			if !errors.Is(err, ErrReclassifyCoverage) || result.Changed {
				t.Fatalf("promotion accepted an unsupported addition: %+v err=%v", result, err)
			}
			assertNotes(t, s, source)
			if _, err := s.ReadSkill("recorded-policy"); !errors.Is(err, ErrNoMatch) {
				t.Fatalf("refused promotion saved an unsupported skill: %v", err)
			}
		})
	}
}

func TestPromoteRefusesUncoveredOrAmbiguousNotesAndDifferentTargets(t *testing.T) {
	for _, tc := range []struct {
		name, old, description, body string
		existing                     bool
		want                         error
	}{
		{"coverage", "source identity", "when restoring state", "Perform unrelated actions.", false, ErrReclassifyCoverage},
		{"ambiguous", "Verify", "when restoring state", "Verify every source identity before replacement.", false, ErrAmbiguous},
		{"missing", "absent", "when restoring state", "Verify every source identity before replacement.", false, ErrNoMatch},
		{"existing differs", "source identity", "when restoring state", "Verify the source identity before replacing the durable state.", true, ErrDuplicateSkill},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewLimited(t.TempDir(), 1000, 200)
			if _, _, err := s.Add("Verify the source identity before replacing the durable state."); err != nil {
				t.Fatal(err)
			}
			if tc.name == "ambiguous" {
				if _, _, err := s.Add("Verify the checkpoint before restoring records."); err != nil {
					t.Fatal(err)
				}
			}
			if tc.existing {
				if _, err := s.WriteSkill("state-recovery", "when checking active ownership", "Inspect the current owner."); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := s.Read()
			res, err := s.PromoteNote(tc.old, "state-recovery", tc.description, tc.body)
			if !errors.Is(err, tc.want) || res.Changed || len(res.Changes) != 0 {
				t.Fatalf("result=%+v err=%v want=%v", res, err, tc.want)
			}
			after, _ := s.Read()
			if before.Rev != after.Rev {
				t.Fatal("refused promotion changed notes")
			}
		})
	}
}

func TestFailedPromotePreservesBothCopiesAndRetryFinishesOnce(t *testing.T) {
	s := NewLimited(t.TempDir(), 1000, 200)
	note := "The selected source identity must be verified before replacement."
	if _, _, err := s.Add(note); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(s.memoryPath()+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	tl, changes := reclassifyToolset(t, s)
	args := map[string]any{"action": "promote", "old_text": "selected source", "name": "source-check", "description": "when replacing selected state", "content": note}
	out := run(t, tl, args)
	if out["success"] != false || out["changed"] != true {
		t.Fatalf("partial result=%v", out)
	}
	assertNotes(t, s, note)
	if _, err := s.ReadSkill("source-check"); err != nil {
		t.Fatal("destination was not preserved", err)
	}
	if len(*changes) != 1 || (*changes)[0].Action != "create" {
		t.Fatalf("partial write missing from trace: %+v", *changes)
	}
	if err := os.Remove(s.memoryPath() + ".tmp"); err != nil {
		t.Fatal(err)
	}
	if out := run(t, tl, args); out["success"] != true || out["changed"] != true {
		t.Fatalf("retry=%v", out)
	}
	assertNotes(t, s)
	if len(*changes) != 2 || (*changes)[1].Action != "promote" {
		t.Fatalf("retry receipts=%+v", *changes)
	}
}

func TestTidyCanDemoteAShortFactAndRetryWithoutDuplicatingIt(t *testing.T) {
	s := NewLimited(t.TempDir(), 1000, 200)
	note := "The selected source identity remains authoritative."
	if _, err := s.WriteSkill("source-identity", "when choosing the durable source", note); err != nil {
		t.Fatal(err)
	}
	tl, changes := reclassifyToolset(t, s)
	args := map[string]any{"action": "demote", "name": "source-identity", "content": note}
	out := run(t, tl, args)
	if out["success"] != true || out["changed"] != true {
		t.Fatalf("demote=%v", out)
	}
	assertNotes(t, s, note)
	if _, err := s.ReadSkill("source-identity"); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("source skill retained: %v", err)
	}
	if len(*changes) != 1 || (*changes)[0].Action != "demote" {
		t.Fatalf("receipts=%+v", *changes)
	}
	if out := run(t, tl, args); out["success"] != true || out["changed"] != false {
		t.Fatalf("retry=%v", out)
	}
	if len(*changes) != 1 {
		t.Fatalf("retry duplicated receipt: %+v", *changes)
	}
}

func TestDemoteRefusesProceduresMissingFactsAndOverBudgetNotes(t *testing.T) {
	for _, tc := range []struct {
		name, body, note string
		limit, max       int
		want             error
	}{
		{"steps", "1. Inspect the selected state.\n2. Verify the restored source.", "Inspect the selected state.", 1000, 200, ErrSkillProcedure},
		{"new fact", "The selected source identity remains authoritative.", "An unrelated source is authoritative.", 1000, 200, ErrReclassifyCoverage},
		{"lost fact", "The selected source identity remains authoritative. Archived records must retain their original ownership.", "The selected source identity remains authoritative.", 1000, 200, ErrReclassifyCoverage},
		{"entry cap", "The selected source identity remains authoritative.", "The selected source identity remains authoritative.", 1000, 20, ErrEntryTooLong},
		{"total budget", "The selected source identity remains authoritative.", "The selected source identity remains authoritative.", 70, 70, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewLimited(t.TempDir(), tc.limit, tc.max)
			if tc.name == "total budget" {
				if _, _, err := s.Add("Keep every prior human preference."); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.WriteSkill("source-fact", "when choosing retained state", tc.body); err != nil {
				t.Fatal(err)
			}
			before, _ := s.Read()
			res, err := s.DemoteSkill("source-fact", tc.note)
			matched := errors.Is(err, tc.want)
			if tc.want == nil {
				var overflow *OverflowError
				matched = errors.As(err, &overflow)
			}
			if !matched || res.Changed || len(res.Changes) != 0 {
				t.Fatalf("result=%+v err=%v want=%v", res, err, tc.want)
			}
			after, _ := s.Read()
			if after.Rev != before.Rev {
				t.Fatal("refused demotion changed notes")
			}
			if _, err := s.ReadSkill("source-fact"); err != nil {
				t.Fatal("refusal removed source", err)
			}
		})
	}
}

func TestReclassificationToolsStayOutOfLiveReviewAndWorkerTools(t *testing.T) {
	s := New(t.TempDir(), 1000)
	if ReclassifyTools(nil, nil) != nil {
		t.Fatal("nil store advertised reclassification")
	}
	if names := ReclassifyNames(); len(names) != 1 || names[0] != ToolMemoryReclassify {
		t.Fatalf("names=%v", names)
	}
	for _, set := range [][]tool.BaseTool{Tools(s, nil), ViewTools(s), CatalogTools(s, nil)} {
		for _, tl := range set {
			info, _ := tl.Info(context.Background())
			if info.Name == ToolMemoryReclassify {
				t.Fatal("reclassification escaped the user-triggered tidy")
			}
		}
	}
	tl := ReclassifyTools(s, nil)[0].(tool.InvokableTool)
	for _, args := range []string{"{not json", `{"action":"replace"}`, `{"action":"promote","old_text":"x","name":"../bad"}`, `{"action":"demote","name":"missing","content":"A fact."}`} {
		out, err := tl.InvokableRun(context.Background(), args)
		if err != nil || !strings.Contains(out, `"success":false`) {
			t.Fatalf("refusal=%q err=%v", out, err)
		}
	}
}

func TestConcurrentPromoteMovesTheNoteOnlyOnce(t *testing.T) {
	s := NewLimited(t.TempDir(), 1000, 200)
	note := "Source identity verification precedes every durable state replacement."
	if _, _, err := s.Add(note); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	changed := 0
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := s.PromoteNote("Source identity", "source-verification", "when replacing durable state", note)
			if err != nil {
				t.Error(err)
				return
			}
			if res.Changed {
				mu.Lock()
				changed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if changed != 1 {
		t.Fatalf("changed %d times", changed)
	}
	assertNotes(t, s)
	if _, err := os.Stat(filepath.Join(s.skillsDir(), "source-verification", SkillFile)); err != nil {
		t.Fatal(err)
	}
}

func TestReclassificationRefusesIncompleteArgumentsDraftsAndUnreadableNotes(t *testing.T) {
	for _, tc := range []struct{ name, action string }{
		{"missing promote fields", "promote"}, {"bad demote name", "bad-name"},
		{"missing demote content", "demote-empty"}, {"note has list", "demote-list"},
		{"draft promote", "draft-promote"}, {"draft demote", "draft-demote"},
		{"unreadable promote", "unreadable-promote"}, {"unreadable demote", "unreadable-demote"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewLimited(t.TempDir(), 1000, 200)
			if strings.HasPrefix(tc.action, "draft-") {
				draft, err := s.BeginNotesDraft()
				if err != nil {
					t.Fatal(err)
				}
				s = draft.Store
			}
			if strings.HasPrefix(tc.action, "unreadable-") {
				if err := os.Mkdir(s.memoryPath(), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			var res ReclassifyResult
			var err error
			switch tc.action {
			case "promote":
				res, err = s.PromoteNote("", "source-fact", "", "")
			case "bad-name":
				res, err = s.DemoteSkill("../escape", "A fact.")
			case "demote-empty":
				res, err = s.DemoteSkill("source-fact", "  ")
			case "demote-list":
				res, err = s.DemoteSkill("source-fact", "- A fact.")
			case "draft-promote", "unreadable-promote":
				res, err = s.PromoteNote("source", "source-fact", "when selecting state", "The source remains authoritative.")
			default:
				res, err = s.DemoteSkill("source-fact", "The source remains authoritative.")
			}
			if err == nil || res.Changed || len(res.Changes) != 0 {
				t.Fatalf("result=%+v err=%v", res, err)
			}
		})
	}
}

func TestPromotePreservesTheNoteWhenItsDestinationCannotBeWritten(t *testing.T) {
	for _, tc := range []string{"unreadable target", "duplicate subject"} {
		t.Run(tc, func(t *testing.T) {
			s := NewLimited(t.TempDir(), 1000, 200)
			note := "Verify the selected source identity before replacing durable state."
			if _, _, err := s.Add(note); err != nil {
				t.Fatal(err)
			}
			if tc == "unreadable target" {
				if err := os.MkdirAll(s.skillPath("source-identity"), 0o700); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := s.WriteSkill("source-identity-known", "when inspecting prior source ownership", "Inspect the retained record."); err != nil {
					t.Fatal(err)
				}
			}
			res, err := s.PromoteNote("selected source", "source-identity", "when replacing state", note)
			if err == nil || res.Changed || len(res.Changes) != 0 {
				t.Fatalf("result=%+v err=%v", res, err)
			}
			assertNotes(t, s, note)
		})
	}
}

func TestDemoteKeepsAnExistingIdenticalNoteAndRejectsUnrelatedSkillOverlap(t *testing.T) {
	t.Run("existing identical note", func(t *testing.T) {
		s := New(t.TempDir(), 1000)
		note := "The source remains authoritative."
		if _, _, err := s.Add(note); err != nil {
			t.Fatal(err)
		}
		if _, err := s.WriteSkill("source-fact", "when selecting retained state", note); err != nil {
			t.Fatal(err)
		}
		res, err := s.DemoteSkill("source-fact", note)
		if err != nil || !res.Changed || len(res.Changes) != 1 || res.Changes[0].Action != "demote" {
			t.Fatalf("result=%+v err=%v", res, err)
		}
		assertNotes(t, s, note)
	})
	t.Run("other skill overlap", func(t *testing.T) {
		s := NewLimited(t.TempDir(), 1000, 200)
		note := "The selected source identity remains authoritative for durable state restoration."
		plantSkill(t, s.dir, "source-fact", "when selecting retained state", note)
		plantSkill(t, s.dir, "unrelated-procedure", note, "Check the active ownership before restoration.")
		res, err := s.DemoteSkill("source-fact", note)
		if !errors.Is(err, ErrNoteSkillOverlap) || res.Changed {
			t.Fatalf("result=%+v err=%v", res, err)
		}
		assertNotes(t, s)
	})
	t.Run("source body exceeds entry cap", func(t *testing.T) {
		s := NewLimited(t.TempDir(), 1000, 40)
		if _, err := s.WriteSkill("source-fact", "when selecting state", strings.Repeat("A lengthy retained fact. ", 8)); err != nil {
			t.Fatal(err)
		}
		res, err := s.DemoteSkill("source-fact", "A lengthy retained fact.")
		if !errors.Is(err, ErrEntryTooLong) || res.Changed {
			t.Fatalf("result=%+v err=%v", res, err)
		}
		assertNotes(t, s)
	})
}

// A token-overlap score can miss one changed negation or number. It is useful
// for finding related memory, but cannot authorize deleting the only source.
func TestReclassificationPreservesNegationNumbersAndEverySourceWord(t *testing.T) {
	for _, tc := range []struct{ name, source, target string }{
		{"negation", "Do not remove selected source records before the operator approves this irreversible action.", "Do remove selected source records before the operator approves this irreversible action."},
		{"number", "Every durable source must retain at least 12 independent verification records before replacement.", "Every durable source must retain at least 13 independent verification records before replacement."},
		{"condition", "Keep the selected source available until every independent verification receipt has been checked.", "Keep the selected source available after every independent verification receipt has been checked."},
	} {
		for _, action := range []string{"promote", "demote"} {
			t.Run(tc.name+"/"+action, func(t *testing.T) {
				s := NewLimited(t.TempDir(), 1000, 200)
				var result ReclassifyResult
				var err error
				if action == "promote" {
					if _, _, err := s.Add(tc.source); err != nil {
						t.Fatal(err)
					}
					result, err = s.PromoteNote(tc.source, "source-facts", "when replacing retained state", tc.target)
				} else {
					if _, err := s.WriteSkill("source-facts", "when replacing retained state", tc.source); err != nil {
						t.Fatal(err)
					}
					result, err = s.DemoteSkill("source-facts", tc.target)
				}
				if !errors.Is(err, ErrReclassifyCoverage) || result.Changed {
					t.Fatalf("altered source accepted: result=%+v err=%v", result, err)
				}
				if action == "promote" {
					assertNotes(t, s, tc.source)
				} else {
					if skill, err := s.ReadSkill("source-facts"); err != nil || skill.Body != tc.source {
						t.Fatalf("source changed: %+v %v", skill, err)
					}
				}
			})
		}
	}
}

func TestDemoteAllowsWhitespaceChangesAndRefusesAnExtraAssertion(t *testing.T) {
	for _, extra := range []bool{false, true} {
		t.Run(map[bool]string{false: "whitespace", true: "extra assertion"}[extra], func(t *testing.T) {
			s := NewLimited(t.TempDir(), 1000, 200)
			body := "The selected source\nidentity remains authoritative."
			if _, err := s.WriteSkill("source-facts", "when selecting state", body); err != nil {
				t.Fatal(err)
			}
			content := "The selected source identity remains authoritative."
			if extra {
				content += " Other retained records may be deleted."
			}
			result, err := s.DemoteSkill("source-facts", content)
			if extra {
				if !errors.Is(err, ErrReclassifyCoverage) || result.Changed {
					t.Fatalf("extra assertion=%+v %v", result, err)
				}
				return
			}
			if err != nil || !result.Changed {
				t.Fatalf("whitespace-only=%+v %v", result, err)
			}
			assertNotes(t, s, content)
		})
	}
}

func TestDemoteReportsAPartiallyRemovedSourceWithoutClaimingAMoveCompleted(t *testing.T) {
	for _, alreadyNoted := range []bool{false, true} {
		t.Run(map[bool]string{false: "new note", true: "existing note"}[alreadyNoted], func(t *testing.T) {
			s := NewLimited(t.TempDir(), 1000, 200)
			note := "The selected source identity remains authoritative."
			if alreadyNoted {
				if _, _, err := s.Add(note); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.WriteSkill("source-facts", "when selecting retained state", note); err != nil {
				t.Fatal(err)
			}
			blocked := filepath.Join(s.skillsDir(), "source-facts", "blocked")
			if err := os.Mkdir(blocked, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(blocked, "held"), []byte("private case data"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(blocked, 0o000); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(blocked, 0o700) })
			tl, changes := reclassifyToolset(t, s)
			args := map[string]any{"action": "demote", "name": "source-facts", "content": note}
			out := run(t, tl, args)
			if out["success"] != false || out["changed"] != true {
				t.Fatalf("partial result=%v", out)
			}
			assertNotes(t, s, note)
			if _, err := s.ReadSkill("source-facts"); !errors.Is(err, ErrNoMatch) {
				t.Fatalf("fixture source still readable: %v", err)
			}
			want := "add,delete"
			if alreadyNoted {
				want = "delete"
			}
			var actions []string
			for _, change := range *changes {
				actions = append(actions, change.Action)
			}
			if strings.Join(actions, ",") != want {
				t.Fatalf("actual writes=%v want %s", actions, want)
			}
			if err := os.Chmod(blocked, 0o700); err != nil {
				t.Fatal(err)
			}
			if out := run(t, tl, args); out["success"] != true || out["changed"] != false {
				t.Fatalf("state-idempotent retry=%v", out)
			}
			if len(*changes) != len(actions) {
				t.Fatalf("retry invented a receipt: %+v", *changes)
			}
		})
	}
}
