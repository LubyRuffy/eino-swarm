package engine

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

func TestDreamRevisionDeltaPreservesExactChangedLines(t *testing.T) {
	const header = "---\nname: procedure\ndescription: check result\n---\n\n"
	previous := header + "original condition\n"
	for _, tc := range []struct {
		name, before, after, wantPrevious, wantCurrent string
	}{
		{"same front matter", previous, header + "revised condition\n", "original condition\n", "revised condition\n"},
		{"only metadata changed", previous, strings.ReplaceAll(previous, "check result", "check outcome"), "description: check result\n", "description: check outcome\n"},
		{"disjoint edits keep intervening content", "top\nold\nanchor\nprior\nend\n", "top\nnew\nanchor\nupdated\nend\n", "old\nanchor\nprior\n", "new\nanchor\nupdated\n"},
		{"insertion", "same\nlast", "same\nadded\nlast", "", "added\n"},
		{"deletion with repeated shared line", "same\nsame\n", "same\n", "same\n", ""},
		{"identical documents", previous, previous, "", ""},
		{"preserve carriage returns and spaces", "same\r\nold  \r\nend", "same\r\nnew  \r\nend", "old  \r\n", "new  \r\n"},
		{"final newline only", "last\n", "last", "last\n", "last"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, after := dreamRevisionDelta(tc.before, tc.after)
			if before != tc.wantPrevious || after != tc.wantCurrent {
				t.Fatalf("revision delta = %q -> %q, want %q -> %q", before, after, tc.wantPrevious, tc.wantCurrent)
			}
		})
	}
}

func TestDreamReviewOnlyReusesCompleteMatchingToolPairs(t *testing.T) {
	pair := func(seq int64, command, stdout string) dreamSource {
		call := store.Event{ID: uint(seq), Seq: seq, Kind: "tool_call", ThreadID: "thread", TurnID: "turn", Text: command}
		result := store.Event{ID: uint(seq + 1), Seq: seq + 1, Kind: "tool_result", ThreadID: "thread", TurnID: "turn", Text: stdout}
		return dreamSource{Call: call, Result: result}
	}
	execPair := func(seq int64, stdout string) dreamSource {
		result, err := json.Marshal(map[string]any{"full_command": "read", "workdir": "/fixture", "exit_code": 0, "stdout": stdout})
		if err != nil {
			t.Fatal(err)
		}
		return pair(seq, `exec({"command":"read"})`, string(result))
	}
	before, after := "shared setup\nprevious revision\nshared conclusion", "shared setup\ndifferent current revision\nshared conclusion"
	candidate := &dreamCandidate{
		Change:   memory.Change{Before: &before, After: &after},
		Read:     pair(1, `skill_view({})`, after),
		Original: []dreamSource{execPair(10, "prior observation")},
		Sources:  []dreamSource{execPair(3, "later receipt A"), execPair(5, "later receipt B")},
	}
	visible := map[int64]store.Event{}
	for _, pair := range append([]dreamSource{candidate.Read}, candidate.Sources...) {
		visible[pair.Call.Seq], visible[pair.Result.Seq] = pair.Call, pair.Result
	}
	prompt, digest, ids := dreamInput(candidate, visible)
	if !strings.HasPrefix(prompt, dreamAuditHeader) || !strings.Contains(prompt, "prior observation") ||
		strings.Contains(prompt, "later receipt A") || strings.Contains(prompt, "shared setup") || strings.Contains(prompt, "shared conclusion") ||
		!strings.Contains(prompt, `"previous_excerpt":"previous revision\n","current_excerpt":"different current revision\n"`) || len(digest) != 64 ||
		strings.Join(ids, ",") != "4,6" || !strings.Contains(prompt, `"later":[[3,4,"1"],[5,6,"1"]]`) {
		t.Fatalf("the audit must cite exact visible receipts without duplicating them: %s, %s, %v", prompt, digest, ids)
	}
	for _, tc := range []struct {
		name string
		seq  int64
		edit func(*store.Event)
	}{
		{"clipped result", 4, func(ev *store.Event) { ev.Text = ev.Text[:len(ev.Text)-1] }},
		{"foreign thread", 5, func(ev *store.Event) { ev.ThreadID = "other" }},
		{"reused sequence", 6, func(ev *store.Event) { ev.ID++ }},
		{"missing skill read", 2, func(ev *store.Event) { *ev = store.Event{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := visible[tc.seq]
			edited := original
			tc.edit(&edited)
			visible[tc.seq] = edited
			t.Cleanup(func() { visible[tc.seq] = original })
			if prompt, digest, ids := dreamInput(candidate, visible); prompt != "" || digest != "" || ids != nil {
				t.Fatal("a nonidentical receipt cannot authorize an audit")
			}
		})
	}
}

func TestDreamReviewAdvertisesACompactTypedDecision(t *testing.T) {
	batch := testReviewBatch(t, nil)
	batch.dream = &dreamReview{ids: []string{"4", "6", "9", "11"}}
	info, err := batch.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	spec, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil || spec == nil || spec.Properties == nil {
		t.Fatalf("review tool schema: %+v %v", spec, err)
	}
	dream, ok := spec.Properties.Get("dream")
	if !ok || dream.Type != "object" || dream.Properties == nil ||
		strings.Join(spec.Required, ",") != "changes,dream" || strings.Join(dream.Required, ",") != "decision" {
		t.Fatalf("The audited schema must require Dream and its decision: %+v", dream)
	}
	decision, hasDecision := dream.Properties.Get("decision")
	quote, hasQuote := dream.Properties.Get("quote")
	sources, hasSources := dream.Properties.Get("sources")
	wantChoices := []string{"keep", "uncertain", "defer", "revert_using_4_and_6", "revert_using_4_and_9", "revert_using_4_and_11", "revert_using_6_and_9", "revert_using_6_and_11", "revert_using_9_and_11"}
	if !hasDecision || decision.Type != "string" || !slices.EqualFunc(decision.Enum, wantChoices, func(got any, want string) bool { return got == want }) ||
		!hasQuote || quote.Type != "string" || !hasSources || sources.Type != "array" ||
		sources.Items == nil || sources.Items.Type != "string" || len(sources.Items.Enum) != 4 ||
		sources.Items.Enum[0] != "4" || sources.Items.Enum[1] != "6" || sources.Items.Enum[2] != "9" || sources.Items.Enum[3] != "11" {
		t.Fatalf("unexpected Dream decision field schema: %+v", dream)
	}
}

func TestDreamReviewRefusesAnAmbiguousDecisionWithoutRetryingTheBatch(t *testing.T) {
	before, after := "old rule", "unsupported new rule"
	for _, tc := range []struct {
		name, args, want string
		offered          bool
	}{
		{"duplicate", `{"changes":[],"dream":{"decision":"keep"},"dream":{"decision":"revert_using_1_and_2","quote":"unsupported new rule"}}`, "invalid", true},
		{"unoffered", `{"changes":[],"dream":{"decision":"revert_using_1_and_2","quote":"unsupported new rule"}}`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			batch := testReviewBatch(t, nil)
			if tc.offered {
				batch.dream = &dreamReview{candidate: &dreamCandidate{Change: memory.Change{Before: &before, After: &after}}, ids: []string{"1", "2"}}
				batch.dream.offered.Store(true)
			}
			_, err := batch.InvokableRun(t.Context(), tc.args)
			if err != nil || batch.complete != tc.offered || batch.dreamStatus != tc.want || batch.outcome.Changed {
				t.Fatalf("accepted=%t, dream=%q, changed=%t, err=%v", batch.complete, batch.dreamStatus, batch.outcome.Changed, err)
			}
		})
	}
}

