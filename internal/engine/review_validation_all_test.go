package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

type reviewAllIssue struct {
	Index           int      `json:"index"`
	Code            string   `json:"code"`
	Chars           int      `json:"chars"`
	EntryMax        int      `json:"entry_max"`
	Usage           string   `json:"usage"`
	OverBy          int      `json:"over_by"`
	ProposedUsage   string   `json:"proposed_usage"`
	AvailableChars  int      `json:"available_chars"`
	MaxContentChars int      `json:"max_content_chars"`
	CurrentEntries  []string `json:"current_entries"`
	Matched         string   `json:"matched"`
	Repair          struct {
		Instruction        string                `json:"instruction"`
		SupportedNumbers   []string              `json:"supported_numbers"`
		UnsupportedNumbers []string              `json:"unsupported_numbers"`
		InvalidEvidence    []reviewEvidenceIssue `json:"invalid_evidence"`
	} `json:"repair"`
}

type reviewAllReceipt struct {
	Success bool             `json:"success"`
	Index   int              `json:"index"`
	Code    string           `json:"code"`
	Issues  []reviewAllIssue `json:"issues"`
}

// Several independent defects in one proposal must not cost one model call
// each, and inspecting a bad proposal must never execute its valid skill write.
func TestReviewReturnsAllIndependentProblemsWithoutSaving(t *testing.T) {
	const source = "Keep the storage limit at 42MiB. Unquoted detail: 73MiB. private-aggregate-source"
	batch := testReviewBatch(t, map[int64]store.Event{
		11: {Seq: 11, Kind: KindUser, Text: source},
		12: {Seq: 12, Kind: KindUser, Text: "Use concise reports and retain recovery steps."},
	})
	const old = "Preserve branch ownership."
	const unrelated = "Keep signing identities private."
	initialText := old + "\n\n" + unrelated
	batch.store = memory.NewLimited(batch.store.Dir(), utf8.RuneCountInString(initialText)+10, 0)
	initial, err := batch.store.Overwrite(initialText)
	if err != nil {
		t.Fatal(err)
	}
	operations := []reviewOperation{
		{Target: memory.ToolMemory, Action: "add", Content: "Keep limits 42MiB, 91MiB and 73MiB; never restart automatically.", Evidence: []reviewCitation{
			{Seq: 11, Quote: "Keep the storage limit at 42MiB."},
			{Seq: 11, Quote: "Keep the storage limit at 91MiB."},
			{Seq: 999001, Quote: "Unavailable source."},
		}},
		{Target: memory.ToolMemory, Action: "add", Content: "Use report cap 7.", Evidence: []reviewCitation{{Seq: 12, Quote: "This quote does not occur."}}},
		{Target: memory.ToolSkillManage, Action: "create", Name: "owner-recovery", Description: "Recover after verifying ownership.", Content: "Inspect ownership, recover, and verify.", Evidence: []reviewCitation{{Seq: 12, Quote: "retain recovery steps"}}},
	}
	raw, err := batch.InvokableRun(context.Background(), encodeReviewOperations(t, operations))
	if err != nil {
		t.Fatal(err)
	}
	var receipt reviewAllReceipt
	if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Success || receipt.Index != 0 || receipt.Code != "evidence" {
		t.Fatalf("first refusal compatibility lost: %s", raw)
	}
	byIndex := map[int]map[string]reviewAllIssue{}
	for _, issue := range receipt.Issues {
		if byIndex[issue.Index] == nil {
			byIndex[issue.Index] = map[string]reviewAllIssue{}
		}
		byIndex[issue.Index][issue.Code] = issue
	}
	for _, index := range []int{0, 1} {
		for _, code := range []string{"evidence", "numeric", "capacity"} {
			if _, ok := byIndex[index][code]; !ok {
				t.Errorf("proposal hid operation %d's %s issue: %s", index, code, raw)
			}
		}
	}
	numeric := byIndex[0]["numeric"].Repair
	if !reflect.DeepEqual(numeric.SupportedNumbers, []string{"42MiB"}) || !reflect.DeepEqual(numeric.UnsupportedNumbers, []string{"91MiB", "73MiB"}) {
		t.Errorf("forged or uncited quotes changed numeric authority: %+v", numeric)
	}
	capacity := byIndex[0]["capacity"]
	if capacity.Usage != fmt.Sprintf("%d/%d", initial.Chars, initial.Limit) || capacity.OverBy <= 0 || !reflect.DeepEqual(capacity.CurrentEntries, initial.Entries) {
		t.Errorf("capacity was hidden behind provenance: %+v", capacity)
	}
	assertReviewBatchNotes(t, batch, old, unrelated)
	skills, err := batch.store.ListSkills()
	if err != nil || len(skills) != 0 || batch.complete || batch.outcome.Changed || batch.outcome.AttemptCount != 1 {
		t.Fatalf("preflight caused side effects: skills=%+v err=%v outcome=%+v", skills, err, batch.outcome)
	}
	for _, index := range []int{0, 1} {
		for _, code := range []string{"evidence", "numeric", "capacity"} {
			found := false
			for _, refusal := range batch.outcome.Refused {
				found = found || refusal.Index == index && refusal.Code == code && refusal.Attempt == 1
			}
			if !found {
				t.Errorf("durable diagnostics hid %d/%s: %+v", index, code, batch.outcome.Refused)
			}
		}
	}
	assertReviewRepairNotLogged(t, batch, "private-aggregate-source", "91MiB", "73MiB", "issues", "current_entries", "supported_numbers", "unsupported_numbers")
}

