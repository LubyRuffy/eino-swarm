package engine

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A repair needs the whole set of bad citations, without making an omitted
// source or a tool call authoritative just because the model referenced it.
func TestReviewRepairListsBadCitationsAndOnlyVisibleSources(t *testing.T) {
	batch := testReviewBatch(t, map[int64]store.Event{
		8: {Seq: 8, Kind: KindUser, Text: "Keep concise reports."},
		3: {Seq: 3, Kind: swarm.NotifyToolResult.String(), Text: `{"output":"limit=63\\nowner=verified"}`},
	})
	op := reviewOperation{Target: memory.ToolMemory, Action: "add", Content: "Keep concise reports.", Evidence: []reviewCitation{
		{Seq: 2, Quote: "a call cannot verify its own arguments"},
		{Seq: 3, Quote: "limit=63\nowner=verified"},
		{Seq: 8, Quote: " \n"},
		{Seq: 8, Quote: "concise reports"},
		{Seq: 3, Quote: "another wrong quote"},
	}}
	response := reviewRepairResponse(t, batch, []reviewOperation{op})
	if response.Index != 0 || response.Code != "evidence" || !reflect.DeepEqual(response.Repair.CitableSources, []int64{3, 8}) {
		t.Fatalf("cannot locate/refine the refused operation: %+v", response)
	}
	issues := response.Repair.InvalidEvidence
	if len(issues) != 4 || issues[0].Index != 0 || issues[0].Seq != 2 || issues[0].Reason != "source_unavailable" || issues[0].SourceText != "" || issues[1].Index != 1 || issues[1].Reason != "quote_mismatch" || issues[1].SourceText != batch.sources[3].Text || issues[2].Index != 2 || issues[2].Reason != "empty_quote" || issues[2].SourceText != batch.sources[8].Text || issues[3].Index != 4 || issues[3].Reason != "quote_mismatch" || issues[3].SourceText != "" {
		t.Fatalf("citation problems were hidden or widened source visibility: %+v", issues)
	}
	if response.Repair.Instruction == "" || batch.complete || batch.outcome.Changed {
		t.Fatalf("refusal must remain repairable without saving: %+v", response)
	}
	assertReviewRepairNotLogged(t, batch, "source_text", "invalid_evidence", "owner=verified")
}

func TestReviewRepairDoesNotReadmitCallsRefusalsOrClippedText(t *testing.T) {
	const hidden = "private omitted tail 2089"
	events := []store.Event{
		{Seq: 1, Kind: KindUser, Text: "Keep concise reports."},
		{Seq: 2, Kind: swarm.NotifyToolCall.String(), AgentID: "manager", ToolCallID: "read", Text: "read_file({})"},
		{Seq: 3, Kind: swarm.NotifyToolResult.String(), AgentID: "manager", ToolCallID: "read", Text: strings.Repeat("visible output ", 300) + hidden},
		{Seq: 4, Kind: swarm.NotifyToolCall.String(), AgentID: "manager", ToolCallID: "write", Text: `memory({"action":"add"})`},
		{Seq: 5, Kind: swarm.NotifyToolResult.String(), AgentID: "manager", ToolCallID: "write", Text: `{"success":true}`},
		{Seq: 6, Kind: swarm.NotifyToolCall.String(), AgentID: "manager", ToolCallID: "refused", Text: "exec({})"},
		{Seq: 7, Kind: swarm.NotifyToolResult.String(), AgentID: "manager", ToolCallID: "refused", Text: `{"success":false}`},
		{Seq: 8, Kind: swarm.NotifyAgentMessage.String(), Text: "An unsupported assistant claim."},
	}
	input := selectReviewInput(events, "", "")
	batch := testReviewBatch(t, reviewEvidenceSources(events, input.Events))
	response := reviewRepairResponse(t, batch, []reviewOperation{{Target: memory.ToolMemory, Action: "add", Content: "Keep concise reports.", Evidence: []reviewCitation{
		{Seq: 2, Quote: events[1].Text}, {Seq: 3, Quote: hidden}, {Seq: 5, Quote: events[4].Text}, {Seq: 7, Quote: events[6].Text}, {Seq: 8, Quote: events[7].Text},
	}}})
	if !reflect.DeepEqual(response.Repair.CitableSources, []int64{1, 3}) || len(response.Repair.InvalidEvidence) != 5 || response.Repair.InvalidEvidence[1].SourceText != input.Events[3].Text {
		t.Fatalf("repair sources differ from actual selection/admission: %+v", response)
	}
	raw, err := json.Marshal(response)
	if err != nil || strings.Contains(string(raw), hidden) {
		t.Fatalf("repair recovered unshown source text: %s err=%v", raw, err)
	}
}

func TestReviewRepairIdentifiesLaterOperationWithoutSavingEarlierNotes(t *testing.T) {
	batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: "Keep compact reports."}})
	first := reviewOperation{Target: memory.ToolMemory, Action: "add", Content: "Keep compact reports.", Evidence: []reviewCitation{{Seq: 1, Quote: "compact reports"}}}
	second := first
	second.Evidence = nil
	response := reviewRepairResponse(t, batch, []reviewOperation{first, second})
	if response.Index != 1 || response.Code != "evidence" {
		t.Fatalf("repair pointed at a valid earlier operation: %+v", response)
	}
	assertReviewBatchNotes(t, batch)
}

