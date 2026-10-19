package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// A batch must mean the entire submitted JSON value. Silently accepting a
// prefix can finish a review while ignoring a second conflicting proposal.
func TestReviewBatchRejectsTrailingProposalWithoutCompleting(t *testing.T) {
	batch := testReviewBatch(t, nil)
	finished := 0
	batch.finish = func(context.Context) error { finished++; return nil }
	raw, err := batch.InvokableRun(context.Background(), `{"changes":[]} {"changes":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	var response struct{ Success bool }
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		t.Fatal(err)
	}
	if response.Success || batch.complete || finished != 0 {
		t.Fatalf("trailing JSON accepted: response=%s complete=%v finish_calls=%d", raw, batch.complete, finished)
	}
}

func TestReviewBatchEmptyCompletionIsIdempotentAndDoesNotCreateMemory(t *testing.T) {
	batch := testReviewBatch(t, nil)
	finished := 0
	batch.finish = func(context.Context) error { finished++; return nil }
	invokeTestReviewBatch(t, batch, nil, true)
	if !batch.complete || batch.outcome.Changed || len(batch.outcome.Evidence) != 0 || finished != 1 {
		t.Fatalf("empty batch outcome=%+v finished=%d complete=%v", batch.outcome, finished, batch.complete)
	}
	if _, err := os.Stat(batch.store.Dir()); !os.IsNotExist(err) {
		t.Fatalf("empty batch created files: %v", err)
	}
	invokeTestReviewBatch(t, batch, []reviewOperation{{Target: "invalid"}}, true)
	if finished != 1 || len(batch.outcome.Refused) != 0 {
		t.Fatalf("completed batch ran again: finish=%d outcome=%+v", finished, batch.outcome)
	}
}

func TestReviewBatchRefusalsCanBeRepairedWithoutPartialNotes(t *testing.T) {
	batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: "Use the new policy."}})
	if _, err := batch.store.Overwrite("old policy"); err != nil {
		t.Fatal(err)
	}
	citation := []reviewCitation{{Seq: 1, Quote: "new policy"}}
	invokeTestReviewBatch(t, batch, []reviewOperation{
		{Target: memory.ToolMemory, Action: "add", Content: "new policy", Evidence: citation},
		{Target: memory.ToolMemory, Action: "remove", OldText: "absent policy", Evidence: citation},
	}, false)
	assertReviewBatchNotes(t, batch, "old policy")
	if batch.outcome.Changed || len(batch.outcome.Changes) != 0 || batch.complete || len(batch.outcome.Refused) != 1 || batch.outcome.Refused[0].Index != 1 {
		t.Fatalf("partial draft was reported as persisted: %+v", batch.outcome)
	}
	invokeTestReviewBatch(t, batch, []reviewOperation{{Target: memory.ToolMemory, Action: "replace", OldText: "old policy", Content: "new policy", Evidence: citation}}, true)
	assertReviewBatchNotes(t, batch, "new policy")
	if !batch.complete || batch.outcome.Notes["replace"] != 1 || len(batch.outcome.Changes) != 1 || len(batch.outcome.Evidence) != 1 {
		t.Fatalf("repaired batch outcome=%+v", batch.outcome)
	}
}

func TestReviewBatchInventedNumericDetailRejectsEntireUncommittedNotes(t *testing.T) {
	batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: "Use the revised format."}})
	quote := []reviewCitation{{Seq: 1, Quote: "revised format"}}
	invokeTestReviewBatch(t, batch, []reviewOperation{
		{Target: memory.ToolMemory, Action: "add", Content: "Use the revised format.", Evidence: quote},
		{Target: memory.ToolMemory, Action: "add", Content: "The format was revised in 2037.", Evidence: quote},
	}, false)
	assertReviewBatchNotes(t, batch)
	if batch.outcome.Changed || batch.complete || len(batch.outcome.Refused) != 1 || batch.outcome.Refused[0].Index != 1 || !strings.Contains(batch.outcome.Refused[0].Reason, "2037") {
		t.Fatalf("invented numeric detail was not identified: %+v", batch.outcome)
	}
}

func TestReviewBatchReplacementMayRetainExistingNumericDetail(t *testing.T) {
	batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: "Use terse replies."}})
	if _, err := batch.store.Overwrite("Use parser 3.7; verbose replies."); err != nil {
		t.Fatal(err)
	}
	invokeTestReviewBatch(t, batch, []reviewOperation{{Target: memory.ToolMemory, Action: "replace", OldText: "verbose replies", Content: "Use parser 3.7; terse replies.", Evidence: []reviewCitation{{Seq: 1, Quote: "terse replies"}}}}, true)
	assertReviewBatchNotes(t, batch, "Use parser 3.7; terse replies.")
}

func TestReviewBatchCommitsNotesAndReportsImmediateSkillsBeforeFinishing(t *testing.T) {
	batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: "Replies should be concise. Keep a recovery procedure that checks the owner then verifies the index."}})
	finished := 0
	batch.finish = func(context.Context) error {
		finished++
		assertReviewBatchNotes(t, batch, "Replies should be concise.")
		if skill, err := batch.store.ReadSkill("index-recovery"); err != nil || skill.Body != "Check the owner, rebuild, then verify the index." {
			t.Fatalf("finish ran before skill persistence: %+v %v", skill, err)
		}
		return nil
	}
	quote := []reviewCitation{{Seq: 1, Quote: "checks the owner then verifies the index"}}
	operations := []reviewOperation{
		{Target: memory.ToolMemory, Action: "add", Content: "Replies should be concise.", Evidence: []reviewCitation{{Seq: 1, Quote: "Replies should be concise."}}},
		{Target: memory.ToolSkillManage, Action: "create", Name: "index-recovery", Description: "Recover an index after checking its owner.", Content: "Check the owner, rebuild, then verify the index.", Evidence: quote},
	}
	invokeTestReviewBatch(t, batch, operations, true)
	if finished != 1 || !batch.complete || !batch.outcome.Changed || batch.outcome.Notes["add"] != 1 || len(batch.outcome.Skills) != 1 || len(batch.outcome.Changes) != 2 || len(batch.outcome.Evidence) != 2 {
		t.Fatalf("completed batch outcome=%+v finished=%d", batch.outcome, finished)
	}
	invokeTestReviewBatch(t, batch, operations, true)
	if finished != 1 || len(batch.outcome.Changes) != 2 || len(batch.outcome.Evidence) != 2 {
		t.Fatalf("duplicate batch repeated changes: %+v finished=%d", batch.outcome, finished)
	}
}

func TestReviewBatchStillReportsPersistedSkillsWhenLaterSkillFails(t *testing.T) {
	batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: "Use concise replies and keep recovery steps."}})
	quote := []reviewCitation{{Seq: 1, Quote: "keep recovery steps"}}
	invokeTestReviewBatch(t, batch, []reviewOperation{
		{Target: memory.ToolMemory, Action: "add", Content: "Use concise replies.", Evidence: []reviewCitation{{Seq: 1, Quote: "concise replies"}}},
		{Target: memory.ToolSkillManage, Action: "create", Name: "index-recovery", Description: "Recover the stalled index.", Content: "Check ownership, recover, and verify.", Evidence: quote},
		{Target: memory.ToolSkillManage, Action: "delete", Name: "missing-skill", Evidence: quote},
	}, false)
	assertReviewBatchNotes(t, batch)
	if _, err := batch.store.ReadSkill("index-recovery"); err != nil {
		t.Fatalf("earlier actual skill change disappeared: %v", err)
	}
	if !batch.outcome.Changed || len(batch.outcome.Skills) != 1 || len(batch.outcome.Changes) != 1 || len(batch.outcome.Notes) != 0 || batch.complete {
		t.Fatalf("immediate skill change reported incorrectly: %+v", batch.outcome)
	}
}

func TestReviewBatchCommitFailurePreservesOriginalNotesAndActualSkillChanges(t *testing.T) {
	batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: "Use concise replies and keep recovery steps."}})
	if _, err := batch.store.Overwrite("original note"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(batch.store.Dir(), memory.MemoryFile+".tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	invokeTestReviewBatch(t, batch, []reviewOperation{
		{Target: memory.ToolMemory, Action: "replace", OldText: "original note", Content: "Use concise replies.", Evidence: []reviewCitation{{Seq: 1, Quote: "concise replies"}}},
		{Target: memory.ToolSkillManage, Action: "create", Name: "index-recovery", Description: "Recover the stalled index.", Content: "Check ownership, recover, and verify.", Evidence: []reviewCitation{{Seq: 1, Quote: "recovery steps"}}},
	}, false)
	assertReviewBatchNotes(t, batch, "original note")
	if _, err := batch.store.ReadSkill("index-recovery"); err != nil {
		t.Fatal(err)
	}
	if batch.complete || len(batch.outcome.Notes) != 0 || len(batch.outcome.Changes) != 1 || len(batch.outcome.Skills) != 1 || len(batch.outcome.Refused) != 1 || batch.outcome.Refused[0].Index != -1 {
		t.Fatalf("commit failure misreported persistence: %+v", batch.outcome)
	}
}

func TestReviewBatchInvalidArgumentsAndEvidenceRemainRecoverable(t *testing.T) {
	for _, raw := range []string{"{", `{}`, `{"changes":null}`, `{"changes":[],"unexpected":true}`, `{"changes":[{"target":"memory","action":"add","extra":true}]}`} {
		batch := testReviewBatch(t, nil)
		result, err := batch.InvokableRun(context.Background(), raw)
		if err != nil || strings.Contains(result, `"success":true`) || batch.complete || len(batch.outcome.Refused) != 1 {
			t.Fatalf("invalid batch raw=%q result=%q err=%v outcome=%+v", raw, result, err, batch.outcome)
		}
	}
	batch := testReviewBatch(t, nil)
	invokeTestReviewBatch(t, batch, []reviewOperation{{Target: memory.ToolMemory, Action: "add", Content: "Uncited policy"}}, false)
	assertReviewBatchNotes(t, batch)
}

func TestReviewBatchDoesNotWriteWhenTheSourceCannotBeRead(t *testing.T) {
	for _, action := range []string{"add", "replace"} {
		t.Run(action, func(t *testing.T) {
			batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: "Use concise replies."}})
			if err := os.MkdirAll(filepath.Join(batch.store.Dir(), memory.MemoryFile), 0700); err != nil {
				t.Fatal(err)
			}
			invokeTestReviewBatch(t, batch, []reviewOperation{{Target: memory.ToolMemory, Action: action, OldText: "old", Content: "Use concise replies.", Evidence: []reviewCitation{{Seq: 1, Quote: "concise replies"}}}}, false)
			if batch.outcome.Changed || batch.complete || len(batch.outcome.Refused) != 1 {
				t.Fatalf("unreadable source outcome=%+v", batch.outcome)
			}
		})
	}
}

func TestReviewBatchEnforcesTargetActionAndFailedTurnBoundaries(t *testing.T) {
	batch := testReviewBatch(t, nil)
	for _, op := range []reviewOperation{{Target: "unknown", Action: "add"}, {Target: memory.ToolMemory, Action: "create"}, {Target: memory.ToolSkillManage, Action: "replace"}} {
		if err := batch.validateOperation(op); err == nil {
			t.Fatalf("invalid target/action accepted: %+v", op)
		}
	}
	for _, action := range []string{"add", "replace", "remove"} {
		if err := batch.validateOperation(reviewOperation{Target: memory.ToolMemory, Action: action}); err != nil {
			t.Fatalf("note action %q rejected: %v", action, err)
		}
	}
	for _, action := range []string{"create", "patch", "merge", "delete"} {
		if err := batch.validateOperation(reviewOperation{Target: memory.ToolSkillManage, Action: action}); err != nil {
			t.Fatalf("skill action %q rejected: %v", action, err)
		}
	}
	batch.library = true
	if err := batch.validateOperation(reviewOperation{Target: memory.ToolMemory, Action: "add"}); err == nil {
		t.Fatal("shared library accepted a project note")
	}
	batch.library, batch.humanOnly = false, true
	if err := batch.validateOperation(reviewOperation{Target: memory.ToolSkillManage, Action: "create"}); err == nil {
		t.Fatal("failed turn accepted a skill change")
	}
}

func TestReviewBatchFinisherErrorDoesNotReplayAlreadyPersistedChanges(t *testing.T) {
	batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: "Use concise replies."}})
	finishErr := errors.New("exit action unavailable")
	finished := 0
	batch.finish = func(context.Context) error { finished++; return finishErr }
	args := encodeReviewOperations(t, []reviewOperation{{Target: memory.ToolMemory, Action: "add", Content: "Use concise replies.", Evidence: []reviewCitation{{Seq: 1, Quote: "concise replies"}}}})
	if _, err := batch.InvokableRun(context.Background(), args); !errors.Is(err, finishErr) {
		t.Fatalf("finish error lost category: %v", err)
	}
	assertReviewBatchNotes(t, batch, "Use concise replies.")
	if !batch.complete || len(batch.outcome.Changes) != 1 || finished != 1 {
		t.Fatalf("committed batch outcome=%+v finish_calls=%d", batch.outcome, finished)
	}
	invokeTestReviewBatch(t, batch, nil, true)
	if finished != 1 || len(batch.outcome.Changes) != 1 {
		t.Fatalf("finisher error replayed changes: %+v", batch.outcome)
	}
}

func TestReviewBatchSchemaRequiresWholeBatchAndEvidenceReferences(t *testing.T) {
	info, err := testReviewBatch(t, nil).Info(context.Background())
	if err != nil || info.Name != reviewBatchToolName {
		t.Fatalf("tool info=%+v err=%v", info, err)
	}
	spec, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil || !reflect.DeepEqual(spec.Required, []string{"changes"}) {
		t.Fatalf("batch schema=%+v err=%v", spec, err)
	}
	changes, ok := spec.Properties.Get("changes")
	if !ok || changes.Items == nil {
		t.Fatal("changes has no operation schema")
	}
	if !containsRequired(changes.Items.Required, "target", "action", "evidence") {
		t.Fatalf("operation requirements=%v", changes.Items.Required)
	}
	citations, ok := changes.Items.Properties.Get("evidence")
	if !ok || citations.Items == nil || !containsRequired(citations.Items.Required, "seq", "quote") {
		t.Fatalf("evidence schema=%+v", citations)
	}
}

func TestReviewNoteDiffIgnoresReorderingAndReportsOnlyNetChanges(t *testing.T) {
	for _, test := range []struct {
		name          string
		before, after []string
		actions, text []string
	}{
		{name: "reorder", before: []string{"a", "b"}, after: []string{"b", "a"}},
		{name: "add", before: []string{"a"}, after: []string{"a", "b", "c"}, actions: []string{"add", "add"}, text: []string{"b", "c"}},
		{name: "remove", before: []string{"a", "b"}, actions: []string{"remove", "remove"}, text: []string{"a", "b"}},
		{name: "replace and add", before: []string{"a", "keep"}, after: []string{"keep", "b", "c"}, actions: []string{"replace", "add"}, text: []string{"b", "c"}},
		{name: "replace and remove", before: []string{"a", "b", "keep"}, after: []string{"keep", "c"}, actions: []string{"replace", "remove"}, text: []string{"c", "b"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			changes := reviewNoteDiff(memory.Snapshot{Entries: test.before}, memory.Snapshot{Entries: test.after})
			if len(changes) != len(test.actions) {
				t.Fatalf("changes=%+v want actions=%v", changes, test.actions)
			}
			for i, change := range changes {
				if change.Target != memory.ToolMemory || change.Action != test.actions[i] || change.Text != test.text[i] {
					t.Fatalf("change[%d]=%+v want action=%q text=%q", i, change, test.actions[i], test.text[i])
				}
			}
		})
	}
	long := strings.Repeat("证", 170)
	changes := reviewNoteDiff(memory.Snapshot{}, memory.Snapshot{Entries: []string{long}})
	if len(changes) != 1 || utf8.RuneCountInString(changes[0].Text) > 162 || !strings.HasPrefix(changes[0].Text, strings.Repeat("证", 160)) {
		t.Fatalf("change preview is unbounded or splits CJK: %+v", changes)
	}
}

func TestInvokeReviewOperationPreservesToolErrorsAndRejectsBadReceipts(t *testing.T) {
	wantErr := errors.New("storage failure")
	for _, test := range []struct {
		name, receipt string
		toolErr       error
	}{
		{name: "tool failure", toolErr: wantErr},
		{name: "malformed receipt", receipt: "not JSON"},
		{name: "refusal", receipt: `{"success":false,"error":"limit exceeded"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := invokeReviewOperation(context.Background(), &reviewReceiptFixture{receipt: test.receipt, err: test.toolErr}, reviewOperation{Target: memory.ToolMemory, Action: "add", Content: "note"})
			if err == nil || test.toolErr != nil && !errors.Is(err, wantErr) {
				t.Fatalf("receipt error=%v", err)
			}
		})
	}
	if err := finishReviewBatch(context.Background()); err == nil {
		t.Fatal("ADK exit unexpectedly succeeded outside an agent state")
	}
}

