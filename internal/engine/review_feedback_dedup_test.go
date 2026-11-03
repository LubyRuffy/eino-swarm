package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// The OpenSSL archive retained source sequences, refusal categories and the
// accepted versions, but not rejected proposals or the original feedback.
// Keep that observed progression and reconstruct the missing input locally.
func TestReviewFeedbackDedupOpenSSLReplay(t *testing.T) {
	const (
		versions     = "OpenSSL 3.0.17\nPython 3.13.7"
		certificate  = "Certificate verification successful"
		verification = "check_cert.py: exit 0"
		accepted     = "Record the certificate checks with OpenSSL 3.0.17 and Python 3.13.7."
		privateProbe = "synthetic-private-repair-source"
	)
	resultBody := func(stdout string) string {
		b, err := json.Marshal(map[string]any{"stdout": stdout, "exit_code": 0})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	// Preserve the original per-thread sequence positions. Only short public
	// excerpts of successful receipts enter this fixture; other rows are inert.
	events := make([]store.Event, 40)
	for i := range events {
		events[i].Kind = "offline_fixture_gap"
	}
	events[0] = store.Event{Kind: KindUser, Text: "Verify the local certificate."}
	for _, pair := range []struct {
		call, result int
		body         string
	}{
		{7, 9, resultBody(versions)},
		{27, 28, resultBody(certificate)},
		{32, 34, resultBody(verification + "\n" + privateProbe + ": Keep \"exact\" text unless overridden.")},
	} {
		id := fmt.Sprintf("openssl-fixture-%d", pair.call)
		events[pair.call-1] = store.Event{Kind: "tool_call", AgentID: "manager", ToolCallID: id, Text: `exec({"command":"inspect certificate evidence"})`}
		events[pair.result-1] = store.Event{Kind: "tool_result", AgentID: "manager", ToolCallID: id, Text: pair.body}
	}
	events[39] = store.Event{Kind: "agent_message", AgentID: "manager", Text: "I verified the certificate."}

	versionQuote := reviewCitation{Seq: 9, Quote: events[8].Text}
	certificateQuote := reviewCitation{Seq: 28, Quote: certificate}
	verificationQuote := reviewCitation{Seq: 34, Quote: verification}
	actualRefs := []reviewCitation{versionQuote, certificateQuote, verificationQuote}
	steps := []struct {
		number  string
		refs    []reviewCitation
		invalid *reviewEvidenceIssue
	}{
		{"3.0.18", []reviewCitation{versionQuote, {Seq: 40, Quote: events[39].Text}}, &reviewEvidenceIssue{Index: 1, Seq: 40, Reason: "source_unavailable"}},
		{"3.0.19", []reviewCitation{versionQuote, certificateQuote, {Seq: 32, Quote: events[31].Text}}, &reviewEvidenceIssue{Index: 2, Seq: 32, Reason: "source_unavailable"}},
		{"3.0.20", []reviewCitation{versionQuote, certificateQuote, {Seq: 34, Quote: "check_cert.py: exit 1"}}, &reviewEvidenceIssue{Index: 2, Seq: 34, Reason: "quote_mismatch", SourceText: events[33].Text}},
		{"3.0.21", actualRefs, nil},
		{"3.0.22", actualRefs, nil},
	}
	var legacyReceipts []string
	var legacyOutcome string
	for _, tc := range []struct {
		name  string
		dedup bool
	}{
		{"default", false},
		{"opt-in", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var pc *projectContext
			var before memory.Snapshot
			e, fixture := newReviewProtocolRequestEngine(t, func(raw json.RawMessage, request int) (string, []reviewOperation) {
				refuseFixture := func(format string, args ...any) (string, []reviewOperation) {
					t.Errorf(format, args...)
					return "The fixture cannot proceed without the expected repair.", nil
				}
				var input struct {
					Messages []struct{ Role, Content string }
				}
				if err := json.Unmarshal(raw, &input); err != nil || request > len(steps)+1 {
					return refuseFixture("invalid OpenSSL replay request %d: %v", request, err)
				}
				var feedback string
				var system strings.Builder
				for _, message := range input.Messages {
					switch message.Role {
					case "system":
						system.WriteString(message.Content)
					case "tool":
						feedback = message.Content
					}
				}
				if request == 1 {
					const legacyInstruction = "The top-level fields repeat the first issue for compatibility."
					dedupPrompt := strings.Contains(system.String(), memory.DeduplicatedReviewFeedbackInstruction)
					legacyPrompt := strings.Contains(system.String(), legacyInstruction)
					if dedupPrompt != tc.dedup || legacyPrompt == tc.dedup {
						return refuseFixture("initial reviewer prompt did not match the feedback mode")
					}
				}
				refs := actualRefs
				if request <= len(steps) {
					refs = steps[request-1].refs
				}
				if request > 1 {
					step := steps[request-2]
					snapshot, err := pc.memory.Read()
					if err != nil || !reflect.DeepEqual(snapshot, before) {
						return refuseFixture("refused attempt %d changed notes: %v", request-1, err)
					}
					full, err := restoreOpenSSLFeedback(feedback, tc.dedup)
					if err != nil {
						return refuseFixture("invalid refusal on attempt %d: %v", request-1, err)
					}
					if tc.dedup {
						if request-2 >= len(legacyReceipts) || !bytes.Equal(full, []byte(legacyReceipts[request-2])) || len(feedback) >= len(full) {
							return refuseFixture("attempt %d was not a smaller, lossless projection of the legacy reply", request-1)
						}
					} else {
						legacyReceipts = append(legacyReceipts, feedback)
					}
					var receipt struct {
						Success     bool   `json:"success"`
						Instruction string `json:"instruction"`
						Issues      []struct {
							Index  int    `json:"index"`
							Code   string `json:"code"`
							Repair struct {
								Instruction        string                `json:"instruction"`
								CitableSources     []int64               `json:"citable_sources"`
								InvalidEvidence    []reviewEvidenceIssue `json:"invalid_evidence"`
								UnsupportedNumbers []string              `json:"unsupported_numbers"`
								SupportedNumbers   []string              `json:"supported_numbers"`
							} `json:"repair"`
						} `json:"issues"`
					}
					wantIssues := 1
					if step.invalid != nil {
						wantIssues++
					}
					if err := json.Unmarshal(full, &receipt); err != nil || receipt.Success || receipt.Instruction == "" || len(receipt.Issues) != wantIssues {
						return refuseFixture("refusal %d did not contain the complete repair envelope", request-1)
					}
					if step.invalid != nil {
						issue := receipt.Issues[0]
						if issue.Code != "evidence" || issue.Index != 0 || issue.Repair.Instruction == "" ||
							!reflect.DeepEqual(issue.Repair.CitableSources, []int64{1, 9, 28, 34}) ||
							!reflect.DeepEqual(issue.Repair.InvalidEvidence, []reviewEvidenceIssue{*step.invalid}) {
							return refuseFixture("attempt %d lost exact citation repair or admitted the wrong source", request-1)
						}
						if source := issue.Repair.InvalidEvidence[0].SourceText; source != "" {
							// The third attempt must repair its quote from the
							// actual provider receipt, including its exact bytes.
							refs = append([]reviewCitation(nil), refs...)
							refs[2].Quote = source
						}
					}
					numeric := receipt.Issues[len(receipt.Issues)-1]
					if numeric.Code != "numeric" || numeric.Index != 0 || numeric.Repair.Instruction == "" ||
						!reflect.DeepEqual(numeric.Repair.UnsupportedNumbers, []string{step.number}) ||
						!reflect.DeepEqual(numeric.Repair.SupportedNumbers, []string{"0", "3.0.17", "3.13.7"}) {
						return refuseFixture("attempt %d lost supported or unsupported numeric spellings", request-1)
					}
				}
				content := accepted
				if request <= len(steps) {
					// The archive omits these drafts. Use distinct invented
					// versions to exercise each observed refusal category.
					content = strings.ReplaceAll(content, "3.0.17", steps[request-1].number)
				}
				return "", []reviewOperation{{Target: memory.ToolMemory, Action: "add", Content: content, Evidence: refs}}
			})
			if tc.dedup {
				e.cfg.Memory.ReviewDeduplicateFeedback = true
			}
			th, turn, project := qualityReviewTurn(t, e, store.TurnDone, events)
			pc = project
			var err error
			before, err = pc.memory.Read()
			if err != nil {
				t.Fatal(err)
			}
			e.scheduleReview(th.ID, turn, store.TurnDone, pc, "Certificate checks finished.")
			settleReviewProtocol(t, e)
			event := waitForReview(t, e, turn.ID)
			outcome := decodeReview(t, event)
			after, err := pc.memory.Read()
			if err != nil || !reflect.DeepEqual(after.Entries, []string{accepted}) || !outcome.Changed ||
				outcome.StopReason != "completed" || outcome.Err != "" || outcome.AttemptCount != 6 ||
				len(outcome.Attempts) != 6 || len(outcome.Refused) != 8 || len(outcome.Changes) != 1 || len(outcome.Evidence) != 1 ||
				!reflect.DeepEqual(outcome.Evidence[0].Sources, []int64{9, 28, 34}) || len(fixture.snapshot()) != 6 {
				t.Fatalf("OpenSSL repair sequence did not commit exactly one supported note: requests=%d refusals=%d err=%v", len(fixture.snapshot()), len(outcome.Refused), err)
			}
			refused := 0
			for i, attempt := range outcome.Attempts {
				status, refs := "accepted", actualRefs
				var codes []string
				if i < len(steps) {
					status, refs = "refused", steps[i].refs
					if steps[i].invalid != nil {
						codes = append(codes, "evidence")
					}
					codes = append(codes, "numeric")
				}
				sources := make([]int64, len(refs))
				for k, ref := range refs {
					sources[k] = ref.Seq
				}
				wantOperation := reviewAttemptOperation{Target: memory.ToolMemory, Action: "add", Sources: sources}
				if attempt.Number != i+1 || attempt.Status != status ||
					!reflect.DeepEqual(attempt.Operations, []reviewAttemptOperation{wantOperation}) {
					t.Fatalf("durable attempt %d lost its status or source identities", i+1)
				}
				for _, code := range codes {
					got := outcome.Refused[refused]
					if got.Code != code || got.Index != 0 || got.Attempt != i+1 || !reflect.DeepEqual(got.Sources, sources) {
						t.Fatalf("durable refusal %d lost its code, index, attempt, or source identities", refused+1)
					}
					refused++
				}
			}
			for _, private := range []string{privateProbe, "invalid_evidence", "source_text", "unsupported_numbers", "supported_numbers", "3.0.18", "3.0.22"} {
				if strings.Contains(event.Text, private) {
					t.Errorf("private repair content %q reached durable diagnostics", private)
				}
			}
			if tc.dedup {
				if event.Text != legacyOutcome {
					t.Fatal("feedback option changed the persisted review outcome")
				}
			} else {
				legacyOutcome = event.Text
			}
		})
	}
}

func restoreOpenSSLFeedback(feedback string, dedup bool) ([]byte, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(feedback), &root); err != nil {
		return nil, err
	}
	var issues []map[string]json.RawMessage
	if err := json.Unmarshal(root["issues"], &issues); err != nil || len(issues) == 0 {
		return nil, fmt.Errorf("missing issues: %w", err)
	}
	if !dedup {
		for k, v := range issues[0] {
			if !bytes.Equal(v, root[k]) {
				return nil, fmt.Errorf("legacy reply lost first issue field %q", k)
			}
		}
		return []byte(feedback), nil
	}
	if len(root) != 3 || string(root["success"]) != "false" {
		return nil, fmt.Errorf("compact reply retained a duplicate or lost refusal status")
	}
	for _, issue := range issues {
		if issue["success"] != nil || issue["note"] != nil || issue["error"] == nil {
			return nil, fmt.Errorf("compact issue has duplicate status/note or lacks its error")
		}
		issue["success"], issue["note"] = root["success"], issue["error"]
	}
	for k, v := range issues[0] {
		root[k] = v
	}
	var err error
	root["issues"], err = json.Marshal(issues)
	if err != nil {
		return nil, err
	}
	return json.Marshal(root)
}