func TestDreamReviewRefusedBatchConsumesTheOffer(t *testing.T) {
	before, after := "old rule", "unsupported new rule"
	batch := testReviewBatch(t, nil)
	batch.dream = &dreamReview{
		candidate: &dreamCandidate{Change: memory.Change{Before: &before, After: &after}},
		ids:       []string{"1", "2"},
	}
	batch.dream.offered.Store(true)
	const verdict = `"dream":{"decision":"revert_using_1_and_2","quote":"unsupported new rule"}`
	if _, err := batch.InvokableRun(t.Context(), `{"changes":null,`+verdict+`}`); err != nil || batch.complete {
		t.Fatalf("malformed batch was accepted: complete=%t err=%v", batch.complete, err)
	}
	if _, err := batch.InvokableRun(t.Context(), `{"changes":[],`+verdict+`}`); err != nil || !batch.complete ||
		batch.dreamStatus != "not_submitted" || len(batch.dreamRefs) != 0 || batch.outcome.Changed {
		t.Fatalf("a second batch reused the refused offer: complete=%t status=%q refs=%v changed=%t err=%v",
			batch.complete, batch.dreamStatus, batch.dreamRefs, batch.outcome.Changed, err)
	}
}

func TestDreamDeferContinuesWithoutAnyFirstBatchWrite(t *testing.T) {
	const note = "Use concise status labels."
	const change = `{"target":"memory","action":"add","content":"Use concise status labels.","evidence":[{"seq":1,"quote":"Use concise status labels."}]}`
	for _, tc := range []struct {
		name, changes, status string
		success               bool
	}{
		{"empty changes continue", "[]", "deferred", true},
		{"changes refuse the whole batch", "[" + change + "]", "refused", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, after := "prior rule", "different rule"
			batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: note}})
			batch.dream = &dreamReview{
				candidate: &dreamCandidate{Change: memory.Change{Before: &before, After: &after}},
				ids:       []string{"4", "6"},
			}
			batch.dream.offered.Store(true)
			finishes := 0
			batch.finish = func(context.Context) error { finishes++; return nil }
			reply, err := batch.InvokableRun(t.Context(), `{"changes":`+tc.changes+`,"dream":{"decision":"defer"}}`)
			var receipt struct{ Success bool }
			decodeErr := json.Unmarshal([]byte(reply), &receipt)
			if err != nil || decodeErr != nil || receipt.Success != tc.success || batch.complete ||
				batch.outcome.Changed || finishes != 0 || batch.dream.offered.Load() || batch.dreamStatus != "" ||
				len(batch.outcome.Attempts) != 1 || batch.outcome.Attempts[0].Status != tc.status {
				t.Fatalf("defer did not leave a clean ordinary continuation: %s, outcome=%+v, err=%v", reply, batch.outcome, err)
			}
			assertReviewBatchNotes(t, batch)

			reply, err = batch.InvokableRun(t.Context(), `{"changes":[`+change+`],"dream":{"decision":"defer"}}`)
			if err != nil || !batch.complete || finishes != 1 || batch.dreamStatus != "not_submitted" {
				t.Fatalf("an ordinary batch could not finish after the consumed offer: %s, outcome=%+v, err=%v", reply, batch.outcome, err)
			}
			assertReviewBatchNotes(t, batch, note)
		})
	}
}

