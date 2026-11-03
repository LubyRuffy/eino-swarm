package engine

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestNuwaReceiptInputEvidencePresence(t *testing.T) {
	for _, tc := range []struct {
		name, args, errorText string
		supplied              bool
		ownIDs                []string
	}{
		{name: "omit preserves", args: `{"title":"updated"} `},
		{name: "own empty clears", args: `{"own_tool_call_ids":[]}`, supplied: true, ownIDs: []string{}},
		{name: "own call", args: `{"own_tool_call_ids":["observed"]}`, supplied: true, ownIDs: []string{"observed"}},
		{name: "own case variant", args: `{"OWN_TOOL_CALL_IDS":["observed"]}`, supplied: true, ownIDs: []string{"observed"}},
		{name: "literal empty clears", args: `{"evidence":[]}`, supplied: true},
		{name: "literal null keeps legacy behavior", args: `{"evidence":null}`, supplied: true},
		{name: "explicit own null refused", args: `{"own_tool_call_ids": null }`, errorText: "must be an array"},
		{name: "two empty arrays still conflict", args: `{"evidence":[],"own_tool_call_ids":[]}`, errorText: "mutually exclusive"},
		{name: "case folded fields still conflict", args: `{"EVIDENCE":[],"OWN_TOOL_CALL_IDS":[]}`, errorText: "mutually exclusive"},
		{name: "case folded own null refused", args: `{"OWN_TOOL_CALL_IDS":null}`, errorText: "must be an array"},
		{name: "non-string IDs refused", args: `{"own_tool_call_ids":[123]}`, errorText: "must be an array"},
		{name: "mixed spellings refuse before hashing", args: `{"own_tool_call_ids":["first"],"OWN_TOOL_CALL_IDS":["second"]}`, errorText: "multiple spellings"},
		{name: "mixed spellings refuse in either order", args: `{"OWN_TOOL_CALL_IDS":["second"],"own_tool_call_ids":["first"]}`, errorText: "multiple spellings"},
		{name: "duplicate key cannot supply an ID from a discarded value", args: `{"own_tool_call_ids":["discarded"],"own_tool_call_ids":[null]}`, supplied: true, ownIDs: []string{""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var input recordWorkArgs
			if err := decodeNuwaArgs(tc.args, &input); err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(tc.args), &fields); err != nil {
				t.Fatal(err)
			}
			supplied, ownIDs, err := decodeNuwaEvidenceFields(fields)
			if tc.errorText != "" {
				if err == nil || !strings.Contains(err.Error(), tc.errorText) {
					t.Fatalf("expected %q, got %v", tc.errorText, err)
				}
				return
			}
			if err != nil || supplied != tc.supplied || !reflect.DeepEqual(ownIDs, tc.ownIDs) {
				t.Fatalf("supplied=%v ownIDs=%v err=%v, want supplied=%v ownIDs=%v",
					supplied, ownIDs, err, tc.supplied, tc.ownIDs)
			}
		})
	}
}
