package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

// A repair needs the write's full result and exact content allowance, not an
// instruction that mistakes additional savings for the resulting free space.
func TestMemoryCapacityRefusalGivesAnExactContentBudget(t *testing.T) {
	for _, tc := range []struct {
		name, initial, action, content string
		limit, entryMax                int
		proposed, available, max       int
	}{
		{name: "add separator", initial: "abcdefgh", action: "add", content: " \n猫犬鳥魚 \n\n", limit: 12, proposed: 14, available: 4, max: 2},
		{name: "normalized internal whitespace", initial: "abcdefgh", action: "add", content: "\t猫犬  \n\n鳥魚 \t", limit: 12, proposed: 15, available: 4, max: 2},
		{name: "replace unicode", initial: "猫犬\n\nabcdefgh", action: "replace", content: " \n猫犬鳥魚馬 \n\n", limit: 14, proposed: 15, available: 2, max: 4},
		{name: "empty store", action: "add", content: "猫犬鳥魚", limit: 3, proposed: 4, available: 3, max: 3},
		{name: "no room for separator", initial: "abcde", action: "add", content: "猫犬", limit: 5, proposed: 9, available: 0, max: 0},
		{name: "entry cap", initial: "abcdefgh", action: "add", content: "猫犬鳥", limit: 12, entryMax: 3, proposed: 13, available: 4, max: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewLimited(t.TempDir(), tc.limit, tc.entryMax)
			before, err := s.Overwrite(tc.initial)
			if err != nil {
				t.Fatal(err)
			}
			mem := Tools(s, nil)[0].(tool.InvokableTool)
			args := map[string]any{"action": tc.action, "content": tc.content, "old_text": "猫犬"}
			res := run(t, mem, args)
			if res["success"] != false || res["proposed_usage"] != fmt.Sprintf("%d/%d", tc.proposed, tc.limit) || res["available_chars"] != float64(tc.available) || res["max_content_chars"] != float64(tc.max) {
				t.Fatalf("capacity feedback does not identify the exact repair budget: %v", res)
			}
			_, hasEntries := res["current_entries"]
			if res["usage"] != fmt.Sprintf("%d/%d", before.Chars, tc.limit) || res["over_by"] != float64(tc.proposed-tc.limit) || !hasEntries {
				t.Fatalf("existing capacity feedback changed: %v", res)
			}
			instruction, _ := res["error"].(string)
			if !strings.Contains(instruction, fmt.Sprintf("at least %d additional characters", tc.proposed-tc.limit)) || strings.Contains(instruction, "characters are free") {
				t.Fatalf("extra savings were confused with total free space: %s", instruction)
			}
			after, err := s.Read()
			if err != nil || after.Rev != before.Rev {
				t.Fatalf("refusal changed real notes: %+v %v", after, err)
			}
			if tc.max == 0 {
				return
			}
			args["content"] = strings.Repeat("好", tc.max+1)
			if oversized := run(t, mem, args); oversized["success"] != false {
				t.Fatalf("one rune above the reported content budget must refuse: %v", oversized)
			}
			args["content"] = strings.Repeat("好", tc.max)
			if repaired := run(t, mem, args); repaired["success"] != true {
				t.Fatalf("the reported exact content budget must fit: %v", repaired)
			}
		})
	}
}

