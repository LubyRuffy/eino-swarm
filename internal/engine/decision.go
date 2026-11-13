package engine

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// KindDecision is the short result from the fixed decisions role.
const KindDecision = "decision"

// ErrDecisionUnavailable means this conversation cannot call the decisions role.
var ErrDecisionUnavailable = errors.New("engine: decision role is not enabled")

const decisionInputLimit = 8 << 10

// Decide asks the fixed decisions role about the current conversation and
// records the answer in that conversation. There is nothing for the user to configure.
func (e *Engine) Decide(ctx context.Context, threadID string) (string, error) {
	modelName, ok := e.roleModelForThread(threadID)
	if !ok || modelName == "" {
		return "", ErrDecisionUnavailable
	}
	th, err := e.store.GetThread(threadID)
	if err != nil {
		return "", err
	}
	input := decisionInput(e, threadID)
	if input == "" {
		return "", ErrDecisionUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	text, err := e.pool.Decide(ctx, th.ProviderID, modelName, input)
	if err != nil {
		return "", err
	}
	e.record(store.Event{ThreadID: threadID, Kind: KindDecision, Text: text})
	return text, nil
}

func (e *Engine) roleModelForThread(threadID string) (string, bool) {
	th, err := e.store.GetThread(threadID)
	if err != nil {
		return "", false
	}
	return e.roleModel(th.ProviderID, "decisions")
}

func decisionInput(e *Engine, threadID string) string {
	msgs, err := e.store.ListMessages(threadID)
	if err != nil {
		return ""
	}
	var parts []string
	for _, msg := range msgs {
		if msg.Role != string(schema.User) && msg.Role != string(schema.Assistant) {
			continue
		}
		text := strings.TrimSpace(msg.Content)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	joined := strings.Join(parts, "\n")
	if len(joined) > decisionInputLimit {
		for len(joined) > decisionInputLimit && !utf8.RuneStart(joined[len(joined)-decisionInputLimit]) {
			joined = joined[:len(joined)-1]
		}
		if len(joined) > decisionInputLimit {
			joined = joined[len(joined)-decisionInputLimit:]
		}
	}
	return strings.TrimSpace(joined)
}
