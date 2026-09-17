package provider

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// exclusiveContentModel strips Message.Content when a multimodal field is
// also set. go-openai's ChatCompletionMessage cannot marshal both, and
// eino-ext copies Content onto that struct even when UserInputMultiContent
// already carries the caption.
type exclusiveContentModel struct {
	inner model.BaseChatModel
}

func (m *exclusiveContentModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return m.inner.Generate(ctx, exclusiveContent(in), opts...)
}

func (m *exclusiveContentModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return m.inner.Stream(ctx, exclusiveContent(in), opts...)
}

func exclusiveContent(in []*schema.Message) []*schema.Message {
	if !needsExclusiveContent(in) {
		return in
	}
	out := make([]*schema.Message, len(in))
	for i, msg := range in {
		out[i] = exclusiveContentOne(msg)
	}
	return out
}

func needsExclusiveContent(in []*schema.Message) bool {
	for _, msg := range in {
		if msg != nil && msg.Content != "" && hasMulti(msg) {
			return true
		}
	}
	return false
}

func exclusiveContentOne(msg *schema.Message) *schema.Message {
	if msg == nil || msg.Content == "" || !hasMulti(msg) {
		return msg
	}
	cp := *msg
	if len(cp.UserInputMultiContent) > 0 {
		cp.UserInputMultiContent = ensureInputText(cp.UserInputMultiContent, cp.Content)
	} else if len(cp.AssistantGenMultiContent) > 0 {
		cp.AssistantGenMultiContent = ensureOutputText(cp.AssistantGenMultiContent, cp.Content)
	} else if len(cp.MultiContent) > 0 {
		cp.MultiContent = ensureDeprecatedText(cp.MultiContent, cp.Content)
	}
	cp.Content = ""
	return &cp
}

func hasMulti(m *schema.Message) bool {
	return len(m.UserInputMultiContent) > 0 ||
		len(m.AssistantGenMultiContent) > 0 ||
		len(m.MultiContent) > 0
}

func ensureInputText(parts []schema.MessageInputPart, text string) []schema.MessageInputPart {
	if inputHasText(parts, text) {
		return parts
	}
	out := make([]schema.MessageInputPart, 0, len(parts)+1)
	out = append(out, schema.MessageInputPart{Type: schema.ChatMessagePartTypeText, Text: text})
	return append(out, parts...)
}

func ensureOutputText(parts []schema.MessageOutputPart, text string) []schema.MessageOutputPart {
	for _, p := range parts {
		if p.Type == schema.ChatMessagePartTypeText && p.Text == text {
			return parts
		}
	}
	out := make([]schema.MessageOutputPart, 0, len(parts)+1)
	out = append(out, schema.MessageOutputPart{Type: schema.ChatMessagePartTypeText, Text: text})
	return append(out, parts...)
}

func ensureDeprecatedText(parts []schema.ChatMessagePart, text string) []schema.ChatMessagePart {
	for _, p := range parts {
		if p.Type == schema.ChatMessagePartTypeText && p.Text == text {
			return parts
		}
	}
	out := make([]schema.ChatMessagePart, 0, len(parts)+1)
	out = append(out, schema.ChatMessagePart{Type: schema.ChatMessagePartTypeText, Text: text})
	return append(out, parts...)
}

func inputHasText(parts []schema.MessageInputPart, text string) bool {
	for _, p := range parts {
		if p.Type == schema.ChatMessagePartTypeText && p.Text == text {
			return true
		}
	}
	return false
}
