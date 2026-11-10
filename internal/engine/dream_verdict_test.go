package engine

import (
	"slices"
	"strings"
	"testing"
)

func TestDreamVerdictRequiresAnExactNewClaimAndIndependentObservations(t *testing.T) {
	before := "Verify the manifest before accepting an import."
	after := before + "\nA successful transfer is always a complete import."
	sources := []string{"1", "2"}
	for _, tc := range []struct {
		name, raw, decision string
	}{
		{"revert", `{"decision":"revert_using_1_and_2","quote":"A successful transfer is always a complete import."}`, "revert"},
		{"matching repeated source list", `{"decision":"revert_using_1_and_2","quote":"A successful transfer","sources":["2","1"]}`, "revert"},
		{"keep", `{"decision":"keep"}`, "keep"},
		{"defer", `{"decision":"defer"}`, "defer"},
		{"keep with source", `{"decision":"keep","sources":["1"]}`, "keep"},
		{"uncertain fenced JSON", "```json\n{\"decision\":\"uncertain\",\"quote\":\"\",\"sources\":[]}\n```", "uncertain"},
		{"one observation", `{"decision":"revert_using_1_and_2","quote":"A successful transfer","sources":["1"]}`, ""},
		{"old claim", `{"decision":"revert_using_1_and_2","quote":"Verify the manifest","sources":["1","2"]}`, ""},
		{"paraphrase", `{"decision":"revert_using_1_and_2","quote":"Every successful transfer is complete","sources":["1","2"]}`, ""},
		{"short quote", `{"decision":"revert_using_1_and_2","quote":"is","sources":["1","2"]}`, ""},
		{"unknown evidence", `{"decision":"revert_using_1_and_2","quote":"A successful transfer","sources":["1","invented"]}`, ""},
		{"duplicate evidence", `{"decision":"revert_using_1_and_2","quote":"A successful transfer","sources":["1","1"]}`, ""},
		{"stray text", `{"decision":"keep"} {}`, ""},
		{"old tuple shape", `["keep",""]`, ""},
		{"empty object", `{}`, ""},
		{"revert without quote", `{"decision":"revert_using_1_and_2","sources":["1","2"]}`, ""},
		{"bare revert", `{"decision":"revert","quote":"A successful transfer","sources":["1","2"]}`, ""},
		{"invented pair", `{"decision":"revert_using_1_and_3","quote":"A successful transfer"}`, ""},
		{"reversed pair", `{"decision":"revert_using_2_and_1","quote":"A successful transfer"}`, ""},
		{"same source twice", `{"decision":"revert_using_1_and_1","quote":"A successful transfer"}`, ""},
		{"unrecognized", `{"decision":"apply"}`, ""},
		{"uncertain with quote", `{"decision":"uncertain","quote":"A successful transfer"}`, "uncertain"},
		{"keep with quote", `{"decision":"keep","quote":"A successful transfer"}`, "keep"},
		{"wrong type", `{"decision":"keep","sources":[1]}`, ""},
		{"null decision", `{"decision":null}`, ""},
		{"null quote", `{"decision":"keep","quote":null}`, ""},
		{"null sources", `{"decision":"keep","sources":null}`, ""},
		{"null ID", `{"decision":"keep","sources":[null]}`, ""},
		{"null field", `null`, ""},
		{"unknown field", `{"decision":"keep","other":[]}`, ""},
		{"duplicate decision", `{"decision":"keep","decision":"uncertain"}`, ""},
		{"duplicate quote", `{"decision":"keep","quote":"","quote":""}`, ""},
		{"duplicate sources", `{"decision":"keep","sources":[],"sources":[]}`, ""},
		{"unclosed fence", "```json\n{\"decision\":\"keep\"}", ""},
		{"too large", `{"decision":"keep"}` + strings.Repeat(" ", 4096), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verdict, reason := parseDreamVerdict(tc.raw, before, after, sources)
			ok := reason == ""
			if ok && (verdict.Decision == "keep" || verdict.Decision == "uncertain") && verdict.Quote != "" {
				t.Fatal("a no-write decision retained a redundant quote")
			}
			if tc.decision == "revert" && !slices.Equal(verdict.Sources, sources) {
				t.Fatalf("revert did not resolve the explicit pair: %+v", verdict)
			}
			if ok != (tc.decision != "") || verdict.Decision != tc.decision {
				t.Fatalf("verdict = %+v, accepted = %v, want %q", verdict, ok, tc.decision)
			}
		})
	}
}

func TestDreamDeferRejectsProofFields(t *testing.T) {
	for _, raw := range []string{
		`{"decision":"defer","quote":""}`,
		`{"decision":"defer","sources":[]}`,
		`{"decision":"defer","sources":["4","6"]}`,
	} {
		verdict, reason := parseDreamVerdict(raw, "prior rule", "different rule", []string{"4", "6"})
		if reason != "unexpected_defer_proof" || verdict.Decision != "" {
			t.Errorf("%s was not refused as proof-bearing defer: %+v %q", raw, verdict, reason)
		}
	}
}

func TestDreamPairRejectsConflictingSupplementalReferences(t *testing.T) {
	for _, raw := range []string{
		`{"decision":"revert_using_1_and_2","quote":"different rule","sources":["1","3"]}`,
		`{"decision":"revert_using_1_and_2","quote":"different rule","sources":["1","2","3"]}`,
		`{"decision":"revert_using_1_and_2","quote":"different rule","sources":["3","2"]}`,
	} {
		verdict, reason := parseDreamVerdict(raw, "prior rule", "different rule", []string{"1", "2", "3"})
		if reason != "source_pair_mismatch" || verdict.Decision != "" {
			t.Errorf("%s did not reject inconsistent proof: %+v %q", raw, verdict, reason)
		}
	}
	if choices := dreamRevertChoices([]string{"7", "7"}); len(choices) != 0 {
		t.Fatalf("the same observation cannot be offered twice in one pair: %+v", choices)
	}
}
