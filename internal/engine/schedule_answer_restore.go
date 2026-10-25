package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/tool"
)

func (e *Engine) restorePendingScheduleAnswer(turn *store.Turn) (*store.PendingScheduleAnswer, *pendingScheduleAnswer, *scheduleAnswerError) {
	rows, err := e.store.ListPendingScheduleAnswers(turn.ID)
	if err != nil {
		return nil, nil, &scheduleAnswerError{Code: "answer_restore_unavailable", Cause: err}
	}
	for i := len(rows) - 1; i >= 0; i-- {
		row := &rows[i]
		if row.Kind != "answer" {
			continue
		}
		// A rejected latest output supersedes every earlier output, including
		// accepted ones. Recovery must not search backwards for success.
		if row.State == "rejected" {
			return nil, nil, nil
		}
		payload, failure := decodeRestoredScheduleAnswer(turn, row)
		if failure != nil {
			return nil, nil, failure
		}
		return row, payload, nil
	}
	return nil, nil, nil
}

func decodeRestoredScheduleAnswer(turn *store.Turn, row *store.PendingScheduleAnswer) (*pendingScheduleAnswer, *scheduleAnswerError) {
	invalid := func(err error) (*pendingScheduleAnswer, *scheduleAnswerError) {
		return nil, &scheduleAnswerError{Code: "answer_restore_invalid", Cause: err}
	}
	decision := row.State == "pending" || row.State == "accepted" || row.Kind == "report" && row.State == "rejected"
	if row.ThreadID != turn.ThreadID || row.TurnID != turn.ID || !decision {
		return invalid(fmt.Errorf("private answer owner or decision mismatch"))
	}
	var payload pendingScheduleAnswer
	decoder := json.NewDecoder(strings.NewReader(row.Payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return invalid(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return invalid(fmt.Errorf("trailing private answer payload"))
	}
	if payload.Version != 1 || payload.AnswerID == "" || payload.AnswerID != row.ID {
		return invalid(fmt.Errorf("unsupported private answer version or identity"))
	}
	for _, message := range payload.Transcript {
		if message != nil && message.Extra[pendingScheduleAnswerKey] != nil && message.Extra[pendingScheduleAnswerKey] != row.ID {
			return invalid(fmt.Errorf("private answer transcript identity mismatch"))
		}
	}
	kind := swarm.NotifyAgentMessage.String()
	if row.Kind == "report" {
		kind = KindScheduleReport
	}
	for _, event := range payload.Events {
		if event.ThreadID != turn.ThreadID || event.TurnID != turn.ID || !isManagerAgent(event.AgentID) || event.Kind != kind {
			return invalid(fmt.Errorf("private answer event owner mismatch"))
		}
	}
	return &payload, nil
}

// report_schedule may commit its rearm before findings classification. Restore
// that original tool result before generic orphan cleanup can invent unknown.
func (e *Engine) restorePendingScheduleReports(turn *store.Turn, snapshot *restoreSnapshot) error {
	rows, err := e.store.ListPendingScheduleAnswers(turn.ID)
	if err != nil {
		return fmt.Errorf("read pending report receipts: %w", err)
	}
	events, err := e.restoreTurnEvents(turn.ID, snapshot)
	if err != nil {
		return fmt.Errorf("read original report calls: %w", err)
	}
	for i := range rows {
		row := &rows[i]
		if row.Kind != "report" {
			continue
		}
		payload, failure := decodeRestoredScheduleAnswer(turn, row)
		if failure != nil {
			return failure
		}
		if payload.ToolCallID == "" || payload.ToolResult == "" || !json.Valid([]byte(payload.ToolResult)) {
			return &scheduleAnswerError{Code: "answer_restore_invalid", Cause: fmt.Errorf("private report is missing its original result")}
		}
		var call *store.Event
		var result *store.Event
		for j := range events {
			event := &events[j]
			if !isManagerAgent(event.AgentID) || event.ToolCallID != payload.ToolCallID {
				continue
			}
			if event.Kind == swarm.NotifyToolCall.String() && toolCallName(event.Text) == ToolReportSchedule {
				call = event
			}
			if event.Kind == swarm.NotifyToolResult.String() {
				result = event
			}
		}
		if call == nil {
			return &scheduleAnswerError{Code: "answer_restore_invalid", Cause: fmt.Errorf("private report has no matching tool call")}
		}
		placeholder := result != nil && (result.Text == swarm.HandoffUnknownToolResult || result.Text == resumeToolStopped || result.Text == swarm.InterruptedToolResult || result.Text == context.Canceled.Error() && result.Err == context.Canceled.Error())
		if result == nil || placeholder {
			receipt := store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: swarm.NotifyToolResult.String(), AgentID: call.AgentID, Role: call.Role, ToolCallID: payload.ToolCallID, Text: payload.ToolResult}
			e.recordMu.Lock()
			_, dropped := e.droppedTurns[turn.ID]
			if !dropped {
				err = e.store.AppendEvent(&receipt)
			}
			e.recordMu.Unlock()
			if dropped {
				return &scheduleAnswerError{Code: "answer_owner_deleted"}
			}
			if err != nil {
				return fmt.Errorf("save recovered report result: %w", err)
			}
			e.broadcast(receipt)
			events = append(events, receipt)
			result = &events[len(events)-1]
		}
		if snapshot != nil {
			present := false
			for _, event := range snapshot.events {
				present = present || event.Seq == result.Seq
			}
			if !present {
				snapshot.events = append(snapshot.events, *result)
			}
		}
	}
	return nil
}

func (rt *runtime) restorePendingScheduleWorkers(ctx context.Context, reg *swarm.Registry) error {
	running, finished := rt.takeWorkerRestore()
	for _, worker := range finished {
		if err := reg.PlantFinished(worker); err != nil {
			return err
		}
	}
	workerTools := append([]tool.BaseTool(nil), reg.SubAgentTools...)
	workerTools = append(workerTools, reg.SendTool())
	for _, worker := range running {
		if _, err := reg.Restore(ctx, worker, reg.ModelBuilder, workerTools...); err != nil {
			return err
		}
	}
	return nil
}
