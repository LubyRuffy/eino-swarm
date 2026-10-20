package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// The real adapter must carry enough of the refused draft back to the next
// model request to repair capacity without discarding unrelated notes.
func TestReviewCapacityRepairProtocolPreservesFeedbackAndCurrentUsage(t *testing.T) {
	const old = "Use comprehensive verbose project reports with the detailed context repeated."
	const unrelated = "Keep branch ownership."
	const revised = "Use concise project reports with citations."
	var citation reviewCitation
	e, fixture := newReviewProtocolEngine(t, func(request int) (string, []reviewOperation) {
		content := revised
		if request == 1 {
			content = old + " " + revised
		}
		return "", []reviewOperation{{Target: memory.ToolMemory, Action: "replace", OldText: old, Content: content, Evidence: []reviewCitation{citation}}}
	})
	th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: revised}})
	pc.memory = memory.NewLimited(pc.memory.Dir(), 110, 0)
	initial, err := pc.memory.Overwrite(old + "\n\n" + unrelated)
	if err != nil {
		t.Fatal(err)
	}
	citation = reviewProtocolCitation(t, e, turn.ID, revised)
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "Finished.")
	settleReviewProtocol(t, e)
	out := decodeReview(t, waitForReview(t, e, turn.ID))
	requests := fixture.snapshot()
	if len(requests) != 2 || !out.Changed || out.Err != "" {
		t.Fatalf("capacity repair outcome=%+v requests=%d", out, len(requests))
	}
	input := reviewProtocolUserInput(t, requests[0])
	if !strings.Contains(input, fmt.Sprintf("%d/%d characters", initial.Chars, initial.Limit)) {
		t.Fatalf("initial review cannot see current capacity: %s", input)
	}
	var repair struct {
		Messages []struct{ Role, Content string }
	}
	if err := json.Unmarshal(requests[1], &repair); err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		Usage          string   `json:"usage"`
		OverBy         int      `json:"over_by"`
		CurrentEntries []string `json:"current_entries"`
		Matched        string   `json:"matched"`
	}
	for _, message := range repair.Messages {
		if message.Role == "tool" {
			if err := json.Unmarshal([]byte(message.Content), &receipt); err != nil {
				t.Fatal(err)
			}
		}
	}
	if receipt.Usage != fmt.Sprintf("%d/%d", initial.Chars, initial.Limit) || receipt.OverBy <= 0 || receipt.Matched != old || !reflect.DeepEqual(receipt.CurrentEntries, initial.Entries) {
		t.Fatalf("repair lost the memory tool's feedback: %+v", receipt)
	}
	snap, err := pc.memory.Read()
	if err != nil || !reflect.DeepEqual(snap.Entries, []string{revised, unrelated}) {
		t.Fatalf("repair changed unrelated notes: %+v err=%v", snap, err)
	}
	metadata := reviewDiagnosticsJSON(t, out)
	if metadata["stop_reason"] != "completed" || metadata["attempt_count"] != float64(2) {
		t.Fatalf("missing completion diagnosis: %v", metadata)
	}
	refused := metadata["refused"].([]any)[0].(map[string]any)
	if refused["code"] != "capacity" || refused["attempt"] != float64(1) || refused["usage"] != receipt.Usage || refused["over_by"] != float64(receipt.OverBy) {
		t.Fatalf("capacity metadata=%v", refused)
	}
}

func TestReviewFailedProposalReportsActualIterationLimit(t *testing.T) {
	const requirement = "Keep reports concise."
	var citation reviewCitation
	e, fixture := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) {
		return "", []reviewOperation{{Target: memory.ToolMemory, Action: "add", Content: requirement + " Effective 2037.", Evidence: []reviewCitation{citation}}}
	})
	e.cfg.Memory.ReviewMaxIterations = 2
	th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: requirement}})
	citation = reviewProtocolCitation(t, e, turn.ID, requirement)
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "Finished.")
	settleReviewProtocol(t, e)
	out := decodeReview(t, waitForReview(t, e, turn.ID))
	metadata := reviewDiagnosticsJSON(t, out)
	if out.Err == "" || out.Changed || len(fixture.snapshot()) != 2 || metadata["stop_reason"] != "iteration_limit" || metadata["attempt_count"] != float64(2) {
		t.Fatalf("iteration exhaustion hidden: %+v metadata=%v", out, metadata)
	}
	assertReviewBatchNotes(t, &reviewBatchTool{store: pc.memory})
}

