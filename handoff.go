package swarm

import (
	"context"
	"errors"
	"io"
	"sort"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// ErrHandoff means work was paused for a process replacement, not completed.
var ErrHandoff = errors.New("swarm: preparing for restart; work remains resumable")

const handoffInstruction = "The host is restarting. Do not start tools or new work. Give a short handoff with confirmed findings, unfinished work, and the next verification step. Distinguish confirmed tool results from unknown external effects. This handoff pauses the task; it does not complete it."

const handoffToolResult = `{"error":"host is restarting; this tool was not started; summarize the task for resumption"}`

// HandoffUnknownToolResult closes replay protocol without claiming that an
// interrupted external operation failed or succeeded.
const HandoffUnknownToolResult = `{"code":"outcome_unknown","message":"The previous process stopped before this tool result was confirmed. Inspect its external effects before deciding whether to repeat it."}`

// WorkerToolCheckpoint records a tool admitted before handoff whose result is
// still unknown. It is evidence to inspect on recovery, never a replay queue.
type WorkerToolCheckpoint struct {
	Name      string `json:"name"`
	CallID    string `json:"call_id"`
	Arguments string `json:"arguments"`
}

// WorkerCheckpoint separates confirmed completion from an optional handoff
// summary. History and Inbox retain the worker's actual input, not a rewritten
// task reconstructed from its summary.
type WorkerCheckpoint struct {
	ID              string                 `json:"id"`
	Role            string                 `json:"role"`
	Task            string                 `json:"task"`
	Instruction     string                 `json:"instruction"`
	History         []*schema.Message      `json:"history,omitempty"`
	Inbox           []string               `json:"inbox,omitempty"`
	TimeoutFailures int                    `json:"timeout_failures"`
	Finished        bool                   `json:"finished"`
	Result          string                 `json:"result,omitempty"`
	Err             string                 `json:"err,omitempty"`
	Summary         string                 `json:"summary,omitempty"`
	InFlight        []WorkerToolCheckpoint `json:"in_flight,omitempty"`
}

type workerHandoff struct {
	task, instruction string
	requested, read   bool
	cancelled         bool
	summary           string
	inFlight          map[string]WorkerToolCheckpoint
	restoreHistory    []*schema.Message
}

// BeginHandoff stops admission of new work. Workers already in a model or
// tool call may finish it; their next normal model round requests a handoff.
// The host must persist SnapshotWorkers before waiting for that best effort.
func (r *Registry) BeginHandoff() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handingOff = true
	for _, h := range r.agents {
		h.mu.Lock()
		if h.finished.IsZero() {
			h.handoff.requested = true
		}
		h.mu.Unlock()
	}
}

// PrepareRestart shares one deadline across all workers, including models or
// tools that ignore cancellation. It never launches extra summarizer models.
// A caller can cancel ctx immediately when only a deterministic checkpoint is
// wanted. The returned unfinished tools require verification after restart.
func (r *Registry) PrepareRestart(ctx context.Context) []WorkerCheckpoint {
	r.BeginHandoff()
	r.mu.Lock()
	handles := make([]*Handle, 0, len(r.agents))
	for _, h := range r.agents {
		handles = append(handles, h)
	}
	r.mu.Unlock()
	for _, h := range handles {
		select {
		case <-h.Done():
		case <-ctx.Done():
			for _, pending := range handles {
				pending.stopForHandoff()
			}
			return r.SnapshotWorkers()
		}
	}
	return r.SnapshotWorkers()
}

