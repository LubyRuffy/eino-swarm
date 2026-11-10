package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const (
	dreamHTTPSkill    = "check-transfer-receipt"
	dreamHTTPGood     = "Check the recorded byte count before accepting a complete transfer."
	dreamHTTPBad      = "A transfer with shell exit zero is always complete."
	dreamHTTPSeed     = "Inspect the earlier transfer record."
	dreamHTTPAudit    = "Audit the independent transfer records."
	dreamHTTPNext     = "Apply the procedure in a separate conversation."
	dreamHTTPNote     = "Retain independent transfer receipts with the run log."
	dreamHTTPOther    = "Use the raw hexadecimal digest."
	dreamHTTPAppendix = "\n\nDream revision audit:\n"
)

// One ordinary reviewer request handles the audit, and the next conversation
// reads the actual restored document. The caller does not name a revision,
// evidence sequence, or a replacement body.
func TestDreamRestoresLearnedRuleFromActualPostTurnEvidence(t *testing.T) {
	for _, status := range []string{"restored", "keep", "uncertain", "invalid", "bare_revert", "malformed", "not_submitted", "revision_changed", "source_changed", "enter_plan", "duplicate", "clipped", "disabled", "blocked_patch", "blocked_merge", "blocked_patch_title", "blocked_merge_title", "read_other_skill", "unsupported_optional_audit"} {
		t.Run(status, func(t *testing.T) { runDreamHTTPScenario(t, status, "") })
	}
	t.Run("explicit_review_effort", func(t *testing.T) { runDreamHTTPScenario(t, "restored", "medium") })
	t.Run("explicit_review_without_thinking", func(t *testing.T) { runDreamHTTPScenario(t, "restored", "none") })
}

