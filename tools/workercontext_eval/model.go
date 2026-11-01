package main

import (
	"context"
	"encoding/json"
	"sync"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type inputMetrics struct {
	Requests              int `json:"requests"`
	OriginalChars         int `json:"original_chars"`
	ForwardedChars        int `json:"forwarded_chars"`
	DuplicateCharsRemoved int `json:"duplicate_chars_removed"`
	ReferenceCount        int `json:"reference_count"`
}

type modelInput struct {
	Original     json.RawMessage `json:"original"`
	Forwarded    json.RawMessage `json:"forwarded"`
	Replacements []replacement   `json:"replacements"`
}

type callUsage struct {
	Backoff          bool   `json:"backoff"`
	AgentID          string `json:"agent_id"`
	ProviderID       string `json:"provider_id"`
	Model            string `json:"model"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	CachedTokens     int    `json:"cached_tokens"`
	DurationMS       int64  `json:"duration_ms"`
	Error            string `json:"error"`
}

type toolReceipt struct {
	Tool      string `json:"tool"`
	CallID    string `json:"tool_call_id"`
	Arguments string `json:"arguments"`
	Result    string `json:"result"`
	Error     string `json:"error"`
}

type audit struct {
	mu         sync.Mutex
	Inputs     []modelInput
	Metrics    inputMetrics
	Calls      []callUsage
	Receipts   []toolReceipt
	Violations []string
}

func (a *audit) snapshot() audit {
	a.mu.Lock()
	defer a.mu.Unlock()
	return audit{Inputs: append([]modelInput(nil), a.Inputs...), Metrics: a.Metrics,
		Calls: append([]callUsage(nil), a.Calls...), Receipts: append([]toolReceipt(nil), a.Receipts...),
		Violations: append([]string(nil), a.Violations...)}
}

func (a *audit) recordCall(call provider.CallRecord) {
	if call.Backoff != nil {
		return
	}
	errText := ""
	if call.Err != nil {
		errText = call.Err.Error()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Calls = append(a.Calls, callUsage{AgentID: call.AgentID, ProviderID: call.ProviderID, Model: call.Model,
		PromptTokens: call.PromptTokens, CompletionTokens: call.CompletionTokens, TotalTokens: call.TotalTokens,
		CachedTokens: call.CachedTokens, DurationMS: call.Duration.Milliseconds(), Error: errText})
}

type contextModel struct {
	inner model.BaseChatModel
	arm   string
	audit *audit
}

func (m *contextModel) prepare(input []*schema.Message) ([]*schema.Message, error) {
	original, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	forwarded := input
	var changes []replacement
	if m.arm == "b" {
		forwarded, changes = referenceDuplicates(input)
	}
	encoded, err := json.Marshal(forwarded)
	if err != nil {
		return nil, err
	}
	m.audit.mu.Lock()
	defer m.audit.mu.Unlock()
	m.audit.Inputs = append(m.audit.Inputs, modelInput{Original: original, Forwarded: encoded, Replacements: changes})
	m.audit.Metrics.Requests++
	m.audit.Metrics.OriginalChars += utf8.RuneCount(original)
	m.audit.Metrics.ForwardedChars += utf8.RuneCount(encoded)
	m.audit.Metrics.ReferenceCount += len(changes)
	for _, change := range changes {
		m.audit.Metrics.DuplicateCharsRemoved += change.ResultChars
	}
	return forwarded, nil
}

func (m *contextModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	forwarded, err := m.prepare(input)
	if err != nil {
		return nil, err
	}
	return m.inner.Generate(ctx, forwarded, opts...)
}

func (m *contextModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	forwarded, err := m.prepare(input)
	if err != nil {
		return nil, err
	}
	return m.inner.Stream(ctx, forwarded, opts...)
}