// Repeating one bad reference throughout a batch must not multiply a long
// source body in the next model input; every operation still needs its issue.
func TestReviewAggregateSharesSourceTextAcrossOperations(t *testing.T) {
	const source = "Keep compact reports. private-shared-aggregate-source"
	batch := testReviewBatch(t, map[int64]store.Event{8: {Seq: 8, Kind: KindUser, Text: source}})
	operations := []reviewOperation{
		{Target: memory.ToolMemory, Action: "add", Content: "Use brief reports.", Evidence: []reviewCitation{{Seq: 8, Quote: "A missing phrase."}}},
		{Target: memory.ToolMemory, Action: "add", Content: "Keep short reports.", Evidence: []reviewCitation{{Seq: 8, Quote: "Another missing phrase."}}},
	}
	raw, err := batch.InvokableRun(context.Background(), encodeReviewOperations(t, operations))
	if err != nil {
		t.Fatal(err)
	}
	var receipt reviewAllReceipt
	if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Success || len(receipt.Issues) != 2 {
		t.Fatalf("shared source lost operation issues: %s", raw)
	}
	for index, issue := range receipt.Issues {
		invalid := issue.Repair.InvalidEvidence
		if issue.Index != index || issue.Code != "evidence" || len(invalid) != 1 || invalid[0].Seq != 8 || invalid[0].Reason != "quote_mismatch" {
			t.Errorf("shared source lost issue identity: %+v", issue)
			continue
		}
		want := ""
		if index == 0 {
			want = source
		}
		if invalid[0].SourceText != want {
			t.Errorf("operation %d repeats or loses shared source: %q want %q", index, invalid[0].SourceText, want)
		}
	}
	var legacy struct {
		Repair struct {
			InvalidEvidence []reviewEvidenceIssue `json:"invalid_evidence"`
		} `json:"repair"`
	}
	if err := json.Unmarshal([]byte(raw), &legacy); err != nil || len(legacy.Repair.InvalidEvidence) != 1 || legacy.Repair.InvalidEvidence[0].SourceText != source {
		t.Fatalf("first refusal compatibility lost source text: %s err=%v", raw, err)
	}
	assertReviewBatchNotes(t, batch)
	assertReviewRepairNotLogged(t, batch, source, "source_text", "invalid_evidence")
}