// SnapshotWorkers does not consume inboxes or wait on model/tool execution.
// Completed workers retained in the archive remain available after Stats.
func (r *Registry) SnapshotWorkers() []WorkerCheckpoint {
	r.mu.Lock()
	workers := make(map[string]WorkerCheckpoint, len(r.agents)+len(r.past))
	for id, p := range r.past {
		workers[id] = cloneWorkerCheckpoint(p.checkpoint)
	}
	handles := make([]*Handle, 0, len(r.agents))
	for _, h := range r.agents {
		handles = append(handles, h)
	}
	r.mu.Unlock()
	for _, h := range handles {
		h.mu.Lock()
		workers[h.ID] = h.checkpointLocked()
		h.mu.Unlock()
	}
	out := make([]WorkerCheckpoint, 0, len(workers))
	for _, w := range workers {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ManagerHistory and ManagerInbox are non-consuming copies for a durable
// restart checkpoint. Peer messages keep their original role and metadata.
func (r *Registry) ManagerHistory() []*schema.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	msgs := cloneMessages(r.hist)
	for _, msg := range msgs {
		if msg.Role == schema.Tool && r.handoffManagerTools[msg.ToolCallID] {
			msg.Content = HandoffUnknownToolResult
		}
	}
	return msgs
}

func (r *Registry) ManagerInbox() []*schema.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneMessages(r.mgrInbox)
}

func (r *Registry) isHandingOff() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.handingOff
}

func (h *Handle) stopForHandoff() {
	h.mu.Lock()
	if !h.finished.IsZero() {
		h.mu.Unlock()
		return
	}
	h.handoff.cancelled = true
	cancel := h.cancel
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (h *Handle) checkpointLocked() WorkerCheckpoint {
	w := WorkerCheckpoint{ID: h.ID, Role: h.Role, Task: h.handoff.task,
		Instruction: h.handoff.instruction, History: cloneMessages(h.history),
		Inbox:           append(append([]string(nil), h.undelivered...), h.inbox...),
		TimeoutFailures: h.timeoutFailures, Summary: h.handoff.summary,
		Finished: !h.finished.IsZero() && !errors.Is(h.err, ErrHandoff)}
	for _, msg := range w.History {
		if _, unknown := h.handoff.inFlight[msg.ToolCallID]; msg.Role == schema.Tool && h.handoff.requested && unknown {
			msg.Content = HandoffUnknownToolResult
		}
	}
	if w.Finished {
		w.Result = h.result
		if h.err != nil {
			w.Err = h.err.Error()
		}
	}
	for _, call := range h.handoff.inFlight {
		w.InFlight = append(w.InFlight, call)
	}
	sort.Slice(w.InFlight, func(i, j int) bool { return w.InFlight[i].CallID < w.InFlight[j].CallID })
	return w
}

func cloneWorkerCheckpoint(w WorkerCheckpoint) WorkerCheckpoint {
	w.History = cloneMessages(w.History)
	w.Inbox = append([]string(nil), w.Inbox...)
	w.InFlight = append([]WorkerToolCheckpoint(nil), w.InFlight...)
	return w
}

func (h *Handle) injectHandoff(state *adk.ChatModelAgentState) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.handoff.requested && !h.handoff.read {
		state.Messages = append(state.Messages, schema.UserMessage(handoffInstruction))
		h.handoff.read = true
	}
}

