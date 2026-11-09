package engine

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	einoexec "github.com/LubyRuffy/eino-tools/exec"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const managedExecResumeCue = "A command session had no confirmed outcome in the last recorded result before restart. The old session ID is unavailable. Inspect the workspace or an external receipt to establish its actual effects before deciding whether any command should run again."

const managedExecBackgroundInstruction = `An exec response with status running means this turn still owns that process.
For a command expected to finish soon whose result you need immediately, use
yield_after_ms to observe it in the initial exec call. This only changes when
exec returns control. For pipelines whose earlier failures matter, use
pipefail so a successful output filter cannot conceal them.

Continue only independent work. Use exec_status with this turn's session ID
to see its state and recent output when you need progress. If the next step needs
a specific command's result, call wait_exec with that session ID. New output
renews the observation window. An observation timeout leaves the process
running. A positive timeout_ms on the original exec is a hard process deadline.
When observing an ID again, pass its last revision as after_revision to request
changes only. After an idle wait or an unchanged status, use a longer wait for
quiet work or continue independent work. An immediate status check repeats the
same evidence.

When any owned result would let you continue, end your current response without
claiming completion. The engine waits inside this turn and
wakes you as soon as an owned command settles. Read the terminal result under
its original call ID before taking dependent action. When an observation gives
output_from_tool_call_id, read the output in that existing result. Use these waits
in place of repeated status checks, restarting commands, or scheduled checks.
Use command sleep when an actual elapsed delay is part of the task.`

const adaptiveExecObservationOption = "exec_adaptive_observation"

const managedExecCompletionCue = "A background exec has settled. Continue the original task using the terminal result for its original tool call. Check the outcomes before reporting completion."

// A process can exit while the manager does independent work. Refresh the
// next request after compaction, without inserting a separate model round.
type managedExecDelivery struct {
	adk.BaseChatModelAgentMiddleware
	sessions *tools.ManagedExecSessions
	adaptive bool
	reg      *swarm.Registry
	acc      *accumulator
}

func (m *managedExecDelivery) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if ctx.Err() != nil {
		return ctx, state, nil
	}
	if m.adaptive && len(state.Messages) > 0 {
		// A steer already drained into this input also takes precedence.
		last := state.Messages[len(state.Messages)-1]
		if last != nil && last.Role == schema.Tool && m.sessions.Pending() &&
			!m.reg.HasPendingSteers() && !m.hasRunningWorker() {
			observeCtx, cancel := context.WithCancel(ctx)
			waitCtx, release := m.reg.WithEpoch(observeCtx)
			err := m.sessions.AwaitAdaptiveObservation(waitCtx)
			if err == nil {
				err = waitCtx.Err()
			}
			release()
			cancel()
			if err != nil {
				return ctx, state, err
			}
		}
	}
	m.sessions.AdvanceModelRequest()
	receipts, err := m.sessions.TakeReady()
	if err != nil || len(receipts) == 0 {
		return ctx, state, err
	}
	next := *state
	next.Messages = spliceManagedExecReceipts(state.Messages, receipts)
	// The registry recorded the input before engine middleware rewrote it.
	m.reg.SetHistory(next.Messages)
	publishManagedExecReceipts(m.acc, receipts)
	return ctx, &next, nil
}

// AfterModelRewriteState sees the complete tool batch before ADK fans it out.
func (m *managedExecDelivery) AfterModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if !m.adaptive || ctx.Err() != nil || len(state.Messages) == 0 {
		return ctx, state, nil
	}
	reply := state.Messages[len(state.Messages)-1]
	if reply == nil || reply.Role != schema.Assistant || len(reply.ToolCalls) != 1 {
		return ctx, state, nil
	}
	call := reply.ToolCalls[0]
	if (call.Function.Name == einoexec.ToolName || call.Function.Name == "wait_exec") && !m.hasRunningWorker() {
		m.sessions.NoteStandaloneModelCall(call.ID, call.Function.Name == "wait_exec")
	}
	return ctx, state, nil
}

