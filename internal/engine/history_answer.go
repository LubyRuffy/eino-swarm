package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/schema"
)

const historyAnswerCorrectionCue = "The runtime withheld this candidate because the history report could not be validated. Continue this same turn using the strict history report JSON protocol and canonical source references. Read original history with query_history if it has not been read in this turn. Repair only the report or its source references; do not repeat completed tools, side effects, workers or scheduling changes. Do not supply historical fact text, excerpts, authorship or status yourself: the host renders complete original sources. Keep your proposed plan in the separate proposals section."

// This is private admission state, not a curated summary. Its canonical sources
// are checked again by the Store in the transaction that publishes the answer.
type pendingHistoryReport struct {
	Request  historyReportRequest  `json:"request"`
	Evidence store.HistoryEvidence `json:"evidence"`
	Rendered string                `json:"rendered"`
}

func isHistoryAnswerCorrection(failure *scheduleAnswerError) bool {
	return failure != nil && (failure.Code == "history_report_invalid" || failure.Code == "history_read_required" || failure.Code == "history_source_changed")
}

func historySourceFailure(err error) *scheduleAnswerError {
	code := "history_source_unavailable"
	if errors.Is(err, store.ErrHistoryReferenceInvalid) || errors.Is(err, store.ErrHistorySourceUnavailable) || errors.Is(err, store.ErrHistoryEvidenceLimit) {
		code = "history_report_invalid"
	}
	if errors.Is(err, store.ErrHistoryEvidenceChanged) {
		code = "history_source_changed"
	}
	return &scheduleAnswerError{Code: code, Cause: err}
}

func (e *Engine) historyAnswerMode(turn *store.Turn) (bool, bool, *scheduleAnswerError) {
	queried, err := e.store.TurnHasHistoryQuery(turn.ThreadID, turn.ID)
	if err != nil {
		return false, false, &scheduleAnswerError{Code: "history_state_unavailable", Cause: err}
	}
	requested, err := e.store.TurnHasHistoryReportRequest(turn.ThreadID, turn.ID)
	if err != nil {
		return false, false, &scheduleAnswerError{Code: "history_state_unavailable", Cause: err}
	}
	return queried || requested, queried, nil
}

func historyReportReferences(request historyReportRequest) []store.HistoryReference {
	refs := make([]store.HistoryReference, len(request.Sources))
	for i, source := range request.Sources {
		refs[i] = source.Source
	}
	return refs
}

func historyScopeFromTools(set *tools.Set) *string {
	if set != nil {
		for _, base := range set.Tools {
			if reader, ok := base.(*historyQueryTool); ok {
				scope := reader.context.ProjectID
				return &scope
			}
		}
	}
	return nil
}

func (b *managerAnswerBuffer) prepareHistoryAnswer(ctx context.Context, turn *store.Turn, set *tools.Set, res swarm.RunResult, runErr error) (swarm.RunResult, *scheduleAnswerError) {
	e := b.acc.engine
	// A tool-round boundary is not a completed answer. Keep its actual tools
	// and reasoning, but never publish intermediate free prose as a report.
	if isMaxIterations(runErr) && strings.TrimSpace(res.Final) == "" {
		required, _, failure := e.historyAnswerMode(turn)
		if failure != nil || !required {
			return res, failure
		}
		return b.replaceHistoryProse(res, ""), nil
	}
	report, failure := e.prepareHistoryReport(ctx, turn, historyScopeFromTools(set), res.Final)
	if failure != nil || report == nil {
		return res, failure
	}
	b.historyReport = report
	return b.replaceHistoryProse(res, report.Rendered), nil
}

// Final manager answers and report_schedule findings share the same deterministic
// protocol. The latter must validate before committing its scheduling action.
func (e *Engine) prepareHistoryReport(ctx context.Context, turn *store.Turn, scope *string, text string) (*pendingHistoryReport, *scheduleAnswerError) {
	required, queried, failure := e.historyAnswerMode(turn)
	if failure != nil || !required {
		return nil, failure
	}
	if !queried {
		return nil, &scheduleAnswerError{Code: "history_read_required"}
	}
	request, err := decodeHistoryReport(text, e.cfg.Tools.ToolResultLimit())
	if err != nil {
		return nil, &scheduleAnswerError{Code: "history_report_invalid", Cause: err}
	}
	if scope == nil {
		thread, err := e.store.GetThread(turn.ThreadID)
		if err != nil {
			return nil, &scheduleAnswerError{Code: "history_state_unavailable", Cause: err}
		}
		scope = &thread.ProjectID
	}
	sources, err := e.store.ReadHistorySources(ctx, *scope, historyReportReferences(request), e.cfg.Tools.ToolResultLimit())
	if err != nil {
		return nil, historySourceFailure(err)
	}
	rendered, err := renderHistoryReport(request, sources, e.cfg.Tools.ToolResultLimit())
	if err != nil {
		return nil, &scheduleAnswerError{Code: "history_report_invalid", Cause: err}
	}
	return &pendingHistoryReport{Request: request, Evidence: store.HistoryEvidence{ProjectID: *scope, Sources: sources}, Rendered: rendered}, nil
}