// A selector matching several old notes is not a license to borrow all of
// their numbers before the model repairs that selector to name one note.
func TestReviewAmbiguousReplacementCannotBorrowAllOldNumbers(t *testing.T) {
	const source = "Keep the existing limits."
	batch := testReviewBatch(t, map[int64]store.Event{4: {Seq: 4, Kind: KindUser, Text: source}})
	const first = "First cap 31MiB."
	const second = "Second cap 73MiB."
	if _, err := batch.store.Overwrite(first + "\n\n" + second); err != nil {
		t.Fatal(err)
	}
	raw, err := batch.InvokableRun(context.Background(), encodeReviewOperations(t, []reviewOperation{{
		Target: memory.ToolMemory, Action: "replace", OldText: "cap", Content: "Keep cap 31MiB and 73MiB.",
		Evidence: []reviewCitation{{Seq: 4, Quote: source}},
	}}))
	if err != nil {
		t.Fatal(err)
	}
	var receipt reviewAllReceipt
	if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
		t.Fatal(err)
	}
	byCode := map[string]reviewAllIssue{}
	for _, issue := range receipt.Issues {
		byCode[issue.Code] = issue
	}
	numeric, numberRefused := byCode["numeric"]
	_, matchRefused := byCode["validation"]
	if receipt.Success || !numberRefused || !matchRefused || len(numeric.Repair.SupportedNumbers) != 0 || !reflect.DeepEqual(numeric.Repair.UnsupportedNumbers, []string{"31MiB", "73MiB"}) {
		t.Fatalf("ambiguous old notes widened numeric authority: %s", raw)
	}
	assertReviewBatchNotes(t, batch, first, second)
}

// A prior operation can move a selector to another note. Numeric preservation
// must follow the entry actually replaced at that step, not its old namesake.
func TestReviewMovedSelectorCannotBorrowNumbersFromAnotherOriginalNote(t *testing.T) {
	const source = "Use Delta for the first record and Alpha for the second. Keep Alpha's approved limit."
	batch := testReviewBatch(t, map[int64]store.Event{4: {Seq: 4, Kind: KindUser, Text: source}})
	const first = "Alpha cap 31MiB."
	const second = "Beta cap 73MiB."
	if _, err := batch.store.Overwrite(first + "\n\n" + second); err != nil {
		t.Fatal(err)
	}
	operations := []reviewOperation{
		{Target: memory.ToolMemory, Action: "replace", OldText: "Alpha", Content: "Delta cap 31MiB.", Evidence: []reviewCitation{{Seq: 4, Quote: "Use Delta for the first record"}}},
		{Target: memory.ToolMemory, Action: "replace", OldText: "Beta", Content: "Alpha cap 73MiB.", Evidence: []reviewCitation{{Seq: 4, Quote: "Alpha for the second"}}},
		{Target: memory.ToolMemory, Action: "replace", OldText: "Alpha", Content: "Alpha cap 31MiB.", Evidence: []reviewCitation{{Seq: 4, Quote: "Keep Alpha's approved limit."}}},
	}
	raw, err := batch.InvokableRun(context.Background(), encodeReviewOperations(t, operations))
	if err != nil {
		t.Fatal(err)
	}
	var receipt reviewAllReceipt
	if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Success || len(receipt.Issues) != 1 || receipt.Issues[0].Index != 2 || receipt.Issues[0].Code != "numeric" || !reflect.DeepEqual(receipt.Issues[0].Repair.UnsupportedNumbers, []string{"31MiB"}) || !reflect.DeepEqual(receipt.Issues[0].Repair.SupportedNumbers, []string{"73MiB"}) {
		t.Fatalf("selector reused an unrelated original note's numbers: %s", raw)
	}
	assertReviewBatchNotes(t, batch, first, second)
}