func (m *managedExecDelivery) hasRunningWorker() bool {
	return slices.ContainsFunc(m.reg.Progress(), func(worker swarm.AgentProgress) bool {
		return worker.Running
	})
}

// Compact only the model request. The next request or a resumed turn may no
// longer contain the original result, so canonical tool messages keep their
// independent output and remain available to compaction and handoff.
func (m *managedExecDelivery) WrapModel(_ context.Context, inner model.BaseModel[*schema.Message], _ *adk.ModelContext) (model.BaseModel[*schema.Message], error) {
	return &managedExecOutputModel{BaseChatModel: inner}, nil
}

type managedExecOutputModel struct {
	model.BaseChatModel
}

func (m *managedExecOutputModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return m.BaseChatModel.Generate(ctx, referenceManagedExecOutput(input), opts...)
}

func (m *managedExecOutputModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return m.BaseChatModel.Stream(ctx, referenceManagedExecOutput(input), opts...)
}

type managedExecOutputEvidence struct {
	SessionID      string `json:"session_id"`
	OriginalCallID string `json:"original_call_id"`
	Status         string `json:"status"`
	ExitCode       *int   `json:"exit_code"`
	Revision       uint64 `json:"revision"`
	Stdout         string `json:"stdout"`
	Stderr         string `json:"stderr"`
	Error          string `json:"error"`
}

func decodeManagedExecOutput(text string) (managedExecOutputEvidence, bool) {
	var result managedExecOutputEvidence
	if json.Unmarshal([]byte(text), &result) != nil ||
		!strings.HasPrefix(result.SessionID, "exec-") || result.Stdout == "" && result.Stderr == "" {
		return result, false
	}
	switch result.Status {
	case "running":
		return result, result.ExitCode == nil && result.Revision != 0
	case "exited", "timed_out", "cancelled", "stalled", "cleanup_failed":
		return result, result.ExitCode != nil
	default:
		return result, false
	}
}

func (result managedExecOutputEvidence) matches(other managedExecOutputEvidence) bool {
	if result.Status != other.Status || result.Stdout != other.Stdout ||
		result.Stderr != other.Stderr || result.Error != other.Error {
		return false
	}
	if result.Status == "running" {
		return result.Revision == other.Revision && result.OriginalCallID == other.OriginalCallID
	}
	return result.ExitCode != nil && other.ExitCode != nil && *result.ExitCode == *other.ExitCode
}

func referenceManagedExecOutput(input []*schema.Message) []*schema.Message {
	calls := make(map[string]string)
	hasObserver := false
	for _, message := range input {
		if message == nil || message.Role != schema.Assistant {
			continue
		}
		for _, call := range message.ToolCalls {
			calls[call.ID] = call.Function.Name
			hasObserver = hasObserver || call.Function.Name == "wait_exec" || call.Function.Name == "exec_status"
		}
	}
	if !hasObserver {
		return input
	}

	type execOutputSource struct {
		managedExecOutputEvidence
		callID string
	}
	originals := make(map[string]execOutputSource)
	for _, message := range input {
		if message == nil || message.Role != schema.Tool || calls[message.ToolCallID] != einoexec.ToolName {
			continue
		}
		if result, ok := decodeManagedExecOutput(message.Content); ok && result.Status != "running" {
			originals[result.SessionID] = execOutputSource{managedExecOutputEvidence: result, callID: message.ToolCallID}
		}
	}
	running := make(map[string]execOutputSource)
	var projected []*schema.Message
	for i, message := range input {
		if message == nil || message.Role != schema.Tool {
			continue
		}
		if name := calls[message.ToolCallID]; name != "wait_exec" && name != "exec_status" {
			continue
		}
		result, ok := decodeManagedExecOutput(message.Content)
		if !ok {
			continue
		}
		source := originals[result.SessionID]
		if result.Status == "running" {
			source = running[result.SessionID]
			if !result.matches(source.managedExecOutputEvidence) {
				running[result.SessionID] = execOutputSource{managedExecOutputEvidence: result, callID: message.ToolCallID}
				continue
			}
		} else if result.OriginalCallID != "" && result.OriginalCallID != source.callID {
			continue
		}
		if source.callID == "" || source.callID == message.ToolCallID || !result.matches(source.managedExecOutputEvidence) {
			continue
		}
		content, ok := managedExecOutputReference(message.Content, source.callID)
		if !ok {
			continue
		}
		if projected == nil {
			projected = slices.Clone(input)
		}
		projectedMessage := *message
		projectedMessage.Content = content
		projected[i] = &projectedMessage
	}
	if projected == nil {
		return input
	}
	return projected
}

