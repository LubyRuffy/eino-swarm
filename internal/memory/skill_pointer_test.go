package memory

import (
	"errors"
	"testing"
)

func TestNamingASkillCannotHideItsCompleteBodyInsideANote(t *testing.T) {
	const description = "when validating durable input ownership before replacing state"
	for _, tc := range []struct{ name, body, note string }{
		{"short body", "Inspect records.", "When validating durable input ownership before replacing state, use quick-check: Inspect records."},
		{"short body only", "Inspect records.", "quick-check: Inspect records."},
		{"extra framing dilutes overlap", "Inspect retained source records and validate durable output.", "When validating durable input ownership before replacing state, use quick-check: Inspect retained source records and validate durable output."},
		{"normalized body", "Inspect retained records.\nVerify restored output.", "Use quick-check: INSPECT retained records. Verify restored output."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewLimited(t.TempDir(), 3000, 500)
			if _, err := s.WriteSkill("quick-check", description, tc.body); err != nil {
				t.Fatal(err)
			}
			if _, _, err := s.Add(tc.note); !errors.Is(err, ErrNoteSkillOverlap) {
				t.Fatalf("naming a skill bypassed its complete body check: %v", err)
			}
			if _, _, err := s.Add("The human prefers concise answers."); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Replace("The human prefers concise answers.", tc.note); !errors.Is(err, ErrNoteSkillOverlap) {
				t.Fatalf("replacement bypassed its complete body check: %v", err)
			}
		})
	}
}

func TestPointerBodyCheckHandlesMissingAndUnrelatedBodies(t *testing.T) {
	for _, tc := range []struct{ note, body string }{
		{"Use the recorded skill.", " \n "},
		{"Use the recorded skill.", "Inspect retained inputs."},
	} {
		if noteCopiesSkillBody(tc.note, tc.body) {
			t.Fatalf("a pointer copied an empty or unrelated body: %+v", tc)
		}
	}
}
