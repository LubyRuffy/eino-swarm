package swarm

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// GenerationInterruption describes one model output, not the worker's lifetime.
// Recovery is updated only once a replacement generation has actually begun.
type GenerationInterruption struct {
	Reason   string `json:"reason" yaml:"reason"`
	Detail   string `json:"detail" yaml:"detail"`
	Recovery string `json:"recovery" yaml:"recovery"`
}

func (s *streamAcc) interruptGeneration(err error) {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return
	}
	s.active = false
	ctx := s.ctx
	s.mu.Unlock()
	cause := GenerationInterruption{Reason: "model_error", Detail: "The model request did not complete.", Recovery: "none"}
	switch {
	case errors.Is(err, errOutputBudget):
		cause.Reason, cause.Detail = "output_limit", "The model reached its output limit before generation completed."
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) && ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded):
		cause.Reason, cause.Detail = "timeout", "The generation deadline expired."
	case errors.Is(err, context.Canceled):
		cause.Reason, cause.Detail = "cancelled", "The generation was cancelled."
	}
	if s.handle != nil {
		s.handle.mu.Lock()
		handoff := s.handle.handoff.cancelled
		s.handle.mu.Unlock()
		if handoff {
			cause.Recovery = "pending"
			if cause.Reason == "cancelled" {
				cause.Reason, cause.Detail = "engine_restart", "The engine interrupted generation for restart handoff."
			}
		}
	}
	body, _ := json.Marshal(cause)
	s.emit(Notification{Kind: NotifyGenerationInterrupted, Text: string(body), Err: err})
}

func (s *streamAcc) completeGeneration() {
	s.mu.Lock()
	active := s.active
	s.active = false
	s.mu.Unlock()
	if active {
		s.emit(Notification{Kind: NotifyGenerationCompleted})
	}
}

// generationModel starts the receipt before the HTTP adapter, so a rejected
// request or cancellation before the first byte cannot disappear from Trace.
type generationModel struct {
	model.BaseChatModel
	acc *streamAcc
}

type generationAttempt struct {
	id  string
	ctx context.Context
}

func (m *generationModel) start(ctx context.Context) *streamAcc {
	attempt := &streamAcc{reg: m.acc.reg, agentID: m.acc.agentID, role: m.acc.role, handle: m.acc.handle, messageID: rand.Text(), ctx: ctx, active: true}
	attempt.emit(Notification{Kind: NotifyGenerationStarted})
	return attempt
}

func (m *generationModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	attempt := m.start(ctx)
	out, err := m.BaseChatModel.Generate(ctx, in, opts...)
	if err != nil {
		attempt.interruptGeneration(err)
	} else if out != nil && out.ResponseMeta != nil && outputStoppedEarly(out.ResponseMeta.FinishReason) {
		attempt.interruptGeneration(errOutputBudget)
		for _, tc := range out.ToolCalls {
			if !json.Valid([]byte(tc.Function.Arguments)) {
				return out, errOutputBudget
			}
		}
		if len(out.ToolCalls) == 0 && strings.TrimSpace(out.Content) == "" {
			return out, errOutputBudget
		}
	} else {
		attempt.completeGeneration()
	}
	return out, err
}

func (m *generationModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	attempt := m.start(ctx)
	out, err := m.BaseChatModel.Stream(ctx, in, opts...)
	if err != nil {
		attempt.interruptGeneration(err)
	} else {
		m.acc.mu.Lock()
		m.acc.pending = append(m.acc.pending, generationAttempt{id: attempt.messageID, ctx: ctx})
		m.acc.mu.Unlock()
		out = guardGenerationTools(out)
	}
	return out, err
}

// ADK may reject a stream before exposing it as an AgentEvent. Pending
// requests still own a lifecycle receipt even when no UI drain was started.
func (s *streamAcc) interruptPending(err error) {
	s.mu.Lock()
	pending := s.pending
	s.pending = nil
	s.mu.Unlock()
	for _, attempt := range pending {
		acc := &streamAcc{reg: s.reg, agentID: s.agentID, role: s.role, handle: s.handle, messageID: attempt.id, ctx: attempt.ctx, active: true}
		acc.interruptGeneration(err)
	}
}

// ADK copies this guarded reader, so its shared parent converts each chunk
// once. EOF validation precedes tool admission, and Close reaches the original
// source without adding a relay goroutine that could remain blocked in Recv.
func guardGenerationTools(source *schema.StreamReader[*schema.Message]) *schema.StreamReader[*schema.Message] {
	var chunks []schema.ToolCall
	rejected := false
	capped := false
	return schema.StreamReaderWithConvert(source, func(msg *schema.Message) (*schema.Message, error) {
		if msg == nil {
			return nil, nil
		}
		chunks = append(chunks, msg.ToolCalls...)
		if msg.ResponseMeta != nil && outputStoppedEarly(msg.ResponseMeta.FinishReason) {
			capped = true
			for _, call := range mergeStreamedToolCalls(chunks) {
				if !json.Valid([]byte(call.Function.Arguments)) {
					rejected = true
					break
				}
			}
		}
		if rejected {
			// Keep text emitted in the same frame; the EOF error rejects the whole
			// tool batch, including call fragments delivered in earlier chunks.
			safe := *msg
			safe.ToolCalls = nil
			return &safe, nil
		}
		return msg, nil
	}, schema.WithOnEOF(func() (any, error) {
		if rejected {
			return nil, errOutputBudget
		}
		return nil, io.EOF
	}), schema.WithErrWrapper(func(err error) error {
		if capped {
			return errOutputBudget
		}
		return err
	}))
}