// A malformed operation must not stop independent checks of later operations,
// and no valid writer may run until every reported problem has been repaired.
func TestReviewAliasConflictAndUnknownActionsStillCheckLaterOperations(t *testing.T) {
	const source = "Use compact summaries and retain recovery steps."
	batch := testReviewBatch(t, map[int64]store.Event{6: {Seq: 6, Kind: KindUser, Text: source}})
	citation := []reviewCitation{{Seq: 6, Quote: source}}
	operations := []reviewOperation{
		{Target: memory.ToolMemory, Action: "add", Content: "First replacement.", NewText: "Conflicting replacement.", Evidence: []reviewCitation{{Seq: 6, Quote: "Also an absent quote."}}},
		{Target: memory.ToolMemory, Action: "add", NewText: "Use compact summaries.", Evidence: []reviewCitation{{Seq: 6, Quote: "Absent quote."}}},
		{Target: "unknown", Action: "add", Content: "Unknown target.", Evidence: citation},
		{Target: memory.ToolMemory, Action: "create", Content: "Unknown note action.", Evidence: citation},
		{Target: memory.ToolSkillManage, Action: "create", Name: "owner-recovery", Description: "Recover after verifying ownership.", Content: "Inspect ownership, recover, and verify.", Evidence: citation},
	}
	raw, err := batch.InvokableRun(context.Background(), encodeReviewOperations(t, operations))
	if err != nil {
		t.Fatal(err)
	}
	var receipt reviewAllReceipt
	if err := json.Unmarshal([]byte(raw), &receipt); err != nil {
		t.Fatal(err)
	}
	got := map[int]map[string]bool{}
	for _, issue := range receipt.Issues {
		if got[issue.Index] == nil {
			got[issue.Index] = map[string]bool{}
		}
		got[issue.Index][issue.Code] = true
	}
	want := map[int]map[string]bool{0: {"validation": true, "evidence": true}, 1: {"evidence": true}, 2: {"validation": true}, 3: {"validation": true}}
	if receipt.Success || receipt.Index != 0 || receipt.Code != "validation" || !reflect.DeepEqual(got, want) {
		t.Fatalf("first malformed operation hid later checks: %s", raw)
	}
	assertReviewBatchNotes(t, batch)
	skills, err := batch.store.ListSkills()
	if err != nil || len(skills) != 0 || batch.complete || batch.outcome.Changed {
		t.Fatalf("invalid proposal executed a valid writer: skills=%+v err=%v outcome=%+v", skills, err, batch.outcome)
	}
}

