package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

func historyAnswerJSON(thread string) string {
	body, _ := json.Marshal(map[string]any{"version": 1, "language": "en", "sources": []any{map[string]any{"key": "original", "source": map[string]any{"kind": "message", "thread_id": thread, "seq": 1}}}, "proposals": []any{}, "questions": []string{}})
	return string(body)
}

func TestHistoryFormattingBudgetAndUnavailableScopeCannotFallBackToGeneratedProse(t *testing.T) {
	e, turn, peer, b, set := historyAnswerFixture(t)
	historyReportRead(e, turn, "manager")
	res, failure := b.prepareHistoryAnswer(t.Context(), turn, set, historyCandidate(b, historyAnswerJSON(peer.ID)), nil)
	if failure != nil {
		t.Fatal(failure, failure.Cause)
	}
	previousLimit := e.cfg.Tools.ResultMaxChars
	e.cfg.Tools.ResultMaxChars = utf8.RuneCountInString(res.Final) - 1
	if _, failure := b.prepareHistoryAnswer(t.Context(), turn, set, historyCandidate(b, historyAnswerJSON(peer.ID)), nil); failure == nil || failure.Code != "history_report_invalid" {
		t.Fatalf("host output budget bypassed: %+v", failure)
	}
	e.cfg.Tools.ResultMaxChars = previousLimit
	if err := e.store.DB().Callback().Query().Before("gorm:query").Register("history_scope_read_outage", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "threads" {
			_ = tx.AddError(errors.New("private scope read unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, failure := b.prepareHistoryAnswer(t.Context(), turn, nil, historyCandidate(b, historyAnswerJSON(peer.ID)), nil); failure == nil || failure.Code != "history_state_unavailable" || isHistoryAnswerCorrection(failure) {
		t.Fatalf("unreadable scope fell back to global history: %+v", failure)
	}
}

func historyAnswerFixture(t *testing.T) (*Engine, *store.Turn, *store.Thread, *managerAnswerBuffer, *tools.Set) {
	t.Helper()
	e := newTestEngine(t)
	project, err := e.CreateProject("Canonical history", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := e.CreateThread("Original proposal", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.AppendMessages(peer.ID, "", []store.Message{{Role: "assistant", Content: "The operation is not implemented.\nDeletion remains prohibited unless separately approved."}}); err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Bound report", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn := mustStoredTurn(t, e, thread.ID, store.Turn{})
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	_, release := ownManagerTestFixture(t.Context(), e.runtimeFor(thread.ID), turn.ID, reg)
	t.Cleanup(release)
	set, err := e.conversationTools(t.Context(), thread)
	if err != nil {
		t.Fatal(err)
	}
	return e, turn, peer, newManagerAnswerBuffer(newAccumulator(e, thread.ID, turn.ID, 0)), set
}

func historyReadEvent(e *Engine, turn *store.Turn, agent string) {
	e.record(store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: "tool_call", AgentID: agent, ToolCallID: "history-call", Text: "query_history({})"})
}

// historyReportRequested is the explicit project-report request that selects
// the source protocol. A read alone leaves the turn's answer ordinary.
func historyReportRequested(e *Engine, turn *store.Turn) {
	e.record(store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: KindHistoryReportRequested, AgentID: "manager"})
}

// historyReportRead is an explicit report turn that has actually read history.
func historyReportRead(e *Engine, turn *store.Turn, agent string) {
	historyReportRequested(e, turn)
	historyReadEvent(e, turn, agent)
}

func historyCandidate(b *managerAnswerBuffer, text string) swarm.RunResult {
	message := markPendingAnswer(schema.AssistantMessage(text, nil), b.id)
	b.generated = []*schema.Message{message}
	b.complete = []swarm.Notification{{Kind: swarm.NotifyAgentMessage, AgentID: "manager", Role: "manager", MessageID: "original-output", Text: text}}
	return swarm.RunResult{Final: text, Transcript: []adk.Message{schema.UserMessage("Inspect the original history."), message}}
}

func TestHistoryAnswerReplacesEveryCurrentProseRepresentationAndKeepsOriginalAuthority(t *testing.T) {
	e, turn, peer, b, set := historyAnswerFixture(t)
	historyReportRead(e, turn, "worker-source-reader")
	res := historyCandidate(b, historyAnswerJSON(peer.ID))
	intermediate := markPendingAnswer(schema.AssistantMessage("Unverified intermediate conclusion", []schema.ToolCall{{ID: "actual-call", Function: schema.FunctionCall{Name: "read_task_messages", Arguments: "{}"}}}), b.id)
	intermediate.ReasoningContent = "Actual model reasoning."
	intermediate.Extra["responses_output"] = json.RawMessage(`[{"type":"reasoning","id":"reason","encrypted_content":"retain"},{"type":"message","id":"unverified","content":[{"type":"output_text","text":"Unverified intermediate conclusion"}]}]`)
	res.Transcript = append([]adk.Message{res.Transcript[0], intermediate, schema.ToolMessage("actual receipt", "actual-call")}, res.Transcript[1])
	got, failure := b.prepareHistoryAnswer(t.Context(), turn, set, res, nil)
	if failure != nil {
		t.Fatal(failure, failure.Cause)
	}
	if !strings.Contains(got.Final, "not implemented") || !strings.Contains(got.Final, "unless separately approved") || !strings.Contains(got.Final, "assistant") || strings.Contains(got.Final, `"version":1`) {
		t.Fatalf("host rendering lost original authority or negation: %s", got.Final)
	}
	joined := joinedContent(got.Transcript)
	if strings.Contains(joined, "Unverified intermediate conclusion") || strings.Contains(joined, `"version":1`) || !strings.Contains(joined, "actual receipt") {
		t.Fatalf("unsafe prose or lost tool/reasoning: %s", joined)
	}
	keptReasoning := false
	for _, message := range got.Transcript {
		keptReasoning = keptReasoning || message != nil && message.ReasoningContent == intermediate.ReasoningContent
	}
	if !keptReasoning {
		t.Fatal("rewriting history prose dropped actual model reasoning")
	}
	if intermediate.Content != "Unverified intermediate conclusion" {
		t.Fatal("rewrote a shared source message")
	}
	row, failure := b.prepare(turn, got, nil)
	if failure != nil {
		t.Fatal(failure)
	}
	var candidate pendingScheduleAnswer
	if err := json.Unmarshal([]byte(row.Payload), &candidate); err != nil {
		t.Fatal(err)
	}
	if candidate.HistoryReport == nil || candidate.HistoryReport.Evidence.ProjectID != peer.ProjectID || candidate.Events[0].MessageID != "original-output" {
		t.Fatalf("private report lost source or generation identity: %+v", candidate)
	}
	if err := validateStoredHistoryReport(&candidate, e.cfg.Tools.ToolResultLimit()); err != nil {
		t.Fatal(err)
	}
	if failure := e.verifyPendingHistoryAnswer(t.Context(), turn, &candidate); failure != nil {
		t.Fatal(failure, failure.Cause)
	}
	if failure := e.admitScheduleAnswerWithHistory(turn.ThreadID, turn.ID, nil, candidate.Events, &candidate.HistoryReport.Evidence, row.ID); failure != nil {
		t.Fatal(failure, failure.Cause)
	}
	events, _ := e.store.ListTurnEvents(turn.ID)
	public := 0
	for _, event := range events {
		if event.Kind == "agent_message" {
			public++
			if event.Text != got.Final {
				t.Fatal("published another answer")
			}
		}
	}
	if public != 1 {
		t.Fatalf("public answer count=%d", public)
	}
}

func TestHistoryReportMechanicalRefusalsShareOneDurableCorrectionAllowance(t *testing.T) {
	e, turn, peer, b, set := historyAnswerFixture(t)
	initial := historyCandidate(b, "ordinary answer")
	got, failure := b.prepareHistoryAnswer(t.Context(), turn, set, initial, nil)
	if failure != nil || got.Final != initial.Final {
		t.Fatal("ordinary non-history turn changed")
	}
	e.record(store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: "history_report_requested", AgentID: "manager"})
	_, failure = b.prepareHistoryAnswer(t.Context(), turn, set, initial, nil)
	if failure == nil || failure.Code != "history_read_required" {
		t.Fatalf("missing actual read was accepted: %+v", failure)
	}
	if retry := e.runtimeFor(turn.ThreadID).recordScheduleAnswerRetry(turn, failure); retry != nil {
		t.Fatal(retry)
	}
	if consumed, err := e.scheduleAnswerContinued(turn); err != nil || !consumed {
		t.Fatalf("history repair budget not durable: %t %v", consumed, err)
	}
	historyReadEvent(e, turn, "manager")
	for _, text := range []string{"free final prose", strings.Replace(historyAnswerJSON(peer.ID), `"language":"en"`, `"language":"en","role":"user"`, 1), historyAnswerJSON("missing-source")} {
		_, failure := b.prepareHistoryAnswer(t.Context(), turn, set, historyCandidate(b, text), nil)
		if !isHistoryAnswerCorrection(failure) {
			t.Fatalf("mechanical report failure was not repairable: %+v", failure)
		}
	}
	_, failure = b.prepareHistoryAnswer(t.Context(), turn, set, historyCandidate(b, historyAnswerJSON(peer.ID)), nil)
	if failure != nil {
		t.Fatal(failure, failure.Cause)
	}
	if !strings.Contains((&scheduleAnswerError{Code: "history_report_invalid"}).Error(), "history_report_unverified") || strings.Contains((&scheduleAnswerError{Code: "history_report_invalid"}).Error(), "future check") {
		t.Fatal("history refusal described an unrelated schedule claim")
	}
}

func TestHistorySourceChangesCannotPublishAnEarlierValidatedReport(t *testing.T) {
	for _, change := range []string{"body", "project", "deleted", "stop", "owner-deleted"} {
		t.Run(change, func(t *testing.T) {
			e, turn, peer, b, set := historyAnswerFixture(t)
			historyReportRead(e, turn, "manager")
			got, failure := b.prepareHistoryAnswer(t.Context(), turn, set, historyCandidate(b, historyAnswerJSON(peer.ID)), nil)
			if failure != nil {
				t.Fatal(failure, failure.Cause)
			}
			row, failure := b.prepare(turn, got, nil)
			if failure != nil {
				t.Fatal(failure)
			}
			switch change {
			case "body":
				e.store.DB().Model(&store.Message{}).Where("thread_id = ?", peer.ID).Update("content", "Changed original text")
			case "project":
				e.store.DB().Model(&store.Thread{}).Where("id = ?", peer.ID).Update("project_id", "")
			case "deleted":
				e.store.DB().Where("thread_id = ?", peer.ID).Delete(&store.Message{})
			case "stop":
				e.record(store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: KindUserStopRequested, AgentID: "manager"})
			case "owner-deleted":
				e.recordMu.Lock()
				e.droppedTurns[turn.ID] = struct{}{}
				e.recordMu.Unlock()
			}
			failure = e.admitScheduleAnswerWithHistory(turn.ThreadID, turn.ID, nil, []store.Event{{Kind: "agent_message", AgentID: "manager", Text: got.Final}}, &b.historyReport.Evidence, row.ID)
			if failure == nil {
				t.Fatal("source/owner change escaped atomic report admission")
			}
			if change == "body" || change == "project" || change == "deleted" {
				if failure.Code != "history_source_changed" {
					t.Fatalf("source refusal=%+v", failure)
				}
			}
			messages, _ := e.store.ListMessages(turn.ThreadID)
			for _, message := range messages {
				if message.Role == "assistant" {
					t.Fatal("refused source report entered replay")
				}
			}
		})
	}
}

func TestHistorySourceFailureClassificationDoesNotRepeatStorageOrCancellation(t *testing.T) {
	for _, err := range []error{store.ErrHistoryReferenceInvalid, store.ErrHistorySourceUnavailable, store.ErrHistoryEvidenceLimit, store.ErrHistoryEvidenceChanged} {
		if !isHistoryAnswerCorrection(historySourceFailure(err)) {
			t.Fatalf("mechanical error is not repairable: %v", err)
		}
	}
	for _, err := range []error{context.Canceled, errors.New("database unavailable")} {
		failure := historySourceFailure(err)
		if isHistoryAnswerCorrection(failure) || !errors.Is(failure, err) {
			t.Fatalf("infrastructure failure invited tool repetition: %+v", failure)
		}
	}
	if isHistoryAnswerCorrection(nil) {
		t.Fatal("nil failure acquired a repair allowance")
	}
}

// Admission rechecks durable mode. A late explicit request still withholds
// prose prepared without it; a late incidental read in an ordinary task does
// not, because reading history never changes what the human asked for.
func TestLateExplicitReportRequestWithholdsPreparedProseButALateReadDoesNot(t *testing.T) {
	for _, marker := range []string{"query", "request"} {
		t.Run(marker, func(t *testing.T) {
			e, turn, _, b, set := historyAnswerFixture(t)
			res, failure := b.prepareHistoryAnswer(t.Context(), turn, set, historyCandidate(b, "Previously prepared free prose."), nil)
			if failure != nil {
				t.Fatal(failure)
			}
			row, failure := b.prepare(turn, res, nil)
			if failure != nil {
				t.Fatal(failure)
			}
			if marker == "query" {
				historyReadEvent(e, turn, "late-worker")
			} else {
				e.record(store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: "history_report_requested", AgentID: "manager"})
			}
			failure = e.admitScheduleAnswer(turn.ThreadID, turn.ID, nil, []store.Event{{Kind: "agent_message", AgentID: "manager", Text: res.Final}}, row.ID)
			if marker == "query" {
				if failure != nil {
					t.Fatalf("an incidental read withheld an ordinary answer: %+v", failure)
				}
				return
			}
			if failure == nil || failure.Code != "history_read_required" {
				t.Fatalf("late explicit history mode escaped admission: %+v", failure)
			}
		})
	}
}
