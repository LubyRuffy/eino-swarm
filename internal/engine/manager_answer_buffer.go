package engine

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/LubyRuffy/eino-swarm/internal/store"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const pendingScheduleAnswerKey = "zwai_pending_schedule_answer"

// Only manager prose waits for admission. Reasoning, tools and workers remain
// live; an unsupported scheduling promise must never reach SSE or replay first.
type managerAnswerBuffer struct {
	adk.BaseChatModelAgentMiddleware
	acc           *accumulator
	id            string
	mu            sync.Mutex
	complete      []swarm.Notification
	pending       *swarm.Notification
	generated     []*schema.Message
	reportFailure *scheduleAnswerError
	historyReport *pendingHistoryReport
	historyScope  *string
}

type scheduleAnswerBufferContextKey struct{}

func (b *managerAnswerBuffer) reportStatus(failure *scheduleAnswerError) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reportFailure = failure
}

func (b *managerAnswerBuffer) reportError() *scheduleAnswerError {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.reportFailure
}

func newManagerAnswerBuffer(acc *accumulator) *managerAnswerBuffer {
	return &managerAnswerBuffer{acc: acc, id: storeAnswerID()}
}

func (b *managerAnswerBuffer) WrapModel(_ context.Context, inner model.BaseModel[*schema.Message], _ *adk.ModelContext) (model.BaseModel[*schema.Message], error) {
	return &pendingAnswerModel{inner: inner, id: b.id}, nil
}

type pendingAnswerModel struct {
	inner model.BaseModel[*schema.Message]
	id    string
}

func (m *pendingAnswerModel) Generate(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	out, err := m.inner.Generate(ctx, messages, opts...)
	return markPendingAnswer(out, m.id), err
}

func (m *pendingAnswerModel) Stream(ctx context.Context, messages []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	out, err := m.inner.Stream(ctx, messages, opts...)
	if err != nil {
		return nil, err
	}
	marked := false
	return schema.StreamReaderWithConvert(out, func(chunk *schema.Message) (*schema.Message, error) {
		if marked || chunk == nil {
			return chunk, nil
		}
		marked = true
		return markPendingAnswer(chunk, m.id), nil
	}), nil
}

func markPendingAnswer(m *schema.Message, id string) *schema.Message {
	if m == nil {
		return nil
	}
	cp := *m
	cp.Extra = make(map[string]any, len(m.Extra)+1)
	for k, v := range m.Extra {
		cp.Extra[k] = v
	}
	cp.Extra[pendingScheduleAnswerKey] = id
	return &cp
}

func (b *managerAnswerBuffer) AfterModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if n := len(state.Messages); n > 0 {
		m := state.Messages[n-1]
		if m != nil && m.Role == schema.Assistant && m.Content != "" {
			b.mu.Lock()
			b.generated = append(b.generated, m)
			b.mu.Unlock()
		}
	}
	return ctx, state, nil
}

func (b *managerAnswerBuffer) onNotify(n swarm.Notification) {
	if !isManagerAgent(n.AgentID) {
		b.acc.onNotify(n)
		return
	}
	b.mu.Lock()
	switch n.Kind {
	case swarm.NotifyDelta:
		copy := n
		b.pending = &copy
		b.mu.Unlock()
		// Preserve the normal boundary between thinking and answering.
		b.acc.gate.Lock()
		b.acc.flushKeyLocked(liveKey(n.AgentID, swarm.NotifyReasoningDelta.String(), ""))
		b.acc.flushReasoning(n.AgentID)
		b.acc.gate.Unlock()
		return
	case swarm.NotifyAgentMessage:
		b.complete = append(b.complete, n)
		b.pending = nil
		b.mu.Unlock()
		return
	case swarm.NotifyToolCall:
		b.sealPending()
	case swarm.NotifyTurn, swarm.NotifyGenerationInterrupted:
		b.pending = nil
	case swarm.NotifyDone, swarm.NotifyError:
		n.Text = ""
	}
	b.mu.Unlock()
	b.acc.onNotify(n)
}

func (b *managerAnswerBuffer) sealPending() {
	if b.pending != nil {
		n := *b.pending
		n.Kind = swarm.NotifyAgentMessage
		b.complete = append(b.complete, n)
		b.pending = nil
	}
}

func (b *managerAnswerBuffer) textLocked() []string {
	var text []string
	for _, m := range b.generated {
		text = append(text, m.Content)
	}
	return text
}

func (b *managerAnswerBuffer) publish(expected []store.Schedule) *scheduleAnswerError {
	b.mu.Lock()
	notes := b.complete
	b.mu.Unlock()
	if len(notes) == 0 {
		return nil
	}
	e := b.acc.engine
	events := make([]store.Event, len(notes))
	for i, n := range notes {
		events[i] = b.acc.event(n, swarm.NotifyAgentMessage.String())
	}
	if failure := e.admitScheduleAnswer(b.acc.threadID, b.acc.turnID, expected, events); failure != nil {
		return failure
	}
	b.mu.Lock()
	b.complete = nil
	b.mu.Unlock()
	return nil
}

