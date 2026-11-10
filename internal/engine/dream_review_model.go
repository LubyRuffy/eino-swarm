package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// dreamReviewModel offers the audit on one request in the existing reviewer.
// The agent's ordinary prompt and tool history remain the source for later calls.
// The first model must not retry an optional reasoning setting on its own.
type dreamReviewModel struct {
	ordinary      model.BaseChatModel
	first         model.BaseChatModel
	dream         *dreamReview
	ordinaryBatch *schema.ToolInfo
	optional      bool
	sent          atomic.Bool
	// One-based index where the first audit response enters the agent history.
	// Zero means no audit response reached the agent.
	auditReplyAt atomic.Int64
}

func (m *dreamReviewModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return compactStream(ctx, m, in, opts...)
}

func (m *dreamReviewModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if m.sent.CompareAndSwap(false, true) {
		tools := model.GetCommonOptions(nil, opts...).Tools
		batch := slices.IndexFunc(tools, func(info *schema.ToolInfo) bool {
			return info != nil && info.Name == reviewBatchToolName
		})
		if offered := m.offer(in); batch >= 0 && offered != nil {
			m.dream.offered.Store(true)
			firstOptions := append(slices.Clone(opts),
				model.WithTools([]*schema.ToolInfo{tools[batch]}),
				model.WithToolChoice(schema.ToolChoiceForced, reviewBatchToolName))
			stream, err := m.first.Stream(ctx, offered, firstOptions...)
			if err == nil {
				m.auditReplyAt.Store(int64(len(in)) + 1)
				return stream, nil
			}
			m.dream.offered.Store(false)
			if stream != nil {
				stream.Close()
				return nil, err
			}
			if !m.optional || ctx.Err() != nil || provider.HTTPStatus(err) != http.StatusBadRequest {
				return nil, err
			}
		}
	}
	m.dream.offered.Store(false)
	return m.ordinary.Stream(ctx, m.ordinaryHistory(in), m.ordinaryOptions(opts)...)
}

func (m *dreamReviewModel) offer(in []*schema.Message) []*schema.Message {
	system, user := -1, -1
	for i, msg := range in {
		if msg == nil {
			continue
		}
		switch msg.Role {
		case schema.System:
			if system < 0 {
				system = i
			}
		case schema.User:
			user = i
		}
	}
	if system < 0 || user < 0 {
		return nil
	}
	out := slices.Clone(in)
	instruction, evidence := *in[system], *in[user]
	instruction.Content = dreamVerdictInstruction + "\n" + instruction.Content
	evidence.Content += m.dream.prompt
	out[system], out[user] = &instruction, &evidence
	return out
}

func (m *dreamReviewModel) ordinaryOptions(opts []model.Option) []model.Option {
	tools := model.GetCommonOptions(nil, opts...).Tools
	for i, info := range tools {
		if info != nil && info.Name == reviewBatchToolName {
			ordinary := slices.Clone(tools)
			ordinary[i] = m.ordinaryBatch
			return append(slices.Clone(opts), model.WithTools(ordinary))
		}
	}
	return opts
}

func (m *dreamReviewModel) ordinaryHistory(in []*schema.Message) []*schema.Message {
	start := m.auditReplyAt.Load() - 1
	if start < 0 || start >= int64(len(in)) {
		return in
	}
	for i := int(start); i < len(in); i++ {
		msg := in[i]
		if msg == nil || msg.Role != schema.Assistant {
			continue
		}
		var calls []schema.ToolCall
		for j, call := range msg.ToolCalls {
			if call.Function.Name != reviewBatchToolName {
				continue
			}
			args := withoutDreamArguments(call.Function.Arguments)
			if args != call.Function.Arguments {
				if calls == nil {
					calls = slices.Clone(msg.ToolCalls)
				}
				calls[j].Function.Arguments = args
			}
		}
		if calls == nil {
			return in
		}
		out := slices.Clone(in)
		projected := *msg
		projected.ToolCalls = calls
		out[i] = &projected
		return out
	}
	return in
}

func withoutDreamArguments(args string) string {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(args), &fields); err != nil || fields == nil {
		// The paired refusal still gives the ordinary reviewer a repair path.
		// Malformed args cannot be proven free of audit text.
		return "{}"
	}
	count := len(fields)
	for name := range fields {
		if strings.EqualFold(name, "dream") {
			delete(fields, name)
		}
	}
	if len(fields) == count {
		return args
	}
	// Every remaining value came from a successful JSON decode.
	raw, _ := json.Marshal(fields)
	return string(raw)
}