type reviewReceiptFixture struct {
	receipt string
	err     error
}

func (f *reviewReceiptFixture) Info(context.Context) (*schema.ToolInfo, error) { return nil, nil }
func (f *reviewReceiptFixture) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return f.receipt, f.err
}

func invokeTestReviewBatch(t *testing.T, batch *reviewBatchTool, operations []reviewOperation, wantSuccess bool) {
	t.Helper()
	raw, err := batch.InvokableRun(context.Background(), encodeReviewOperations(t, operations))
	if err != nil {
		t.Fatalf("batch refusal must be recoverable: %v", err)
	}
	var result struct{ Success bool }
	if err := json.Unmarshal([]byte(raw), &result); err != nil || result.Success != wantSuccess {
		t.Fatalf("batch response=%s wantSuccess=%v err=%v", raw, wantSuccess, err)
	}
}

func encodeReviewOperations(t *testing.T, operations []reviewOperation) string {
	t.Helper()
	if operations == nil {
		operations = []reviewOperation{}
	}
	raw, err := json.Marshal(struct {
		Changes []reviewOperation `json:"changes"`
	}{Changes: operations})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func assertReviewBatchNotes(t *testing.T, batch *reviewBatchTool, want ...string) {
	t.Helper()
	snap, err := batch.store.Read()
	if err != nil || !reflect.DeepEqual(snap.Entries, want) {
		t.Fatalf("persisted notes=%q want=%q err=%v", snap.Entries, want, err)
	}
}

func containsRequired(got []string, want ...string) bool {
	for _, value := range want {
		found := false
		for _, item := range got {
			found = found || item == value
		}
		if !found {
			return false
		}
	}
	return true
}

func testReviewBatch(t *testing.T, sources map[int64]store.Event) *reviewBatchTool {
	t.Helper()
	return &reviewBatchTool{
		store:   memory.NewLimited(filepath.Join(t.TempDir(), "memory"), 6000, 300),
		sources: sources, mu: &sync.Mutex{}, outcome: &reviewOutcome{},
	}
}