func managedExecOutputReference(content, callID string) (string, bool) {
	// The evidence decoder already checked the JSON object and known fields.
	var result map[string]json.RawMessage
	if json.Unmarshal([]byte(content), &result) != nil {
		return "", false
	}
	delete(result, "stdout")
	delete(result, "stderr")
	id, _ := json.Marshal(callID) // JSON strings are always encodable.
	result["output_from_tool_call_id"] = id
	projected, err := json.Marshal(result)
	return string(projected), err == nil
}

func publishManagedExecReceipts(acc *accumulator, receipts []tools.ManagedExecReceipt) {
	for _, receipt := range receipts {
		acc.onNotify(swarm.Notification{Kind: swarm.NotifyToolResult, AgentID: swarm.DefaultManagerID,
			Role: "manager", ToolCallID: receipt.CallID, Text: receipt.Result})
	}
}

// A human steer can wake the manager while the turn still owns its processes.
// Stop and handoff cancel the parent instead, so the owner joins them on exit.
func waitForManagedExec(ctx context.Context, reg *swarm.Registry, sessions *tools.ManagedExecSessions) ([]tools.ManagedExecReceipt, error) {
	waitCtx, release := reg.WithEpoch(ctx)
	defer release()
	if reg.TakePreempt() {
		return nil, ctx.Err()
	}
	receipts, err := sessions.Wait(waitCtx)
	if ctx.Err() != nil {
		return receipts, ctx.Err()
	}
	if errors.Is(err, context.Canceled) && waitCtx.Err() != nil {
		reg.TakePreempt()
		return nil, nil
	}
	return receipts, err
}

// Replace the provisional result without sending two outputs for the same
// tool call. If compaction removed its pair, restore the authentic original
// function call so command output retains its Tool provenance on the wire.
func spliceManagedExecReceipts(messages []adk.Message, receipts []tools.ManagedExecReceipt) []adk.Message {
	if len(receipts) == 0 {
		return messages
	}
	calls := make(map[string]bool)
	for _, message := range messages {
		if message != nil && message.Role == schema.Assistant {
			for _, call := range message.ToolCalls {
				calls[call.ID] = true
			}
		}
	}
	missing := make(map[string]bool)
	results := make([]store.Event, 0, len(receipts))
	for _, receipt := range receipts {
		results = append(results, store.Event{ToolCallID: receipt.CallID, Text: receipt.Result})
		if !calls[receipt.CallID] {
			missing[receipt.CallID] = true
		}
	}
	if len(missing) > 0 {
		retained := make([]adk.Message, 0, len(messages)+len(missing))
		for _, message := range messages {
			if message == nil || message.Role != schema.Tool || !missing[message.ToolCallID] {
				retained = append(retained, message)
			}
		}
		for _, receipt := range receipts {
			if missing[receipt.CallID] {
				retained = append(retained, schema.AssistantMessage("", []schema.ToolCall{{
					ID: receipt.CallID, Type: "function", Function: schema.FunctionCall{Name: einoexec.ToolName, Arguments: receipt.Arguments},
				}}))
			}
		}
		messages = retained
	}
	return stitchManagerToolResults(messages, results)
}