func (b *managerAnswerBuffer) accept(res swarm.RunResult) swarm.RunResult {
	out := make([]adk.Message, len(res.Transcript))
	for i, m := range res.Transcript {
		out[i] = m
		if m == nil || m.Extra[pendingScheduleAnswerKey] != b.id {
			continue
		}
		cp := *m
		cp.Extra = make(map[string]any, len(m.Extra))
		for k, v := range m.Extra {
			if k != pendingScheduleAnswerKey {
				cp.Extra[k] = v
			}
		}
		out[i] = &cp
	}
	res.Transcript = out
	return res
}

// Pending text cannot become a durable compacted fact before admission.
// Keep the actual tool calls, results and reasoning while removing prose.
func withoutPendingManagerProse(messages []*schema.Message) []*schema.Message {
	out := make([]*schema.Message, 0, len(messages))
	for _, m := range messages {
		if m == nil || m.Extra[pendingScheduleAnswerKey] == nil {
			out = append(out, m)
			continue
		}
		cp := withoutScheduleAnswerProse(m)
		if len(cp.ToolCalls) == 0 && cp.ReasoningContent == "" && cp.Extra["responses_output"] == nil {
			continue
		}
		out = append(out, cp)
	}
	return out
}

func (e *Engine) admitScheduleAnswer(threadID, turnID string, expected []store.Schedule, events []store.Event, candidates ...string) *scheduleAnswerError {
	return e.admitScheduleAnswerWithHistory(threadID, turnID, expected, events, nil, candidates...)
}

func (e *Engine) admitScheduleAnswerWithHistory(threadID, turnID string, expected []store.Schedule, events []store.Event, history *store.HistoryEvidence, candidates ...string) *scheduleAnswerError {
	// Stop/restart ownership and publication share the established rt.mu ->
	// recordMu order. A still-running turn row alone does not prove ownership.
	rt := e.runtimeFor(threadID)
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if !rt.running || rt.turnID != turnID {
		return &scheduleAnswerError{Code: "answer_owner_ended"}
	}
	if rt.userStopped || rt.abandoned {
		return &scheduleAnswerError{Code: "answer_owner_stopped"}
	}
	if len(candidates) > 0 && rt.reg != nil && rt.reg.HasPendingSteers() {
		return &scheduleAnswerError{Code: "answer_superseded"}
	}
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	if _, dropped := e.droppedTurns[turnID]; dropped {
		return &scheduleAnswerError{Code: "answer_owner_deleted"}
	}
	// A worker can start a history read while manager policy verification is
	// pending. Tool-call recording shares recordMu, so the final mode decision
	// cannot race a newly durable query/request into an unstructured answer.
	if history == nil {
		for _, event := range events {
			if (event.Kind != swarm.NotifyAgentMessage.String() && event.Kind != KindScheduleReport) || event.Text == "" {
				continue
			}
			required, queried, failure := e.historyAnswerMode(&store.Turn{ID: turnID, ThreadID: threadID})
			if failure != nil {
				return failure
			}
			if !required {
				break
			}
			prose := true
			if event.Kind == KindScheduleReport {
				var report struct {
					Findings string `json:"findings"`
				}
				if err := json.Unmarshal([]byte(event.Text), &report); err != nil {
					return &scheduleAnswerError{Code: "history_report_invalid", Cause: err}
				}
				prose = report.Findings != ""
			}
			if !prose {
				continue
			}
			if !queried {
				return &scheduleAnswerError{Code: "history_read_required"}
			}
			return &scheduleAnswerError{Code: "history_report_invalid"}
		}
	}
	if err := e.store.AdmitScheduleAnswerWithHistory(threadID, turnID, expected, events, history, candidates...); err != nil {
		if errors.Is(err, store.ErrScheduleAnswerAlreadyAdmitted) {
			return nil
		}
		if errors.Is(err, store.ErrScheduleAnswerStopped) {
			rt.userStopped = true
			if rt.cancel != nil {
				rt.cancel()
			}
			return &scheduleAnswerError{Code: "answer_owner_stopped", Cause: err}
		}
		code := "answer_storage_failed"
		if errors.Is(err, store.ErrHistoryEvidenceChanged) {
			code = "history_source_changed"
		} else if errors.Is(err, store.ErrScheduleAnswerChanged) {
			code = "schedule_state_changed"
		} else {
			e.log.Warn("could not persist an admitted manager answer", "thread", threadID, "turn", turnID, "code", code, "err", err)
		}
		return &scheduleAnswerError{Code: code, Cause: err}
	}
	for _, ev := range events {
		e.broadcast(ev)
	}
	e.reindex(threadID)
	return nil
}

// Marked model outputs retain identity across compaction copies. Never remove
// prior/user messages by text similarity when rejecting this attempt's prose.
func (b *managerAnswerBuffer) reject(res swarm.RunResult) swarm.RunResult {
	res.Final = ""
	out := make([]adk.Message, 0, len(res.Transcript))
	for _, m := range res.Transcript {
		if m != nil && m.Extra[pendingScheduleAnswerKey] == b.id {
			cp := withoutScheduleAnswerProse(m)
			if len(cp.ToolCalls) == 0 && cp.ReasoningContent == "" && cp.Extra["responses_output"] == nil {
				continue
			}
			out = append(out, cp)
		} else {
			out = append(out, m)
		}
	}
	res.Transcript = out
	return res
}