func TestDreamReviewRecordsExactSelectedLaterSources(t *testing.T) {
	before, after := "old rule", "unsupported new rule"
	offered := []dreamEventRef{{Seq: 12}, {Seq: 15}, {Seq: 18}}
	dream := &dreamReview{
		candidate: &dreamCandidate{Change: memory.Change{Before: &before, After: &after}},
		ids:       []string{"1", "2", "3"},
		receipt:   dreamReceipt{Sources: offered},
	}
	dream.offered.Store(true)
	status, selected, reason := dream.decision(json.RawMessage(`{"decision":"revert_using_1_and_3","quote":"unsupported new rule"}`))
	if status != "revert" || reason != "" || len(selected) != 2 || selected[0] != offered[0] || selected[1] != offered[2] {
		t.Fatalf("status=%q, selected=%+v", status, selected)
	}
}

func TestDreamUndoesRestorationWhenItsDurableReceiptFails(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Memory.AutoDream = true
	project, thread := projectThread(t, e)
	turn := dreamDiscoveryTurn(t, e, project.ID, thread.ID)
	dreamDiscoveryFinish(t, e, turn)
	mem := e.ProjectMemory(project.ID)
	const name = "verify-transfer"
	write := func(body string) string {
		t.Helper()
		if _, err := mem.WriteSkill(name, "How to verify a transfer", body); err != nil {
			t.Fatal(err)
		}
		_, raw, err := mem.ReadSkillRevision(name)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before, after := write("old rule"), write("new rule")
	patch := store.Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: KindMemoryReview, AgentID: ReviewAgentID, Text: `{"changed":true}`}
	if err := e.store.AppendEvent(&patch); err != nil {
		t.Fatal(err)
	}
	call, result := dreamDiscoveryPair(t, e, turn, "view", "skill_view", `{"name":"verify-transfer"}`, "new rule")
	candidate := &dreamCandidate{Patch: patch, Change: memory.Change{Name: name, Before: &before, After: &after}, Read: dreamSource{Call: call, Result: result}}
	faulted := false
	if err := e.store.DB().Callback().Create().Before("gorm:create").Register("reject_dream_receipt", func(tx *gorm.DB) {
		ev, ok := tx.Statement.Dest.(*store.Event)
		if ok && ev.Kind == KindMemoryReview && strings.Contains(ev.Text, `"status":"restored"`) {
			faulted = true
			_ = tx.AddError(errors.New("injected verification receipt failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if recorded := e.commitDream(turn, &projectContext{project: project, memory: mem}, candidate, &dreamReceipt{Status: "revert", Skill: name}, &reviewOutcome{}); !recorded || !faulted {
		t.Fatalf("the fault did not reach the attempted restoration receipt: handled=%t faulted=%t", recorded, faulted)
	}
	_, got, err := mem.ReadSkillRevision(name)
	if err != nil || got != after {
		t.Fatalf("restoration remained without a durable receipt: %q, %v", got, err)
	}
}
