package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/compose"
)

type preparedScheduleReport struct {
	turn           *store.Turn
	findings       string
	quiet          bool
	events         []store.Event
	committedRearm []string
	historyReport  *pendingHistoryReport
}

func (e *Engine) reportScheduleContextJSON(ctx context.Context, threadID, turnID, args string) (string, error) {
	buffer, _ := ctx.Value(scheduleAnswerBufferContextKey{}).(*managerAnswerBuffer)
	if buffer != nil && compose.GetToolCallID(ctx) == "" {
		return reportAdmissionFailure(buffer, &scheduleAnswerError{Code: "report_identity_unavailable"}), nil
	}
	// Verification happens before lifecycle locks and every report side effect.
	// Even keep:false must not mutate the wait when a recadence was refused.
	if a, err := parseScheduleToolArgs(args); err == nil && a.NextInSPresent {
		if turn, err := e.store.GetTurn(turnID); err == nil && turn.ThreadID == threadID && turn.ScheduleContinue {
			if sch := e.scheduleForTurn(turn); sch != nil && sch.Kind == store.ScheduleThread && sch.ThreadID == threadID {
				if err := e.verifyThreadWakePolicy(ctx, threadID, turnID); err != nil {
					return threadWakeFailure(err), nil
				}
			}
		}
	}

	report, result := e.prepareScheduleReport(ctx, threadID, turnID, args)
	if report == nil {
		return result, nil
	}
	// Private test/adapter callers keep synchronous admission. They do not
	// retain the global goal/wake lock while an external model is responding.
	var evidence []store.Schedule
	var failure *scheduleAnswerError
	if report.findings != "" {
		evidence, failure = e.verifyScheduleAnswer(ctx, report.turn, []string{report.findings}, report.committedRearm...)
	}
	if failure == nil {
		var history *store.HistoryEvidence
		if report.historyReport != nil {
			history = &report.historyReport.Evidence
		}
		failure = e.admitScheduleAnswerWithHistory(threadID, turnID, evidence, report.events, history)
	}
	if failure != nil {
		return reportAdmissionFailure(nil, failure), nil
	}
	body, _ := json.Marshal(map[string]any{"ok": true, "quiet": report.quiet})
	return string(body), nil
}

