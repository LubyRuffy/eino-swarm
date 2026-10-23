package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func tidyReclassifyMessages(t *testing.T, catalog string, entries []string, entryMax int) []*schema.Message {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"entries": entries, "entry_max": entryMax, "char_limit": 2000, "chars": 0})
	if err != nil {
		t.Fatal(err)
	}
	return []*schema.Message{schema.UserMessage("Catalog to curate:\n\n" + catalog + "\n\n" + tidyNotesMark + string(raw))}
}

func tidyScriptArgs(t *testing.T, msg *schema.Message, name string) map[string]any {
	t.Helper()
	if msg == nil || len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != name {
		t.Fatalf("want %s call, got %+v", name, msg)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(msg.ToolCalls[0].Function.Arguments), &args); err != nil {
		t.Fatal(err)
	}
	return args
}

func TestOfflineTidyPromotesOnlyRecordedExplicitSteps(t *testing.T) {
	for _, marker := range []string{"1. ", "2) ", "- ", "* ", "+ "} {
		t.Run(marker, func(t *testing.T) {
			note := marker + "Inspect retained inputs.\n" + marker + "Verify the observed output."
			msgs := tidyReclassifyMessages(t, "", []string{"An established constraint remains in force.", note}, 100)
			out := catalogTidyScript(1, msgs)
			args := tidyScriptArgs(t, out, "memory_reclassify")
			if args["action"] != "promote" || args["content"] != note || args["old_text"] != note || args["name"] != "recorded-method-"+conversationTag(note) {
				t.Fatalf("promotion did not preserve the recorded source: %v", args)
			}
			if !strings.HasPrefix(args["description"].(string), "When ") {
				t.Fatalf("description does not state applicability: %v", args)
			}
			for _, leak := range []string{"notes.md", "weekly", "release", "fofa", "python", "git "} {
				if strings.Contains(strings.ToLower(out.ToolCalls[0].Function.Arguments), leak) {
					t.Fatalf("offline script leaked an unrelated example %q: %v", leak, args)
				}
			}
		})
	}
}

func TestOfflineTidyViewsBeforeDemotingTheExactStoredBody(t *testing.T) {
	const body = "Established source identity remains authoritative."
	msgs := tidyReclassifyMessages(t, "- recorded-constraint — When validating an established constraint", nil, 100)
	view := catalogTidyScript(1, msgs)
	if args := tidyScriptArgs(t, view, "skill_view"); args["name"] != "recorded-constraint" {
		t.Fatalf("view=%v", args)
	}
	raw, _ := json.Marshal(map[string]any{"success": true, "name": "recorded-constraint", "body": body})
	msgs = append(msgs, view, schema.ToolMessage(string(raw), view.ToolCalls[0].ID))
	move := catalogTidyScript(2, msgs)
	args := tidyScriptArgs(t, move, "memory_reclassify")
	if args["action"] != "demote" || args["name"] != "recorded-constraint" || args["content"] != body {
		t.Fatalf("demotion changed source facts: %v", args)
	}
	msgs = append(msgs, move, schema.ToolMessage(`{"success":true,"changed":true}`, move.ToolCalls[0].ID))
	if done := catalogTidyScript(3, msgs); len(done.ToolCalls) != 0 || !strings.Contains(done.Content, "reclassified") {
		t.Fatalf("successful move did not terminate: %+v", done)
	}
}