func runDreamHTTPScenario(t *testing.T, expected, explicitEffort string) {
	t.Helper()
	var e *Engine
	var seedThread, auditThread, projectID string
	var reviewCalls, auditCalls atomic.Int32
	dreamEndpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var sdk struct {
			scheduleCompletionRequest
			Choice json.RawMessage `json:"tool_choice"`
		}
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" || json.NewDecoder(r.Body).Decode(&sdk) != nil || len(sdk.Messages) == 0 {
			t.Error("unexpected local SDK request")
			http.Error(w, "bad fixture request", http.StatusBadRequest)
			return
		}
		req := sdk.scheduleCompletionRequest
		if replyNoScheduleClaimsFixture(t, w, req.Messages[0].Content, req.Stream, len(req.Tools)) {
			return
		}

		var user string
		results := map[string]string{}
		for _, msg := range req.Messages {
			if msg.Role == "user" {
				user = msg.Content
			}
			if msg.Role == "tool" {
				results[msg.ToolCallID] = msg.Content
			}
		}
		for _, offered := range req.Tools {
			if offered.Function.Name != memory.ToolApplyReview {
				continue
			}
			reviewCalls.Add(1)
			transcript, appendix, hasAudit := strings.Cut(user, dreamHTTPAppendix)
			dreamField, hasAuditField := offered.Function.Parameters.Properties["dream"]
			excluded := offered.Function.Parameters.Properties["changes"].Description == "Exclude the audited skill."
			if excluded != hasAudit {
				t.Errorf("ordinary change schema has exclusion=%t (audit=%t)", excluded, hasAudit)
			}
			if hasAuditField && dreamField.Description != "Decision for the supplied Dream audit." {
				t.Errorf("unexpected Dream tool schema: %+v", dreamField)
			}
			wantEffort := explicitEffort
			if wantEffort == "" {
				wantEffort = "low"
				if hasAudit {
					wantEffort = "none"
				}
			}
			if req.ReasoningEffort == nil || *req.ReasoningEffort != wantEffort {
				t.Errorf("review reasoning must be %q (audit=%t), got %v", wantEffort, hasAudit, req.ReasoningEffort)
			}
			hasAuditInstruction := strings.HasPrefix(req.Messages[0].Content, "For the Dream revision audit,")
			if hasAudit {
				if len(req.Tools) != 1 || req.Tools[0].Function.Name != memory.ToolApplyReview {
					t.Error("the named audit request sent tool schemas it cannot call")
				}
				var choice struct {
					Type     string                `json:"type"`
					Function struct{ Name string } `json:"function"`
				}
				if err := json.Unmarshal(sdk.Choice, &choice); err != nil || choice.Type != "function" || choice.Function.Name != memory.ToolApplyReview {
					t.Errorf("audit did not select its named batch tool: %s (%v)", sdk.Choice, err)
				}
			} else if len(sdk.Choice) != 0 && string(sdk.Choice) != `"auto"` {
				t.Errorf("ordinary review inherited forced tool choice: %s", sdk.Choice)
			}
			if !req.Stream || hasAuditField != hasAudit || hasAuditInstruction != hasAudit {
				t.Errorf("reviewer call stream=%t, dream field=%t, instruction=%t, appendix=%t",
					req.Stream, hasAuditField, hasAuditInstruction, hasAudit)
			}
			if prior, retried := results["submit-review"]; retried && strings.HasPrefix(expected, "blocked_") {
				if !strings.Contains(prior, "the audited skill must use the Dream decision") || hasAudit {
					t.Errorf("blocked ordinary write did not fall back from the audit: %s", prior)
				}
				assertDreamHTTPHistoryOmitsVerdict(t, req)
			}
			ops := []reviewOperation{}
			response := map[string]any{}
			if strings.Contains(user, dreamHTTPSeed) {
				for _, event := range dreamHTTPEvents(t, e, seedThread) {
					if event.Kind == "tool_result" && event.ToolCallID == "original-receipt" {
						ops = append(ops, reviewOperation{Target: memory.ToolSkillManage, Action: "patch", Name: dreamHTTPSkill,
							OldText: dreamHTTPGood, NewText: dreamHTTPBad, Evidence: []reviewCitation{{Seq: event.Seq, Quote: "transfer complete"}}})
					}
				}
			}
			if !hasAudit && expected == "read_other_skill" && strings.Contains(user, dreamHTTPAudit) {
				if len(req.Tools) != 2 {
					t.Error("ordinary continuation lost its normal tools")
				}
				if results["other-view"] == "" {
					if !strings.Contains(results["defer-review"], "Continue the ordinary memory review.") {
						t.Errorf("audit did not defer to ordinary review: %s", results["defer-review"])
					}
					writeDreamHTTPTool(w, req, "other-view", memory.ToolSkillView, map[string]string{"name": "checksum-format"})
					return
				}
				if !strings.Contains(results["other-view"], dreamHTTPOther) {
					t.Error("ordinary review lost the actual result of a different skill read")
				}
				var ids []string
				for _, event := range dreamHTTPEvents(t, e, auditThread) {
					if event.Kind == "tool_result" && (event.ToolCallID == "later-receipt" || event.ToolCallID == "later-byte-count") {
						ids = append(ids, strconv.FormatInt(event.Seq, 10))
					}
					if event.Kind == "tool_result" && event.ToolCallID == "later-receipt" {
						ops = append(ops, reviewOperation{Target: memory.ToolMemory, Action: "add", Content: dreamHTTPNote,
							Evidence: []reviewCitation{{Seq: event.Seq, Quote: "short receipt"}}})
					}
				}
				// A fabricated late verdict cannot restore the skill or block
				// an otherwise valid ordinary proposal.
				if len(ids) != 2 {
					t.Errorf("late verdict needs both actual receipt IDs: %v", ids)
					return
				}
				response["dream"] = map[string]any{"decision": "revert_using_" + ids[0] + "_and_" + ids[1], "quote": dreamHTTPBad}
			}
			if hasAudit {
				auditCalls.Add(1)
				if expected == "unsupported_optional_audit" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":{"message":"unknown parameter: reasoning_effort","type":"invalid_request_error"}}`))
					return
				}
				result, ids := checkDreamHTTPAppendix(t, transcript, appendix, dreamHTTPEvents(t, e, auditThread))
				if expected == "read_other_skill" {
					writeDreamHTTPTool(w, req, "defer-review", memory.ToolApplyReview, map[string]any{
						"changes": ops, "dream": map[string]string{"decision": "defer"},
					})
					return
				}
				if len(ids) != 2 {
					t.Errorf("audit did not contain two result references: %v", ids)
					return
				}
				verdict := map[string]any{"decision": "revert_using_" + ids[0] + "_and_" + ids[1], "quote": dreamHTTPBad}
				response["dream"] = verdict
				switch expected {
				case "keep":
					response["dream"] = map[string]string{"decision": "keep", "quote": "A redundant explanation for a no-write decision."}
				case "uncertain":
					if !strings.Contains(req.Messages[0].Content, "same operation under the conditions in the changed claim") ||
						!strings.Contains(req.Messages[0].Content, "if they only test related behavior or omit a condition") {
						t.Error("the actual audit request must distinguish a preview from a receipt produced by a transfer")
					}
					response["dream"] = map[string]string{"decision": "uncertain"}
				case "bare_revert":
					verdict["decision"] = "revert"
					verdict["sources"] = ids
				case "invalid", "malformed", "not_submitted":
					ops = append(ops, reviewOperation{Target: memory.ToolMemory, Action: "add", Content: dreamHTTPNote,
						Evidence: []reviewCitation{{Seq: result, Quote: "short receipt"}}})
					switch expected {
					case "invalid":
						verdict["quote"] = "An invented clause"
					case "malformed":
						response["dream"] = "not a decision"
					case "not_submitted":
						delete(response, "dream")
					}
				case "revision_changed":
					if _, err := e.ProjectMemory(projectID).WriteSkill(dreamHTTPSkill, "A later human description.", dreamHTTPBad); err != nil {
						t.Error(err)
					}
				case "source_changed":
					if err := e.MoveThread(seedThread, ""); err != nil {
						t.Error(err)
					}
				case "enter_plan":
					if err := e.SetPlanMode(auditThread, true); err != nil {
						t.Error(err)
					}
				case "blocked_patch", "blocked_merge", "blocked_patch_title", "blocked_merge_title":
					op := reviewOperation{Target: memory.ToolSkillManage, Action: "patch", Name: dreamHTTPSkill,
						OldText: dreamHTTPBad, NewText: "An ordinary proposal cannot edit the audited skill.",
						Evidence: []reviewCitation{{Seq: result, Quote: "short receipt"}}}
					if strings.HasSuffix(expected, "_title") {
						op.Name = "Check Transfer Receipt"
					}
					if strings.HasPrefix(expected, "blocked_merge") {
						op.Action, op.Name, op.Sources = "merge", "transfer-log-format", []string{op.Name}
						op.OldText, op.NewText = "", ""
					}
					ops = append(ops, op)
				}
			}
			response["changes"] = ops
			writeDreamHTTPTool(w, req, "submit-review", memory.ToolApplyReview, response)
			return
		}
		switch user {
		case dreamHTTPSeed:
			if results["original-receipt"] == "" {
				writeDreamHTTPTool(w, req, "original-receipt", "exec", map[string]string{"command": "printf '%s' 'transfer complete'"})
				return
			}
		case dreamHTTPAudit:
			switch {
			case results["view"] == "":
				writeDreamHTTPTool(w, req, "view", memory.ToolSkillView, map[string]string{"name": dreamHTTPSkill})
				return
			case results["later-receipt"] == "":
				if !strings.Contains(results["view"], dreamHTTPBad) {
					t.Error("the auditing turn did not actually read the learned skill")
				}
				command := "printf '%s' 'short receipt'"
				if expected == "uncertain" {
					command = "printf '%s' 'preview: short receipt'"
				}
				writeDreamHTTPTool(w, req, "later-receipt", "exec", map[string]string{"command": command})
				return
			case results["later-byte-count"] == "":
				command := "printf '%s' 'short byte count'"
				switch expected {
				case "duplicate":
					command = "printf '%s' 'short receipt'"
				case "clipped":
					command += "; printf '%02500d' 0"
				case "uncertain":
					command = "printf '%s' 'preview: short byte count'"
				}
				writeDreamHTTPTool(w, req, "later-byte-count", "exec", map[string]string{"command": command})
				return
			}
		case dreamHTTPNext:
			if results["next-view"] == "" {
				writeDreamHTTPTool(w, req, "next-view", memory.ToolSkillView, map[string]string{"name": dreamHTTPSkill})
				return
			}
			if !strings.Contains(results["next-view"], dreamHTTPGood) || strings.Contains(results["next-view"], dreamHTTPBad) {
				t.Error("the next conversation did not receive the restored document")
			}
		default:
			t.Errorf("unexpected foreground request %q", user)
		}
		writeContextFlowReply(w, req.Stream, map[string]any{"content": "The transfer records were checked."}, "stop")
	}))
	t.Cleanup(dreamEndpoint.Close)
	e = newTestEngine(t)
	if expected == "disabled" {
		e.cfg.Memory.AutoDream = false
	}
	e.cfg.Memory.ReviewReasoningEffort = explicitEffort
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Swarm.ComposerPredictions = false
	e.cfg.Models.Providers[0].BaseURL = dreamEndpoint.URL
	e.cfg.Models.Providers[0].Model = "local-dream-fixture"
	e.pool = provider.New(e.cfg)
	project, source := projectThread(t, e)
	seedThread, projectID = source.ID, project.ID
	mem := e.ProjectMemory(project.ID)
	if _, err := mem.WriteSkill(dreamHTTPSkill, "Check whether an observed transfer is complete.", dreamHTTPGood); err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(expected, "blocked_merge") {
		if _, err := mem.WriteSkill("transfer-log-format", "Format the transfer log.", "Use plain text for transfer logs."); err != nil {
			t.Fatal(err)
		}
	}
	if expected == "read_other_skill" {
		if _, err := mem.WriteSkill("checksum-format", "Represent the calculated checksum.", dreamHTTPOther); err != nil {
			t.Fatal(err)
		}
	}
	seed := runDreamHTTPTurn(t, e, source.ID, dreamHTTPSeed)
	if seed.ProjectID != project.ID || len(seed.Changes) != 1 || seed.Dream != nil || reviewCalls.Load() != 1 || auditCalls.Load() != 0 {
		t.Fatalf("the original review did not durably record one scoped patch: %+v", seed)
	}
	audit, err := e.CreateThread("Independent verification", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	auditThread = audit.ID
	checked := runDreamHTTPTurn(t, e, audit.ID, dreamHTTPAudit)
	wantReviews, wantAudits := int32(2), int32(1)
	wantStatus := expected
	switch expected {
	case "duplicate", "clipped", "disabled":
		wantAudits, wantStatus = 0, ""
	case "enter_plan":
		wantStatus = ""
	case "malformed", "bare_revert":
		wantStatus = "invalid"
	case "blocked_patch", "blocked_merge", "blocked_patch_title", "blocked_merge_title", "unsupported_optional_audit":
		wantReviews, wantStatus = 3, "not_submitted"
	case "read_other_skill":
		wantReviews, wantStatus = 4, "not_submitted"
	}
	if reviewCalls.Load() != wantReviews || auditCalls.Load() != wantAudits {
		t.Fatalf("review used %d requests and %d audit appendices, want %d and %d: %+v",
			reviewCalls.Load(), auditCalls.Load(), wantReviews, wantAudits, checked)
	}
	if wantStatus == "" {
		if checked.Dream != nil {
			t.Fatalf("an ineligible observation was audited or applied: %+v", checked)
		}
	} else if checked.Dream == nil || checked.Dream.Status != wantStatus || checked.Dream.Patch.ThreadID != source.ID || len(checked.Dream.Sources) != 2 {
		t.Fatalf("verification did not produce the expected %q result: %+v", wantStatus, checked)
	}
	if expected == "invalid" || expected == "bare_revert" || expected == "malformed" {
		raw, err := json.Marshal(checked.Dream)
		var diagnostic struct {
			InvalidReason string `json:"invalid_reason"`
		}
		wantReason := "quote_not_new"
		switch expected {
		case "bare_revert":
			wantReason = "invalid_decision"
		case "malformed":
			wantReason = "invalid_object"
		}
		if err != nil || json.Unmarshal(raw, &diagnostic) != nil || diagnostic.InvalidReason != wantReason ||
			strings.Contains(string(raw), "An invented clause") || strings.Contains(string(raw), "not a decision") {
			t.Fatalf("audit refusal must retain only its reason code %q: %s", wantReason, raw)
		}
	}
	if checked.Err != "" || checked.StopReason != "completed" {
		t.Fatalf("the ordinary batch did not complete: %+v", checked)
	}
	if expected != "restored" {
		skill, err := mem.ReadSkill(dreamHTTPSkill)
		if err != nil || skill.Body != dreamHTTPBad || len(checked.Skills) != 0 {
			t.Fatalf("a non-restoring decision changed the skill: %+v %v", skill, err)
		}
		if expected == "revision_changed" && skill.Description != "A later human description." {
			t.Fatal("a later manual metadata edit was overwritten")
		}
		if expected == "invalid" || expected == "malformed" || expected == "not_submitted" || expected == "read_other_skill" {
			snapshot, err := mem.Read()
			if err != nil || len(snapshot.Entries) != 1 || snapshot.Entries[0] != dreamHTTPNote || checked.Notes["add"] != 1 {
				t.Fatalf("the optional audit lost an ordinary note: %+v %v", snapshot, err)
			}
		}
		if expected == "clipped" {
			var complete bool
			for _, ev := range dreamHTTPEvents(t, e, audit.ID) {
				complete = complete || ev.Kind == "tool_result" && ev.ToolCallID == "later-byte-count" && strings.Contains(ev.Text, strings.Repeat("0", 2500))
			}
			if !complete {
				t.Fatal("the fixture did not retain the later result before reviewer clipping")
			}
		}
		if expected == "uncertain" {
			_, document, err := mem.ReadSkillRevision(dreamHTTPSkill)
			if err != nil || seed.Changes[0].After == nil || document != *seed.Changes[0].After ||
				len(checked.Changes) != 0 || len(checked.Dream.Selected) != 0 || checked.Dream.InvalidReason != "" {
				t.Fatalf("out-of-scope results did not leave the complete revision untouched: %+v (%v)", checked, err)
			}
		}
		if expected == "keep" {
			raw, err := json.Marshal(checked.Dream)
			if err != nil || checked.Dream.InvalidReason != "" || strings.Contains(string(raw), "A redundant explanation") {
				t.Fatalf("keep retained the explanation or a rejection: %s (%v)", raw, err)
			}
			repeat, err := e.CreateThread("Same evidence, different conversation", "", project.ID)
			if err != nil {
				t.Fatal(err)
			}
			auditThread = repeat.ID
			if repeated := runDreamHTTPTurn(t, e, repeat.ID, dreamHTTPAudit); repeated.Dream != nil || auditCalls.Load() != 1 || reviewCalls.Load() != 3 {
				t.Fatalf("the same effective input was audited twice: %+v", repeated)
			}
		}
		return
	}
	if len(checked.Changes) != 1 || len(checked.Dream.Selected) != 2 ||
		checked.Dream.Selected[0] != checked.Dream.Sources[0] || checked.Dream.Selected[1] != checked.Dream.Sources[1] {
		t.Fatalf("the real review did not restore one revision using its explicitly chosen receipts: %+v", checked)
	}
	change := checked.Changes[0]
	if change.Before == nil || change.After == nil || *change.Before != *seed.Changes[0].After || *change.After != *seed.Changes[0].Before {
		t.Fatalf("the receipt does not describe the actual restored bytes: %+v", change)
	}
	next, err := e.CreateThread("Later reader", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := runDreamHTTPTurn(t, e, next.ID, dreamHTTPNext); got.Dream != nil || auditCalls.Load() != 1 || reviewCalls.Load() != 3 {
		t.Fatalf("an ordinary skill read without later observations called the verifier: %+v", got)
	}
}

func assertDreamHTTPHistoryOmitsVerdict(t *testing.T, req scheduleCompletionRequest) {
	t.Helper()
	for _, msg := range req.Messages {
		for _, call := range msg.ToolCalls {
			if call.ID != "submit-review" {
				continue
			}
			var args map[string]json.RawMessage
			if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil || len(args["changes"]) == 0 || args["dream"] != nil {
				t.Errorf("ordinary fallback did not retain only the original ordinary proposal: %q (%v)", call.Function.Arguments, err)
			}
			return
		}
	}
	t.Error("ordinary fallback lost the previous proposal from its tool history")
}

func checkDreamHTTPAppendix(t *testing.T, transcript, appendix string, events []store.Event) (int64, []string) {
	t.Helper()
	var input struct {
		PreviousExcerpt string              `json:"previous_excerpt"`
		CurrentExcerpt  string              `json:"current_excerpt"`
		CurrentResult   int64               `json:"current_result"`
		Original        []json.RawMessage   `json:"original_observations"`
		Later           [][]json.RawMessage `json:"later"`
	}
	if err := json.Unmarshal([]byte(appendix), &input); err != nil || input.PreviousExcerpt != dreamHTTPGood+"\n" || input.CurrentExcerpt != dreamHTTPBad+"\n" ||
		len(input.Original) != 1 || len(input.Later) != 2 {
		t.Errorf("missing paired exact revision excerpts in the actual audit: %+v %v", input, err)
		return 0, nil
	}
	var original map[string]json.RawMessage
	if err := json.Unmarshal(input.Original[0], &original); err != nil || len(original) != 4 ||
		string(original["stdout"]) != `"transfer complete"` || string(original["exit_code"]) != "0" ||
		string(original["workdir"]) != `"1"` || string(original["command"]) != `"printf '%s' 'transfer complete'"` {
		t.Errorf("the original observation lost its scope or included redundant data: %s", input.Original[0])
	}
	if strings.Contains(appendix, "description:") || strings.Contains(appendix, "short receipt") || strings.Contains(appendix, "short byte count") {
		t.Error("the audit repeated evidence already visible in the ordinary transcript")
	}
	visible := func(seq int64, kind, id, body string) bool {
		for _, ev := range events {
			if ev.Seq == seq && ev.Kind == kind && ev.ToolCallID == id && strings.Contains(ev.Text, body) &&
				strings.Contains(transcript, fmt.Sprintf("[seq=%d kind=%s", ev.Seq, ev.Kind)) && strings.Contains(transcript, ev.Text) {
				return true
			}
		}
		return false
	}
	if !visible(input.CurrentResult, "tool_result", "view", dreamHTTPBad) {
		t.Error("the current revision reference does not identify the visible skill result")
	}
	var firstResult int64
	ids := make([]string, 0, len(input.Later))
	for i, result := range []struct{ id, text string }{{"later-receipt", "short receipt"}, {"later-byte-count", "short byte count"}} {
		ref := input.Later[i]
		var callSeq, resultSeq int64
		if len(ref) != 3 || json.Unmarshal(ref[0], &callSeq) != nil || json.Unmarshal(ref[1], &resultSeq) != nil ||
			string(ref[2]) != string(original["workdir"]) ||
			!visible(callSeq, "tool_call", result.id, result.text) ||
			!visible(resultSeq, "tool_result", result.id, result.text) {
			t.Errorf("later reference %s does not identify a complete visible receipt for %s", ref, result.id)
		}
		if i == 0 {
			firstResult = resultSeq
		}
		ids = append(ids, strconv.FormatInt(resultSeq, 10))
	}
	return firstResult, ids
}

func dreamHTTPEvents(t *testing.T, e *Engine, thread string) []store.Event {
	t.Helper()
	turns, err := e.Store().ListTurns(thread)
	if err != nil || len(turns) != 1 {
		t.Errorf("fixture source turn is absent: %v", err)
		return nil
	}
	events, err := e.Store().ListTurnEvents(turns[0].ID)
	if err != nil {
		t.Errorf("fixture events unavailable: %v", err)
		return nil
	}
	return events
}

func writeDreamHTTPTool(w http.ResponseWriter, req scheduleCompletionRequest, id, name string, args any) {
	body, _ := json.Marshal(args)
	writeContextFlowReply(w, req.Stream, map[string]any{"tool_calls": []any{map[string]any{
		"index": 0, "id": id, "type": "function", "function": map[string]any{"name": name, "arguments": string(body)},
	}}}, "tool_calls")
}

func runDreamHTTPTurn(t *testing.T, e *Engine, thread, input string) reviewOutcome {
	t.Helper()
	turn, err := e.StartTurn(thread, input)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForTurn(t, e, turn.ID); got.Status != store.TurnDone {
		t.Fatalf("the actual turn did not finish: %+v", got)
	}
	return decodeReview(t, waitForReview(t, e, turn.ID))
}