func (r *Registry) beginWorkerTool(h *Handle, name, callID, arguments string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.handingOff {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.handoff.inFlight == nil {
		h.handoff.inFlight = map[string]WorkerToolCheckpoint{}
	}
	h.handoff.inFlight[callID] = WorkerToolCheckpoint{Name: name, CallID: callID, Arguments: arguments}
	return true
}

func (h *Handle) finishTool(name, callID, result string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Cancellation can race an external commit. Retain that call as unknown
	// even if the local runner returned an error or partial stdout.
	if err == nil {
		delete(h.handoff.inFlight, callID)
	}
	if err != nil && result == "" {
		result = err.Error()
	}
	if h.handoff.requested && err != nil {
		result = HandoffUnknownToolResult
	}
	h.history = append(h.history, schema.ToolMessage(result, callID, schema.WithToolName(name)))
}

func checkpointToolError(ctx context.Context, err error) error {
	if err != nil {
		return err
	}
	return ctx.Err()
}

func (r *Registry) checkpointToolResult(agentID, callID, result string) string {
	r.mu.Lock()
	managerUnknown := agentID == DefaultManagerID && r.handoffManagerTools[callID]
	h := r.agents[agentID]
	if h == nil {
		h = r.closingWorkers[agentID]
	}
	r.mu.Unlock()
	if managerUnknown {
		return HandoffUnknownToolResult
	}
	if h == nil {
		return result
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, unknown := h.handoff.inFlight[callID]; h.handoff.requested && unknown {
		return HandoffUnknownToolResult
	}
	return result
}

func (r *Registry) checkpointManagerToolResult(ctx context.Context, callID, result string) string {
	if ctx.Err() == nil {
		return result
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.handingOff {
		return result
	}
	if r.handoffManagerTools == nil {
		r.handoffManagerTools = map[string]bool{}
	}
	r.handoffManagerTools[callID] = true
	return HandoffUnknownToolResult
}

func (in *Injector) WrapStreamableToolCall(_ context.Context,
	endpoint adk.StreamableToolCallEndpoint, tc *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, args string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		if !in.reg.beginWorkerTool(in.handle, tc.Name, tc.CallID, args) {
			in.handle.finishTool(tc.Name, tc.CallID, handoffToolResult, nil)
			return schema.StreamReaderFromArray([]string{handoffToolResult}), nil
		}
		ctx = in.reg.bindToolOutput(ctx, in.handle.ID, in.handle.Role, tc)
		sr, err := endpoint(ctx, args, opts...)
		if err != nil {
			in.handle.finishTool(tc.Name, tc.CallID, "", err)
			return nil, err
		}
		var result strings.Builder
		failed := false
		return schema.StreamReaderWithConvert(sr, func(chunk string) (string, error) {
			result.WriteString(chunk)
			return chunk, nil
		}, schema.WithErrWrapper(func(err error) error {
			failed = true
			in.handle.finishTool(tc.Name, tc.CallID, result.String(), err)
			return err
		}), schema.WithOnEOF(func() (any, error) {
			if !failed {
				in.handle.finishTool(tc.Name, tc.CallID, result.String(), checkpointToolError(ctx, nil))
			}
			return nil, io.EOF
		})), nil
	}, nil
}

func restoredWorkerMessages(task string, history []*schema.Message) []*schema.Message {
	msgs := make([]*schema.Message, 0, len(history))
	resuming := false
	open := map[string]schema.ToolCall{}
	seal := func() {
		ids := make([]string, 0, len(open))
		for id := range open {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			msgs = append(msgs, schema.ToolMessage(HandoffUnknownToolResult, id, schema.WithToolName(open[id].Function.Name)))
			delete(open, id)
		}
	}
	for _, msg := range cloneMessages(history) {
		if msg.Content == handoffInstruction {
			resuming = true
			continue
		}
		if msg.Role == schema.Tool {
			// HistoryLimit can cut away the corresponding assistant message.
			if _, exists := open[msg.ToolCallID]; exists {
				msgs = append(msgs, msg)
				delete(open, msg.ToolCallID)
			}
			continue
		}
		seal()
		msgs = append(msgs, msg)
		for _, call := range msg.ToolCalls {
			open[call.ID] = call
		}
	}
	seal()
	if len(msgs) == 0 {
		msgs = append(msgs, schema.UserMessage(task))
	}
	if resuming {
		msgs = append(msgs, schema.UserMessage("The host has restarted. Resume the original task using the handoff as context, not as evidence of task completion. Verify unknown external effects before repeating an interrupted operation."))
	}
	return msgs
}

func workerReplayHistory(history []*schema.Message, instruction string) []*schema.Message {
	out := make([]*schema.Message, 0, len(history))
	leading := true
	for _, msg := range history {
		if msg != nil && msg.Role != schema.System {
			leading = false
		}
		if leading && msg != nil && msg.Role == schema.System && msg.Content == instruction {
			continue
		}
		out = append(out, msg)
	}
	return out
}