func TestReviewProposalMissingAndEmptyCommitHaveDifferentOutcomes(t *testing.T) {
	for _, proseOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(proseOnly), func(t *testing.T) {
			const secret = "private-proposal-text-must-not-be-logged"
			e, fixture := newReviewProtocolEngine(t, func(int) (string, []reviewOperation) {
				if proseOnly {
					return secret, nil
				}
				return secret, []reviewOperation{}
			})
			th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: "No durable change to retain."}})
			e.scheduleReview(th.ID, turn, store.TurnDone, pc, "Finished.")
			settleReviewProtocol(t, e)
			event := waitForReview(t, e, turn.ID)
			out := decodeReview(t, event)
			wantStop, attempts := "completed", float64(1)
			if proseOnly {
				wantStop, attempts = "proposal_missing", 0
			}
			metadata := reviewDiagnosticsJSON(t, out)
			if metadata["stop_reason"] != wantStop || metadata["attempt_count"] != attempts || (out.Err != "") != proseOnly || strings.Contains(event.Text, secret) || len(fixture.snapshot()) != 1 {
				t.Fatalf("missing and empty proposals confused: %+v", out)
			}
		})
	}
}

func TestReviewModelErrorIsNotMisreportedAsAnIterationLimit(t *testing.T) {
	e, requests := newAPIErrorEngine(t, http.StatusUnauthorized, `{"error":{"message":"review fixture denied","type":"authentication_error"}}`)
	th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: "Keep project reports concise."}})
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "Finished.")
	settleReviewProtocol(t, e)
	out := decodeReview(t, waitForReview(t, e, turn.ID))
	if out.StopReason != "error" || out.AttemptCount != 0 || len(out.Attempts) != 0 || out.Err == "" || requests.Load() != 1 || out.Changed {
		t.Fatalf("model failure confused with capacity: %+v requests=%d", out, requests.Load())
	}
}

func TestReviewAttemptsAndSourceReferencesStayBoundedAndTyped(t *testing.T) {
	batch := testReviewBatch(t, nil)
	var refs []reviewCitation
	for seq := int64(-1); seq < reviewDiagnosticSources+10; seq++ {
		refs = append(refs, reviewCitation{Seq: seq, Quote: "private-quote"}, reviewCitation{Seq: seq, Quote: "private-quote"})
	}
	operations := make([]reviewOperation, reviewDiagnosticOperations+10)
	for i := range operations {
		operations[i] = reviewOperation{Target: "private-target", Action: "private-action", Evidence: refs}
	}
	for i := 0; i < reviewDiagnosticAttempts+10; i++ {
		invokeTestReviewBatch(t, batch, operations, false)
	}
	if batch.outcome.AttemptCount != reviewDiagnosticAttempts+10 || len(batch.outcome.Attempts) != reviewDiagnosticAttempts || len(batch.outcome.Refused) != reviewDiagnosticAttempts {
		t.Fatalf("unbounded attempts: %+v", batch.outcome)
	}
	for _, attempt := range batch.outcome.Attempts {
		if attempt.Status != "refused" || len(attempt.Operations) != reviewDiagnosticOperations {
			t.Fatalf("operation metadata=%+v", attempt)
		}
		for _, operation := range attempt.Operations {
			if operation.Target != "unknown" || operation.Action != "unknown" || len(operation.Sources) != reviewDiagnosticSources || operation.Sources[0] != 1 {
				t.Fatalf("untrusted or unbounded operation=%+v", operation)
			}
		}
	}
}

func TestReviewEvidenceRefusalMetadataDoesNotRetainQuotes(t *testing.T) {
	const secret = "private-source-quote"
	batch := testReviewBatch(t, nil)
	invokeTestReviewBatch(t, batch, []reviewOperation{{Target: memory.ToolMemory, Action: "add", Content: secret, Evidence: []reviewCitation{{Seq: 42, Quote: secret}}}}, false)
	refused := batch.outcome.Refused[0]
	if refused.Code != "evidence" || refused.Attempt != 1 || !reflect.DeepEqual(refused.Sources, []int64{42}) || batch.outcome.Attempts[0].Status != "refused" {
		t.Fatalf("evidence diagnosis=%+v", batch.outcome)
	}
	raw, _ := json.Marshal(batch.outcome)
	if strings.Contains(string(raw), secret) {
		t.Fatalf("quote logged: %s", raw)
	}
}