// The second proposal is chosen from the actual aggregate receipt, not its
// request number. This freezes the facts that a successful compact repair
// must retain without treating the scripted model as a semantic evaluator.
func TestReviewProtocolRepairsAllProblemsAndPreservesRelatedConditions(t *testing.T) {
	const source = "For staging, retry only after approval; never restart automatically; wait 48h. Include checksums."
	const old = "For the staging project, retry only after approval, and never restart automatically; the waiting interval is 48h."
	const revised = "Staging: retry only after approval; never restart automatically; wait 48h; include checksums."
	const unrelated = "Keep the signing identity private."
	var citation reviewCitation
	var initial memory.Snapshot
	e, fixture := newReviewProtocolRequestEngine(t, func(raw json.RawMessage, _ int) (string, []reviewOperation) {
		var request struct {
			Messages []struct {
				Role, Content string
				ToolCalls     []struct {
					Function struct{ Name, Arguments string }
				} `json:"tool_calls"`
			}
		}
		if err := json.Unmarshal(raw, &request); err != nil {
			t.Errorf("invalid model request: %v", err)
			return "Cannot inspect an invalid request.", nil
		}
		var previous []reviewOperation
		var receiptRaw string
		for _, message := range request.Messages {
			for _, call := range message.ToolCalls {
				if call.Function.Name == reviewBatchToolName {
					var batch struct{ Changes []reviewOperation }
					if err := json.Unmarshal([]byte(call.Function.Arguments), &batch); err != nil {
						t.Errorf("invalid previous proposal: %v", err)
						return "Cannot inspect the proposal.", nil
					}
					previous = batch.Changes
				}
			}
			if message.Role == "tool" {
				receiptRaw = message.Content
			}
		}
		if receiptRaw == "" {
			return "", []reviewOperation{{Target: memory.ToolMemory, Action: "replace", OldText: old,
				Content:  old + " Set the requested waiting interval to 96h while keeping every condition. Include checksums.",
				Evidence: []reviewCitation{citation, {Seq: citation.Seq, Quote: "wait 96h."}},
			}}
		}
		var receipt reviewAllReceipt
		if err := json.Unmarshal([]byte(receiptRaw), &receipt); err != nil || receipt.Success || len(previous) != 1 {
			t.Errorf("invalid repair receipt: %s err=%v", receiptRaw, err)
			return "Cannot repair an invalid receipt.", nil
		}
		issues := map[string]reviewAllIssue{}
		for _, issue := range receipt.Issues {
			if issue.Index == 0 {
				issues[issue.Code] = issue
			}
		}
		for _, code := range []string{"evidence", "numeric", "capacity"} {
			if _, ok := issues[code]; !ok {
				t.Errorf("actual adapter omitted %s from aggregate repair: %s", code, receiptRaw)
				return "Cannot repair without all independent problems.", nil
			}
		}
		numeric := issues["numeric"].Repair
		capacity := issues["capacity"]
		if !reflect.DeepEqual(numeric.UnsupportedNumbers, []string{"96h"}) || !reflect.DeepEqual(numeric.SupportedNumbers, []string{"48h"}) || capacity.Usage != fmt.Sprintf("%d/%d", initial.Chars, initial.Limit) || capacity.AvailableChars != 4 || capacity.MaxContentChars < utf8.RuneCountInString(revised) || capacity.ProposedUsage == "" || capacity.OverBy <= 0 || !reflect.DeepEqual(capacity.CurrentEntries, initial.Entries) {
			t.Errorf("repair has no actionable supported capacity plan: %s", receiptRaw)
			return "Cannot identify a safe supported compact replacement.", nil
		}
		previous[0].Content = revised
		previous[0].Evidence = []reviewCitation{citation}
		return "", previous
	})
	e.cfg.Memory.ReviewMaxIterations = 2
	th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: source}})
	initialText := old + "\n\n" + unrelated
	pc.memory = memory.NewLimited(pc.memory.Dir(), utf8.RuneCountInString(initialText)+4, 0)
	var err error
	initial, err = pc.memory.Overwrite(initialText)
	if err != nil {
		t.Fatal(err)
	}
	citation = reviewProtocolCitation(t, e, turn.ID, source)
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "Finished.")
	settleReviewProtocol(t, e)
	out := decodeReview(t, waitForReview(t, e, turn.ID))
	snap, err := pc.memory.Read()
	if err != nil || !reflect.DeepEqual(snap.Entries, []string{revised, unrelated}) || out.Err != "" || out.StopReason != "completed" || !out.Changed {
		t.Fatalf("aggregate repair did not preserve frozen facts: notes=%+v outcome=%+v err=%v", snap, out, err)
	}
	if out.AttemptCount != 2 || len(fixture.snapshot()) != 2 || len(out.Refused) != 3 || len(out.Attempts) != 2 || out.Attempts[0].Status != "refused" || out.Attempts[1].Status != "accepted" {
		t.Fatalf("repairs consumed separate model calls: outcome=%+v calls=%d", out, len(fixture.snapshot()))
	}
	for _, refusal := range out.Refused {
		if refusal.Index != 0 || refusal.Attempt != 1 {
			t.Errorf("aggregate refusal lost its proposal identity: %+v", refusal)
		}
	}
	calls, err := e.store.ListLLMCalls(turn.ID)
	if err != nil || len(calls) != 2 {
		t.Fatalf("actual model call accounting=%+v err=%v", calls, err)
	}
	durable, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"96h", "issues", "invalid_evidence", "source_text", "unsupported_numbers", "current_entries", "proposed_usage", "max_content_chars"} {
		if strings.Contains(string(durable), forbidden) {
			t.Errorf("private repair detail leaked to durable outcome: %q", forbidden)
		}
	}
}