// A running tool response exists only to return control to the model. A new
// engine cannot own its process. Keep the original call open in the indexed
// recovery read so a real terminal event can still settle it.
func markBackgroundExecUnknown(messages []*schema.Message) {
	var execCalls map[string]bool
	for i, message := range messages {
		if message == nil {
			continue
		}
		if message.Role == schema.Assistant {
			for _, call := range message.ToolCalls {
				if call.Function.Name == einoexec.ToolName {
					if execCalls == nil {
						execCalls = make(map[string]bool)
					}
					execCalls[call.ID] = true
				}
			}
		}
		if message.Role != schema.Tool || !execCalls[message.ToolCallID] {
			continue
		}
		var result struct {
			Status    string `json:"status"`
			SessionID string `json:"session_id"`
			ExitCode  *int   `json:"exit_code"`
		}
		if json.Unmarshal([]byte(message.Content), &result) == nil && result.Status == "running" && result.ExitCode == nil && strings.HasPrefix(result.SessionID, "exec-") {
			unknown := *message
			unknown.Content = swarm.HandoffUnknownToolResult
			messages[i] = &unknown
		}
	}
}

// Only a fresh, ordinary manager may route exec through the experiment. Its
// tools begin with the workspace toolset in the same order, so its copy can
// replace just this slot without touching the worker toolset.
func (rt *runtime) managedExecToolIndex(turn *store.Turn, th *store.Thread, toolset *tools.Set, resumed bool) int {
	if resumed || th == nil || th.PlanMode || turn.NuwaReadOnly || hasOpenGoal(th) ||
		turn.GoalContinue || turn.ScheduleContinue || turn.ScheduleRunID != "" || toolset == nil {
		return -1
	}
	cfg := rt.engine.cfg.Snapshot().Tools
	if !cfg.IsEnabled(tools.ExecSessionToolName) || cfg.IsDisabled(tools.ExecSessionToolName) ||
		!tools.ManagedExecSupported() || rt.recordingPlanImplement() {
		return -1
	}
	return slices.Index(toolset.Names, einoexec.ToolName)
}

// Earlier versions of the experiment exposed running sessions as completed
// tool responses, which the ordinary orphaned-call scan cannot identify.
func hasUnfinishedManagedExec(events []store.Event) bool {
	pendingStarts := make(map[string]bool)
	terminalSessions := make(map[string]bool)
	for _, event := range events {
		if !isManagerAgent(event.AgentID) || event.ToolCallID == "" {
			continue
		}
		switch event.Kind {
		case swarm.NotifyToolCall.String():
			name, args := splitToolCallText(event.Text)
			_, seen := pendingStarts[event.ToolCallID]
			if name != tools.ExecSessionToolName || seen {
				continue
			}
			var request struct {
				SessionID string `json:"session_id"`
			}
			pendingStarts[event.ToolCallID] = json.Unmarshal([]byte(args), &request) != nil || request.SessionID == ""
		case swarm.NotifyToolResult.String():
			if _, seen := pendingStarts[event.ToolCallID]; !seen {
				continue
			}
			var reply struct {
				SessionID string `json:"session_id"`
				Status    string `json:"status"`
			}
			if json.Unmarshal([]byte(event.Text), &reply) != nil || reply.SessionID == "" && reply.Status != "error" {
				// A synthetic or incomplete receipt cannot settle a start.
				continue
			}
			pendingStarts[event.ToolCallID] = false
			if reply.SessionID != "" {
				terminal := reply.Status == "exited" || reply.Status == "timed_out" || reply.Status == "cancelled"
				// Concurrent observations can deliver a stale running
				// receipt after a confirmed terminal receipt.
				terminalSessions[reply.SessionID] = terminalSessions[reply.SessionID] || terminal
			}
		}
	}
	for _, pending := range pendingStarts {
		if pending {
			return true
		}
	}
	for _, terminal := range terminalSessions {
		if !terminal {
			return true
		}
	}
	return false
}
