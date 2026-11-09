package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/cloudwego/eino/adk"
)

const modelRequestCloseoutPrefix = "Model requests remaining:"

type modelRequestCloseout struct {
	adk.BaseChatModelAgentMiddleware
	pool       *provider.Pool
	providerID string
}

func (m *modelRequestCloseout) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, _ *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	limit, remaining, known := m.pool.ModelRequestBudget(m.providerID)
	showBudget := known && remaining > 0
	var messages []adk.Message
	for i, msg := range state.Messages {
		if isRuntimeMessage(msg) && strings.HasPrefix(msg.Content, modelRequestCloseoutPrefix) {
			if messages == nil {
				messages = make([]adk.Message, 0, len(state.Messages))
				messages = append(messages, state.Messages[:i]...)
			}
			continue
		}
		if messages != nil {
			messages = append(messages, msg)
		}
	}
	if !showBudget && messages == nil {
		return ctx, state, nil
	}
	if showBudget {
		if messages == nil {
			messages = append(make([]adk.Message, 0, len(state.Messages)+1), state.Messages...)
		}
		text := fmt.Sprintf("%s %d of %d.", modelRequestCloseoutPrefix, remaining, limit)
		if remaining <= max(1, limit/5) {
			text += " Use the remaining requests for the user's required verification and delivery. If the current evidence supports a small reversible correction, apply it now and rerun the smallest failing check. Verify the result at the requested destination. Leave optional investigation for after the required outcome. Report any requirement that still lacks evidence."
		}
		messages = append(messages, runtimeUserMessage(text))
	}
	next := *state
	next.Messages = messages
	return ctx, &next, nil
}