// Remove every prose representation belonging to this attempt, preserving
// reasoning and actual tool calls/results. Only the one host-rendered answer is
// eligible for publication and later Responses replay.
func (b *managerAnswerBuffer) replaceHistoryProse(res swarm.RunResult, rendered string) swarm.RunResult {
	res = b.reject(res)
	var message *schema.Message
	if rendered != "" {
		message = markPendingAnswer(schema.AssistantMessage(rendered, nil), b.id)
		res.Transcript = append(res.Transcript, message)
		res.Final = rendered
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var messageID string
	if len(b.complete) > 0 {
		messageID = b.complete[len(b.complete)-1].MessageID
	}
	b.complete, b.generated, b.pending = nil, nil, nil
	if message != nil {
		b.generated = []*schema.Message{message}
		b.complete = []swarm.Notification{{Kind: swarm.NotifyAgentMessage, AgentID: swarm.DefaultManagerID, Role: swarm.DefaultManagerID, MessageID: messageID, Text: rendered}}
	}
	return res
}

func validateStoredHistoryReport(candidate *pendingScheduleAnswer, maxChars int) error {
	report := candidate.HistoryReport
	if report == nil {
		return nil
	}
	rendered, err := renderHistoryReport(report.Request, report.Evidence.Sources, maxChars)
	if err != nil {
		return err
	}
	if rendered != report.Rendered || !reflect.DeepEqual(candidate.Text, []string{rendered}) || len(candidate.Events) != 1 {
		return fmt.Errorf("private history report rendering mismatch")
	}
	if candidate.ToolCallID != "" {
		var findings struct {
			Findings string `json:"findings"`
			Quiet    bool   `json:"quiet"`
		}
		if candidate.Final != "" || len(candidate.Transcript) != 0 || candidate.Events[0].Kind != KindScheduleReport || json.Unmarshal([]byte(candidate.Events[0].Text), &findings) != nil || findings.Findings != rendered || findings.Quiet {
			return fmt.Errorf("private history schedule report rendering mismatch")
		}
		return nil
	}
	if candidate.Final != rendered || candidate.Events[0].Text != rendered {
		return fmt.Errorf("private history report final rendering mismatch")
	}
	prose := 0
	for _, message := range candidate.Transcript {
		if message == nil || message.Extra[pendingScheduleAnswerKey] != candidate.AnswerID {
			continue
		}
		if len(message.MultiContent) != 0 || len(message.AssistantGenMultiContent) != 0 {
			return fmt.Errorf("private history report retained generated multimodal prose")
		}
		if message.Content != "" {
			prose++
			if message.Content != rendered {
				return fmt.Errorf("private history report retained unrendered prose")
			}
		}
		clean := withoutScheduleAnswerProse(message)
		before, beforeErr := json.Marshal(message.Extra["responses_output"])
		after, afterErr := json.Marshal(clean.Extra["responses_output"])
		if beforeErr != nil || afterErr != nil || string(before) != string(after) {
			return fmt.Errorf("private history report retained raw Responses prose")
		}
	}
	if prose != 1 {
		return fmt.Errorf("private history report has no unique rendered transcript answer")
	}
	return nil
}

func (e *Engine) verifyPendingHistoryAnswer(ctx context.Context, turn *store.Turn, candidate *pendingScheduleAnswer) *scheduleAnswerError {
	required, queried, failure := e.historyAnswerMode(turn)
	if failure != nil || !required && candidate.HistoryReport == nil {
		return failure
	}
	if candidate.MaxIterations && candidate.Final == "" && candidate.HistoryReport == nil {
		return nil
	}
	if !queried {
		return &scheduleAnswerError{Code: "history_read_required"}
	}
	if candidate.HistoryReport == nil {
		return &scheduleAnswerError{Code: "history_report_invalid", Cause: fmt.Errorf("missing structured history report")}
	}
	if err := validateStoredHistoryReport(candidate, e.cfg.Tools.ToolResultLimit()); err != nil {
		return &scheduleAnswerError{Code: "history_report_invalid", Cause: err}
	}
	report := candidate.HistoryReport
	sources, err := e.store.ReadHistorySources(ctx, report.Evidence.ProjectID, historyReportReferences(report.Request), e.cfg.Tools.ToolResultLimit())
	if err != nil {
		return historySourceFailure(err)
	}
	if !sameHistorySourceSnapshots(sources, report.Evidence.Sources) {
		return historySourceFailure(store.ErrHistoryEvidenceChanged)
	}
	return nil
}

// The private JSON round trip intentionally treats omitted/empty image slices
// equally. Normalize timestamp locations too, while retaining every serialized
// source field rather than duplicating the Store's field-by-field comparator.
func sameHistorySourceSnapshots(a, b []store.HistorySource) bool {
	canonical := func(sources []store.HistorySource) ([]byte, error) {
		copy := make([]store.HistorySource, len(sources))
		for i, source := range sources {
			copy[i] = source
			copy[i].CreatedAt = source.CreatedAt.UTC()
		}
		return json.Marshal(copy)
	}
	left, leftErr := canonical(a)
	right, rightErr := canonical(b)
	return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
}
