package engine

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Repairs must be possible from the actual tool receipts that survive the
// provider adapter. A fixture that changes its reply on request two would
// accept a broken feedback path even though the real reviewer stays stuck.
func TestReviewProtocolRepairsQuotesAndNumbersFromActualToolFeedback(t *testing.T) {
	const privateSource = "private-repair-source-only"
	const source = "Keep limit 64MiB, ratio 37%.\nCopy \"exact\" text. " + privateSource
	const proposed = "Keep limit 91MiB, ratio 83%, limit 91MiB."
	const accepted = "Keep limit 64MiB, ratio 37%, limit 64MiB."
	var citation reviewCitation
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
			return "Cannot repair an invalid request.", nil
		}
		var previous []reviewOperation
		var toolResult string
		for _, message := range request.Messages {
			for _, call := range message.ToolCalls {
				if call.Function.Name != reviewBatchToolName {
					continue
				}
				var batch struct {
					Changes []reviewOperation `json:"changes"`
				}
				if err := json.Unmarshal([]byte(call.Function.Arguments), &batch); err != nil {
					t.Errorf("invalid previous proposal: %v", err)
					return "Cannot repair an invalid proposal.", nil
				}
				previous = batch.Changes
			}
			if message.Role == "tool" {
				toolResult = message.Content
			}
		}
		if toolResult == "" {
			return "", []reviewOperation{{Target: memory.ToolMemory, Action: "add", Content: proposed, Evidence: []reviewCitation{
				{Seq: 999001, Quote: "Unavailable source."},
				{Seq: citation.Seq, Quote: "Keep limit 64MiB; ratio 37%."},
			}}}
		}
		var receipt struct {
			Success bool   `json:"success"`
			Code    string `json:"code"`
			Index   *int   `json:"index"`
			Repair  struct {
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
		if err := json.Unmarshal([]byte(toolResult), &receipt); err != nil || receipt.Success || receipt.Index == nil || *receipt.Index != 0 || receipt.Repair.Instruction == "" || len(previous) != 1 {
			t.Errorf("missing actionable repair receipt: %s; err=%v", toolResult, err)
			return "Cannot repair without actionable feedback.", nil
		}
		switch receipt.Code {
		case "evidence":
			invalid := receipt.Repair.InvalidEvidence
			if len(invalid) != 2 || invalid[0].Index != 0 || invalid[0].Seq != 999001 || invalid[0].Reason != "source_unavailable" || invalid[0].SourceText != "" || invalid[1].Index != 1 || invalid[1].Seq != citation.Seq || invalid[1].Reason != "quote_mismatch" || invalid[1].SourceText != source || !reflect.DeepEqual(receipt.Repair.CitableSources, []int64{citation.Seq}) {
				t.Errorf("evidence repair lost source authority or exact bytes: %s", toolResult)
				return "Cannot identify a valid exact quote.", nil
			}
			previous[0].Evidence = []reviewCitation{{Seq: invalid[1].Seq, Quote: invalid[1].SourceText}}
		case "numeric":
			if !reflect.DeepEqual(receipt.Repair.UnsupportedNumbers, []string{"91MiB", "83%"}) || !reflect.DeepEqual(receipt.Repair.SupportedNumbers, []string{"64MiB", "37%"}) {
				t.Errorf("numeric repair did not retain all distinct unsupported/supported spellings: %s", toolResult)
				return "Cannot identify supported numeric spellings.", nil
			}
			for i, unsupported := range receipt.Repair.UnsupportedNumbers {
				previous[0].Content = strings.ReplaceAll(previous[0].Content, unsupported, receipt.Repair.SupportedNumbers[i])
			}
		default:
			t.Errorf("unexpected repair category: %s", toolResult)
			return "Cannot repair an unexpected refusal.", nil
		}
		return "", previous
	})
	th, turn, pc := qualityReviewTurn(t, e, store.TurnDone, []store.Event{{Kind: KindUser, Text: source}})
	citation = reviewProtocolCitation(t, e, turn.ID, source)
	e.scheduleReview(th.ID, turn, store.TurnDone, pc, "Finished.")
	settleReviewProtocol(t, e)
	out := decodeReview(t, waitForReview(t, e, turn.ID))
	snap, err := pc.memory.Read()
	if err != nil || !reflect.DeepEqual(snap.Entries, []string{accepted}) || out.Err != "" || out.StopReason != "completed" || !out.Changed {
		t.Fatalf("feedback-driven repair did not commit: notes=%+v outcome=%+v err=%v", snap, out, err)
	}
	// This legacy client reads only the first issue, so it still takes three
	// requests. Diagnostics must also retain the first proposal's numeric issue.
	if out.AttemptCount != 3 || len(out.Attempts) != 3 || out.Attempts[0].Status != "refused" || out.Attempts[1].Status != "refused" || out.Attempts[2].Status != "accepted" || len(out.Refused) != 3 || out.Refused[0].Code != "evidence" || out.Refused[1].Code != "numeric" || out.Refused[1].Attempt != 1 || out.Refused[2].Code != "numeric" || out.Refused[2].Attempt != 2 || len(fixture.snapshot()) != 3 {
		t.Fatalf("repair loop did not retain its actual receipts: %+v; model calls=%d", out, len(fixture.snapshot()))
	}
	durable, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{privateSource, "invalid_evidence", "source_text", "unsupported_numbers", "supported_numbers", "91MiB", "83%"} {
		if strings.Contains(string(durable), forbidden) {
			t.Errorf("private repair feedback leaked to durable review outcome: %q", forbidden)
		}
	}
}