// The per-entry guard must not hide a simultaneous total overflow, or the
// next model proposal merely discovers a second capacity failure.
func TestEntryCapacityRefusalAlsoPlansTheTotalBudget(t *testing.T) {
	for _, tc := range []struct {
		name, initial, action, oldText, content string
		limit, entryMax, proposed, over, max    int
	}{
		{name: "add both limits", initial: "abcdefgh", action: "add", content: " \n猫犬鳥魚馬 \n", limit: 12, entryMax: 3, proposed: 15, over: 3, max: 2},
		{name: "replace both limits", initial: "猫犬\n\nabcdefgh", action: "replace", oldText: "猫犬", content: "猫犬鳥魚馬", limit: 14, entryMax: 3, proposed: 15, over: 1, max: 3},
		{name: "add entry limit only", initial: "abc", action: "add", content: "猫犬鳥魚", limit: 20, entryMax: 3, proposed: 9, max: 3},
		{name: "replace entry limit only", initial: "猫犬\n\nabcdefgh", action: "replace", oldText: "猫犬", content: "猫犬鳥魚馬", limit: 20, entryMax: 3, proposed: 15, max: 3},
		{name: "empty store both limits", action: "add", content: "猫犬鳥魚馬", limit: 4, entryMax: 2, proposed: 5, over: 1, max: 2},
		{name: "no content can fit", initial: "abcdefgh", action: "add", content: "猫犬", limit: 8, entryMax: 1, proposed: 12, over: 4},
		{name: "duplicate legacy note", initial: "猫犬鳥魚馬", action: "add", content: "猫犬鳥魚馬", limit: 10, entryMax: 3, proposed: 5, max: 3},
		{name: "duplicate cannot become a shorter addition", initial: "猫犬鳥魚馬", action: "add", content: "猫犬鳥魚馬", limit: 6, entryMax: 3, proposed: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewLimited(t.TempDir(), tc.limit, tc.entryMax)
			before, err := s.Overwrite(tc.initial)
			if err != nil {
				t.Fatal(err)
			}
			mem := Tools(s, nil)[0].(tool.InvokableTool)
			args := map[string]any{"action": tc.action, "old_text": tc.oldText, "content": tc.content}
			res := run(t, mem, args)
			if res["success"] != false || res["chars"] == nil || res["entry_max"] != float64(tc.entryMax) || res["usage"] != fmt.Sprintf("%d/%d", before.Chars, tc.limit) || res["proposed_usage"] != fmt.Sprintf("%d/%d", tc.proposed, tc.limit) || res["over_by"] != float64(tc.over) || res["available_chars"] != float64(tc.limit-before.Chars) || res["max_content_chars"] != float64(tc.max) {
				t.Fatalf("entry refusal hides the total capacity plan: %v", res)
			}
			if tc.action == "replace" && res["matched"] != tc.oldText {
				t.Fatalf("replacement allowance is not tied to the actual matched note: %v", res)
			}
			after, err := s.Read()
			if err != nil || after.Rev != before.Rev {
				t.Fatalf("capacity planning changed notes: %+v %v", after, err)
			}
			if tc.max > 0 {
				args["content"] = strings.Repeat("好", tc.max)
				if repaired := run(t, mem, args); repaired["success"] != true {
					t.Fatalf("the joint entry/total budget must fit: %v", repaired)
				}
			}
		})
	}
}

func TestEntryCapacityRefusalDoesNotInventAReplacementBudget(t *testing.T) {
	for _, oldText := range []string{"oak", "absent"} {
		t.Run(oldText, func(t *testing.T) {
			s := NewLimited(t.TempDir(), 14, 2)
			before, err := s.Overwrite("oak-a\n\noak-b")
			if err != nil {
				t.Fatal(err)
			}
			res := run(t, Tools(s, nil)[0].(tool.InvokableTool), map[string]any{"action": "replace", "old_text": oldText, "content": "猫犬鳥魚"})
			if res["success"] != false || res["entry_max"] != float64(2) || !strings.Contains(res["error"].(string), "matches") {
				t.Fatalf("the entry and matching defects must both be visible: %v", res)
			}
			for _, field := range []string{"proposed_usage", "max_content_chars", "matched"} {
				if _, exists := res[field]; exists {
					t.Fatalf("unresolved note identity invented %s: %v", field, res)
				}
			}
			after, err := s.Read()
			if err != nil || after.Rev != before.Rev {
				t.Fatalf("failed match changed notes: %+v %v", after, err)
			}
		})
	}
}

func TestEntryCapacityRefusalDoesNotInventAnUnreadableStoreBudget(t *testing.T) {
	s := NewLimited(t.TempDir(), 12, 2)
	if err := os.Mkdir(filepath.Join(s.Dir(), MemoryFile), 0o700); err != nil {
		t.Fatal(err)
	}
	res := run(t, Tools(s, nil)[0].(tool.InvokableTool), map[string]any{"action": "add", "content": "猫犬鳥魚"})
	if res["success"] != false || res["entry_max"] != float64(2) {
		t.Fatalf("unreadable preview lost the known entry limit: %v", res)
	}
	for _, field := range []string{"usage", "proposed_usage", "available_chars", "max_content_chars"} {
		if _, exists := res[field]; exists {
			t.Fatalf("unreadable notes invented %s: %v", field, res)
		}
	}
}

func TestReviewCapacityRepairPreservesMeaningAndResubmitsTheWholeProposal(t *testing.T) {
	prompt := ReviewPrompt()
	for _, required := range []string{
		"proposed_usage", "available_chars", "max_content_chars",
		"automatically repair and resubmit the complete batch",
		"related replace", "merge this turn's supported facts",
		"Preserve existing facts, conditions, negation, scope and numeric spelling",
		"Do not truncate", "do not treat numbers in unrelated old notes as evidence",
	} {
		if !strings.Contains(prompt, required) {
			t.Errorf("capacity repair lacks %q", required)
		}
	}
}