func TestReviewRepairMissingEvidenceAndFailedTurnKeepSourceBoundary(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{true: "missing", false: "human-only"}[missing], func(t *testing.T) {
			batch := testReviewBatch(t, map[int64]store.Event{
				5: {Seq: 5, Kind: KindUser, Text: "Keep compact reports."},
				9: {Seq: 9, Kind: swarm.NotifyToolResult.String(), Text: "private tool result"},
			})
			batch.humanOnly = true
			op := reviewOperation{Target: memory.ToolMemory, Action: "add", Content: "Keep compact reports."}
			if !missing {
				op.Evidence = []reviewCitation{{Seq: 9, Quote: "private tool result"}}
			}
			response := reviewRepairResponse(t, batch, []reviewOperation{op})
			if !reflect.DeepEqual(response.Repair.CitableSources, []int64{5}) || len(response.Repair.InvalidEvidence) != 1 {
				t.Fatalf("failed-turn repair suggested non-human evidence: %+v", response)
			}
			issue := response.Repair.InvalidEvidence[0]
			if missing && (issue.Reason != "missing_evidence" || issue.Index != -1) || !missing && (issue.Reason != "human_only" || issue.SourceText != "") {
				t.Fatalf("unusable citation repair: %+v", issue)
			}
			assertReviewRepairNotLogged(t, batch, "private tool result", "repair")
		})
	}
}

// Report all missing tokens together, but never use unquoted text, unrelated
// old notes, source IDs or numeric reformatting to silently accept a claim.
func TestReviewRepairReportsAllNumbersFromOnlyCitedQuotesAndReplacedNote(t *testing.T) {
	batch := testReviewBatch(t, map[int64]store.Event{
		81: {Seq: 81, Kind: KindUser, Text: "Use parser 3.7. Observed limit 2037 and ratio 24.0%."},
	})
	if _, err := batch.store.Overwrite("Keep output cap 11MiB.\n\nAn unrelated count is 81."); err != nil {
		t.Fatal(err)
	}
	op := reviewOperation{Target: memory.ToolMemory, Action: "replace", OldText: "Keep output cap", Content: "Parser 3.7; cap 11MiB; limit 2037; count 81; ratio 24.0%; count 81.", Evidence: []reviewCitation{{Seq: 81, Quote: "Use parser 3.7."}}}
	response := reviewRepairResponse(t, batch, []reviewOperation{op})
	if response.Code != "numeric" || !reflect.DeepEqual(response.Repair.UnsupportedNumbers, []string{"2037", "81", "24.0%"}) || !reflect.DeepEqual(response.Repair.SupportedNumbers, []string{"3.7", "11MiB"}) {
		t.Fatalf("numeric repair lost details or accepted unquoted/unrelated evidence: %+v", response)
	}
	assertReviewBatchNotes(t, batch, "Keep output cap 11MiB.", "An unrelated count is 81.")
	assertReviewRepairNotLogged(t, batch, "unsupported_numbers", "supported_numbers", "2037", "11MiB")
}

func TestReviewRepairKeepsNumericSpellingAndRequiresQuotedSupport(t *testing.T) {
	for _, test := range []struct{ content, quote string }{
		{"Observed 1,234 items.", "Observed 1234 items."},
		{"Use version 9.2.", "Use version 9.1."},
		{"Retry after 60s.", "Retry after 1min."},
	} {
		t.Run(test.content, func(t *testing.T) {
			batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: test.quote}})
			response := reviewRepairResponse(t, batch, []reviewOperation{{Target: memory.ToolMemory, Action: "add", Content: test.content, Evidence: []reviewCitation{{Seq: 1, Quote: test.quote}}}})
			if response.Code != "numeric" || len(response.Repair.UnsupportedNumbers) == 0 {
				t.Fatalf("numeric repair weakened spelling validation: %+v", response)
			}
			assertReviewBatchNotes(t, batch)
		})
	}
}

type reviewRepairResult struct {
	Index  int    `json:"index"`
	Code   string `json:"code"`
	Repair struct {
		Instruction     string  `json:"instruction"`
		CitableSources  []int64 `json:"citable_sources"`
		InvalidEvidence []struct {
			Index      int    `json:"index"`
			Seq        int64  `json:"seq"`
			Reason     string `json:"reason"`
			SourceText string `json:"source_text"`
		} `json:"invalid_evidence"`
		UnsupportedNumbers []string `json:"unsupported_numbers"`
		SupportedNumbers   []string `json:"supported_numbers"`
	} `json:"repair"`
}

func reviewRepairResponse(t *testing.T, batch *reviewBatchTool, operations []reviewOperation) reviewRepairResult {
	t.Helper()
	args, err := json.Marshal(map[string]any{"changes": operations})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := batch.InvokableRun(context.Background(), string(args))
	if err != nil {
		t.Fatal(err)
	}
	var response reviewRepairResult
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func assertReviewRepairNotLogged(t *testing.T, batch *reviewBatchTool, forbidden ...string) {
	t.Helper()
	raw, err := json.Marshal(batch.outcome)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range forbidden {
		if strings.Contains(string(raw), value) {
			t.Fatalf("repair-only data reached durable diagnostics: %s", raw)
		}
	}
}