// A per-note refusal must not hide the tighter total-store budget from the
// next real model request; the reviewer needs both limits in the same receipt.
func TestReviewProtocolRepairsCombinedEntryAndTotalLimitsFromOneReceipt(t *testing.T) {
	const old = "For staging reports, use concise wording only after approval, never restart automatically, and retain the 48h wait."
	const revised = "Staging reports: concise only after approval; never auto-restart; retain 48h wait."
	const unrelated = "Keep signing identities private."
	const proposed = old + " " + old
	entryMax := utf8.RuneCountInString(revised) + 3
	var citation reviewCitation
	var initial memory.Snapshot
	e, fixture := newReviewProtocolRequestEngine(t, func(raw json.RawMessage, _ int) (string, []reviewOperation) {
		var request struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.Unmarshal(raw, &request); err != nil {
			t.Errorf("invalid model request: %v", err)
			return "Cannot inspect an invalid request.", nil
		}
		var receiptRaw string
		for _, message := range request.Messages {
			if message.Role == "tool" {
				receiptRaw = message.Content
			}
		}
		if receiptRaw == "" {
			return "", []reviewOperation{{Target: memory.ToolMemory, Action: "replace", OldText: old, Content: proposed, Evidence: []reviewCitation{citation}}}
		}
		var receipt reviewAllReceipt
		if err := json.Unmarshal([]byte(receiptRaw), &receipt); err != nil || receipt.Success || len(receipt.Issues) != 1 {
			t.Errorf("missing combined-limit issue: %s err=%v", receiptRaw, err)
			return "Cannot repair without a combined-limit receipt.", nil
		}
		issue := receipt.Issues[0]
		proposedChars := initial.Chars - utf8.RuneCountInString(old) + utf8.RuneCountInString(proposed)
		if issue.Index != 0 || issue.Code != "capacity" || issue.Chars != utf8.RuneCountInString(proposed) || issue.EntryMax != entryMax || issue.Usage != fmt.Sprintf("%d/%d", initial.Chars, initial.Limit) || issue.ProposedUsage != fmt.Sprintf("%d/%d", proposedChars, initial.Limit) || issue.OverBy != proposedChars-initial.Limit || issue.AvailableChars != 4 || issue.MaxContentChars != entryMax || !reflect.DeepEqual(issue.CurrentEntries, initial.Entries) || issue.Matched != old {
			t.Errorf("adapter dropped a simultaneous entry/total budget field: %+v", issue)
			return "Cannot identify the supported content allowance.", nil
		}
		return "", []reviewOperation{{Target: memory.ToolMemory, Action: "replace", OldText: issue.Matched, Content: revised, Evidence: []reviewCitation{citation}}}
	})
	e.cfg.Memory.ReviewMaxIterations = 2
	th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: old}})
	initialText := old + "\n\n" + unrelated
	pc.memory = memory.NewLimited(pc.memory.Dir(), utf8.RuneCountInString(initialText)+4, entryMax)
	var err error
	initial, err = pc.memory.Overwrite(initialText)
	if err != nil {
		t.Fatal(err)
	}
	citation = reviewProtocolCitation(t, e, turn.ID, old)
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "Finished.")
	settleReviewProtocol(t, e)
	out := decodeReview(t, waitForReview(t, e, turn.ID))
	snap, err := pc.memory.Read()
	if err != nil || !reflect.DeepEqual(snap.Entries, []string{revised, unrelated}) || out.Err != "" || out.StopReason != "completed" || !out.Changed || out.AttemptCount != 2 || len(fixture.snapshot()) != 2 {
		t.Fatalf("combined-limit repair did not complete in two proposals: notes=%+v outcome=%+v calls=%d err=%v", snap, out, len(fixture.snapshot()), err)
	}
	if len(out.Refused) != 1 || out.Refused[0].Code != "capacity" || out.Refused[0].Attempt != 1 || out.Refused[0].Index != 0 || out.Refused[0].OverBy <= 0 {
		t.Fatalf("combined capacity refusal lost safe diagnostics: %+v", out.Refused)
	}
}