// The action and its private publication intent are serialized together.
// A failed policy request may delay findings; it must never repeat a rearm.
func (e *Engine) prepareScheduleReport(ctx context.Context, threadID, turnID, args string) (*preparedScheduleReport, string) {
	e.goalWakeMu.Lock()
	defer e.goalWakeMu.Unlock()
	buffer, _ := ctx.Value(scheduleAnswerBufferContextKey{}).(*managerAnswerBuffer)
	callID := compose.GetToolCallID(ctx)
	candidateID := turnID + ":" + callID
	if buffer != nil {
		previous, err := e.store.GetPendingScheduleAnswer(candidateID)
		if err == nil {
			var saved pendingScheduleAnswer
			if previous.ThreadID != threadID || previous.TurnID != turnID || previous.Kind != "report" ||
				json.Unmarshal([]byte(previous.Payload), &saved) != nil || saved.Version != 1 || saved.ToolCallID != callID || saved.ToolResult == "" {
				return nil, reportAdmissionFailure(buffer, &scheduleAnswerError{Code: "answer_state_invalid"})
			}
			if previous.State == "rejected" {
				return nil, reportAdmissionFailure(buffer, &scheduleAnswerError{Code: "no_matching_schedule"})
			}
			return nil, saved.ToolResult
		}
		if !errors.Is(err, store.ErrNotFound) {
			return nil, reportAdmissionFailure(buffer, &scheduleAnswerError{Code: "answer_storage_failed", Cause: err})
		}
	}
	turn, err := e.store.GetTurn(turnID)
	if err != nil {
		return nil, scheduleToolFailure("%s", err.Error())
	}
	if turn.ThreadID != threadID {
		return nil, scheduleToolFailure("%s", "that turn is not on this conversation")
	}
	if !turn.ScheduleContinue {
		return nil, scheduleToolFailure("%s", "report_schedule is only for a scheduled check")
	}
	a, err := parseScheduleToolArgs(args)
	if err != nil {
		return nil, scheduleToolFailure("%s", err.Error())
	}
	if a.NextInS != 0 {
		if _, err := parseScheduleSpec(0, a.NextInS, "", e.scheduleMinInterval()); err != nil {
			return nil, scheduleToolFailure("%s", err.Error())
		}
	}
	findings := strings.TrimSpace(a.Findings)
	quiet := findings == ""
	var historyReport *pendingHistoryReport
	if !quiet {
		var scope *string
		if buffer != nil {
			scope = buffer.historyScope
		}
		var failure *scheduleAnswerError
		historyReport, failure = e.prepareHistoryReport(ctx, turn, scope, findings)
		if failure != nil {
			return nil, reportAdmissionFailure(buffer, failure)
		}
		if historyReport != nil {
			findings = historyReport.Rendered
		}
	}
	scope, err := e.wakeGoalScope(threadID, turnID)
	if err != nil {
		return nil, goalScheduleFailure(err)
	}
	if err := e.store.CheckGoalContinuation(scope, a.Keep == nil || *a.Keep); err != nil {
		return nil, goalScheduleFailure(err)
	}
	sch, err := e.scheduleForGoalReport(turn, scope)
	if err != nil {
		return nil, goalScheduleFailure(err)
	}
	actionCommitted := false
	if sch != nil {
		if a.Keep != nil && !*a.Keep {
			var cancelErr error
			var changed *store.Schedule
			if a.NextInSPresent && sch.Kind == store.ScheduleThread {
				changed, cancelErr = e.store.CancelScheduleForTurnWithWakePolicy(scope, sch.ID)
			} else if scope == nil || strings.TrimSpace(scope.Goal) == "" {
				changed, cancelErr = e.store.CancelSchedule(sch.ID)
			} else {
				changed, cancelErr = e.store.CancelScheduleForTurn(scope, sch.ID)
			}
			if err := cancelErr; err != nil {
				return nil, goalScheduleFailure(err)
			}
			if changed != nil {
				actionCommitted = true
				e.recordScheduleCancelled(changed)
				e.continueGoalAfterWakeStop(changed)
			}
		} else if a.NextInS != 0 {
			if msg, err := e.interruptedWakeRefusal(threadID); err != nil {
				return nil, scheduleToolFailure("%s", err.Error())
			} else if msg != "" {
				return nil, scheduleToolFailure("%s", msg)
			}
			if err := e.rearmScheduleIntervalForTurn(sch, a.NextInS, scope); err != nil {
				return nil, goalScheduleFailure(err)
			}
			actionCommitted = true
		}
	}
	payload, _ := json.Marshal(struct {
		Findings string `json:"findings"`
		Quiet    bool   `json:"quiet"`
	}{Findings: findings, Quiet: quiet})
	var committedRearm []string
	if sch != nil && a.NextInS > 0 && (a.Keep == nil || *a.Keep) {
		committedRearm = append(committedRearm, sch.ID)
	}

	report := &preparedScheduleReport{turn: turn, findings: findings, quiet: quiet, committedRearm: committedRearm, historyReport: historyReport,
		events: []store.Event{{ThreadID: threadID, TurnID: turnID, Kind: KindScheduleReport,
			AgentID: swarm.DefaultManagerID, Text: string(payload)}},
	}
	if buffer == nil {
		return report, ""
	}
	// The tool result acknowledges only the committed action and queued
	// report. Unverified findings remain solely in the private candidate.
	receipt, _ := json.Marshal(map[string]any{"ok": true, "quiet": quiet, "report_pending": true})
	candidate := pendingScheduleAnswer{AnswerID: candidateID, Events: report.events,
		CommittedRearm: committedRearm, ToolCallID: callID, ToolResult: string(receipt), HistoryReport: historyReport}
	if findings != "" {
		candidate.Text = []string{findings}
	}
	if _, failure := e.savePendingScheduleAnswer(turn, "report", candidateID, candidate); failure != nil {
		scheduleID := ""
		if sch != nil {
			scheduleID = sch.ID
		}
		return nil, reportSaveFailure(buffer, failure, actionCommitted, scheduleID)
	}
	return nil, string(receipt)
}

// Scheduling and private report storage are distinct commits. If the latter
// fails, a generic tool refusal would invite repeating an already-applied action.
func reportSaveFailure(buffer *managerAnswerBuffer, failure *scheduleAnswerError, actionCommitted bool, scheduleID string) string {
	buffer.reportStatus(failure)
	detail := "The report could not be saved for verification and was not published. "
	if actionCommitted {
		detail += "The schedule change was committed. Do not repeat the schedule change. "
	}
	detail += "Inspect the current schedule state before retrying the report."
	body := map[string]any{"ok": false, "code": failure.Code, "error": detail,
		"action_committed": actionCommitted, "report_published": false}
	if scheduleID != "" {
		body["schedule_id"] = scheduleID
	}
	out, _ := json.Marshal(body)
	return string(out)
}

func reportAdmissionFailure(buffer *managerAnswerBuffer, failure *scheduleAnswerError) string {
	if buffer != nil {
		buffer.reportStatus(failure)
	}
	code := "schedule_claim_unverified"
	if strings.HasPrefix(failure.Code, "history_") {
		code = failure.Code
	}
	body, _ := json.Marshal(map[string]any{"ok": false, "code": code, "error": failure.Error()})
	return string(body)
}