func TestOfflineReclassificationStopsOnMissingOrUnsafeInput(t *testing.T) {
	for _, tc := range []struct {
		name, catalog, result string
		entries               []string
		entryMax              int
	}{
		{name: "no skills or steps", entries: []string{"A durable constraint."}, entryMax: 100},
		{name: "invalid empty skill name", catalog: "-  — When applicable", entryMax: 100},
		{name: "view refused", catalog: "- stored-constraint — When applicable", result: `{"success":false,"error":"unavailable"}`, entryMax: 100},
		{name: "malformed view", catalog: "- stored-constraint — When applicable", result: "{broken", entryMax: 100},
		{name: "missing body", catalog: "- stored-constraint — When applicable", result: `{"success":true,"name":"stored-constraint"}`, entryMax: 100},
		{name: "missing name", catalog: "- stored-constraint — When applicable", result: `{"success":true,"body":"A durable constraint."}`, entryMax: 100},
		{name: "body exceeds cap", catalog: "- stored-constraint — When applicable", result: `{"success":true,"name":"stored-constraint","body":"A durable constraint."}`, entryMax: 4},
		{name: "no note cap", catalog: "- stored-constraint — When applicable", result: `{"success":true,"name":"stored-constraint","body":"A durable constraint."}`},
		{name: "multiline body", catalog: "- stored-constraint — When applicable", result: `{"success":true,"name":"stored-constraint","body":"First constraint.\nSecond constraint."}`, entryMax: 100},
		{name: "procedural body", catalog: "- stored-constraint — When applicable", result: `{"success":true,"name":"stored-constraint","body":"1. Inspect the input."}`, entryMax: 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msgs := tidyReclassifyMessages(t, tc.catalog, tc.entries, tc.entryMax)
			if tc.result != "" {
				msgs = append(msgs, nil, schema.ToolMessage(tc.result, "mock-tidy-view"))
			}
			out, handled := catalogReclassificationScript(msgs)
			if !handled || out == nil || len(out.ToolCalls) != 0 {
				t.Fatalf("unsafe input generated a move: handled=%v out=%+v", handled, out)
			}
		})
	}
	for _, msgs := range [][]*schema.Message{nil, {nil, schema.UserMessage("Catalog to curate:\n\n(none)")}} {
		if out, handled := catalogReclassificationScript(msgs); handled || out != nil {
			t.Fatalf("library without notes acquired move capability: %v %+v", handled, out)
		}
	}
	if out, handled := catalogReclassificationScript([]*schema.Message{schema.UserMessage("Catalog to curate:\n\n" + tidyNotesMark + "{broken")}); !handled || out == nil || len(out.ToolCalls) != 0 || !strings.Contains(out.Content, "could not be read") {
		t.Fatalf("malformed notes were not reported: %v %+v", handled, out)
	}
}

func TestOfflineReclassificationNeverRetriesAnAttemptedMove(t *testing.T) {
	for _, tc := range []struct{ name, result, want string }{
		{"success", `{"success":true}`, "reclassified"},
		{"refusal", `{"success":false,"error":"refused"}`, "not completed"},
		{"invalid result", "{broken", "not completed"},
		{"no result", "", "already requested"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msgs := tidyReclassifyMessages(t, "", []string{"1. Inspect retained inputs."}, 100)
			msgs = append(msgs, nil, mockTidyCall("prior-move", "memory_reclassify", map[string]any{"action": "promote"}))
			if tc.result != "" {
				msgs = append(msgs, schema.ToolMessage(tc.result, "prior-move"))
			}
			out, handled := catalogReclassificationScript(msgs)
			if !handled || len(out.ToolCalls) != 0 || !strings.Contains(out.Content, tc.want) {
				t.Fatalf("attempted move repeated or misreported: %+v", out)
			}
		})
	}
	msgs := tidyReclassifyMessages(t, "- stored-constraint — When applicable", nil, 100)
	msgs = append(msgs, mockTidyCall("mock-tidy-view", "skill_view", map[string]any{"name": "stored-constraint"}), schema.ToolMessage(`{"success":true}`, "unrelated-call"))
	if out, handled := catalogReclassificationScript(msgs); !handled || len(out.ToolCalls) != 0 || !strings.Contains(out.Content, "not available") {
		t.Fatalf("a view with no result was repeated: %+v", out)
	}
}

func TestOfflineTidyKeepsMergePriorityAndFindsTheActualCatalogMessage(t *testing.T) {
	msgs := tidyReclassifyMessages(t, "These recorded skills must become one skill\n- source-a, source-b", []string{"1. Inspect retained inputs."}, 100)
	first := catalogTidyScript(1, msgs)
	if args := tidyScriptArgs(t, first, "skill_manage"); args["action"] != "merge" {
		t.Fatalf("reclassification replaced a required merge: %v", args)
	}
	msgs = append(msgs, first, schema.ToolMessage(`{"success":true}`, first.ToolCalls[0].ID))
	if done := catalogTidyScript(2, msgs); len(done.ToolCalls) != 0 || !strings.Contains(done.Content, "Folded") {
		t.Fatalf("merge continuation acquired an unrelated move: %+v", done)
	}
	actual := tidyReclassifyMessages(t, "", []string{"1. Inspect retained inputs."}, 100)
	actual = append([]*schema.Message{nil, schema.UserMessage("Earlier context without a catalog.")}, actual...)
	model := newMockModel(reviewerRole)
	first, err := model.Generate(context.Background(), actual)
	if err != nil {
		t.Fatal(err)
	}
	if args := tidyScriptArgs(t, first, "memory_reclassify"); args["action"] != "promote" {
		t.Fatalf("earlier user framing hid the actual catalog: %v", args)
	}
	if out := catalogTidyScript(2, tidyReclassifyMessages(t, "", nil, 100)); len(out.ToolCalls) != 0 {
		t.Fatalf("an empty continuation invented work: %+v", out)
	}
}
