package engine

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// foldWithEino runs eino's summarization middleware for the briefing, with
// our prompt and Finalize. The middleware still calls Generate; the model
// we hand it drains Stream underneath. Trigger, events, swallow-on-error,
// and token accounting stay in autoCompact — those are product, not a
// second summarizer.
func (m *autoCompact) foldWithEino(ctx context.Context, state *adk.ChatModelAgentState) (*adk.ChatModelAgentState, error) {
	cm, err := m.summarizer(ctx)
	if err != nil {
		return nil, err
	}
	prev := ""
	if th, e := m.engine.store.GetThread(m.threadID); e == nil {
		prev = th.CompactSummary
	}
	mw, err := summarization.New(ctx, m.einoSummarizeConfig(cm, prev))
	if err != nil {
		return nil, err
	}
	_, next, err := mw.BeforeModelRewriteState(ctx, state, nil)
	return next, err
}

// einoSummarizeConfig is the lockable wiring: override the coding-agent
// prompt, never DefaultFinalize. Tests inspect this; do not inline a
// different Config in foldWithEino.
func (m *autoCompact) einoSummarizeConfig(cm model.BaseChatModel, prev string) *summarization.Config {
	keep := m.keep
	return &summarization.Config{
		Model:           cm,
		Trigger:         &summarization.TriggerCondition{ContextMessages: 1},
		UserInstruction: compactPrompt(),
		GenModelInput: func(_ context.Context, _, _ *schema.Message, original []*schema.Message) ([]*schema.Message, error) {
			_, older, _ := splitCompactable(original, keep)
			return []*schema.Message{
				schema.SystemMessage(compactPrompt()),
				schema.UserMessage(compactInputFromADK(prev, older)),
			}, nil
		},
		Finalize: m.finalizeFold,
	}
}

func (m *autoCompact) finalizeFold(_ context.Context, original []*schema.Message, summary *schema.Message) ([]*schema.Message, error) {
	prev := ""
	if th, err := m.engine.store.GetThread(m.threadID); err == nil {
		prev = th.CompactSummary
	}
	_, older, _ := splitCompactable(original, m.keep)
	text := acceptBriefing(compactAssistantText(summary), compactInputFromADK(prev, older))
	if text == "" {
		return nil, errors.New(briefingRejectReason(compactAssistantText(summary), compactInputFromADK(prev, older)))
	}
	system, older, tail := splitCompactable(original, m.keep)
	roster := dropPairsAlreadyIn(m.rosterPin(), tail)
	next := assembleCompacted(system, text, older, tail, roster)
	clearBilledUsage(next)
	return next, nil
}

func compactAssistantText(m *schema.Message) string {
	if m == nil {
		return ""
	}
	if s := strings.TrimSpace(m.Content); s != "" {
		return s
	}
	return strings.TrimSpace(m.ReasoningContent)
}

func briefingFromMessages(msgs []*schema.Message) string {
	for _, m := range msgs {
		if isCompactBriefingMessage(m) {
			return strings.TrimSpace(m.Content)
		}
	}
	return ""
}