func TestReviewRefusalMetadataUsesOnlySafeUsageAndControlledReasons(t *testing.T) {
	for _, code := range []string{"evidence", "numeric", "capacity", "validation", "conflict", "storage"} {
		if reason := reviewRefusalReason(code); reason == "" || reason == code {
			t.Fatalf("missing human readable reason for %s: %q", code, reason)
		}
	}
	for _, feedback := range []string{
		`{"usage":"private-note","over_by":-3,"current_entries":["private-note"]}`,
		`{"usage":"10/20","entry_max":5,"chars":30}`,
		`{"matching":["private-note"],"existing":"private-note","existing_description":"private-note"}`,
	} {
		batch := testReviewBatch(t, nil)
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(feedback), &fields); err != nil {
			t.Fatal(err)
		}
		err := &reviewOperationError{message: "private-note", feedback: fields}
		number := batch.beginAttempt()
		result := batch.refuse(number, -1, "storage", err, nil)
		if !strings.Contains(result, "private-note") {
			t.Fatalf("repair feedback was unexpectedly discarded: %s", result)
		}
		raw, _ := json.Marshal(batch.outcome)
		if strings.Contains(string(raw), "private-note") || batch.outcome.Refused[0].OverBy < 0 {
			t.Fatalf("private receipt leaked: %s", raw)
		}
	}
}

func TestReviewTerminalFailurePreservesCategoryWithoutStoragePath(t *testing.T) {
	for _, cause := range []error{memory.ErrConflict, errors.New("private-storage-path")} {
		batch := testReviewBatch(t, nil)
		number := batch.beginAttempt()
		err := batch.terminalError(number, cause, nil)
		if !errors.Is(err, cause) || strings.Contains(err.Error(), "private-storage-path") || batch.outcome.Attempts[0].Status != "error" {
			t.Fatalf("terminal cause lost or leaked: err=%v outcome=%+v", err, batch.outcome)
		}
	}
}

func TestReviewDiagnosticsExcludeUntrustedProposalText(t *testing.T) {
	const secret = "private-marker-88473920"
	batch := testReviewBatch(t, map[int64]store.Event{1: {Seq: 1, Kind: KindUser, Text: "Use concise reports."}})
	for _, op := range []reviewOperation{
		{Target: memory.ToolMemory, Action: "replace", OldText: secret, Content: "Use concise reports.", Evidence: []reviewCitation{{Seq: 1, Quote: "concise reports"}}},
		{Target: memory.ToolMemory, Action: "add", Content: secret, Evidence: []reviewCitation{{Seq: 1, Quote: "concise reports"}}},
		{Target: secret, Action: secret, Name: secret, Content: secret, Evidence: []reviewCitation{{Seq: 999, Quote: secret}}},
	} {
		invokeTestReviewBatch(t, batch, []reviewOperation{op}, false)
	}
	raw, err := json.Marshal(batch.outcome)
	if err != nil || strings.Contains(string(raw), secret) {
		t.Fatalf("diagnostic leaked rejected text: %s err=%v", raw, err)
	}
	metadata := reviewDiagnosticsJSON(t, *batch.outcome)
	if metadata["attempt_count"] != float64(3) {
		t.Fatalf("missing attempts: %s", raw)
	}
}

func TestReviewReceiptFeedbackRetainsStructuredCapacity(t *testing.T) {
	const receipt = `{"success":false,"error":"limit exceeded","usage":"30/30","over_by":12,"current_entries":["private note"],"matched":"private note"}`
	err := invokeReviewOperation(context.Background(), &reviewReceiptFixture{receipt: receipt}, reviewOperation{})
	if err == nil {
		t.Fatal("capacity refusal was accepted")
	}
	// Error text alone is insufficient; the typed refusal must preserve the
	// original receipt so the batch can supply it to the model without logging it.
	var refused *reviewOperationError
	if !errors.As(err, &refused) || !strings.Contains(refused.Error(), "limit exceeded") || string(refused.feedback["matched"]) != `"private note"` {
		t.Fatalf("unusable refusal: %v", err)
	}
}

func reviewDiagnosticsJSON(t *testing.T, out reviewOutcome) map[string]any {
	t.Helper()
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
