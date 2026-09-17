package provider

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestExclusiveContentLeavesPlainTextAlone(t *testing.T) {
	in := []*schema.Message{schema.UserMessage("hello")}
	got := exclusiveContent(in)
	if len(got) != 1 || got[0] != in[0] {
		t.Fatal("a text-only slice must be returned untouched so ADK history is not copied")
	}
}

func TestExclusiveContentDropsCaptionAlreadyInParts(t *testing.T) {
	orig := &schema.Message{
		Role:    schema.User,
		Content: "look",
		UserInputMultiContent: []schema.MessageInputPart{
			{Type: schema.ChatMessagePartTypeText, Text: "look"},
			{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{}},
		},
	}
	in := []*schema.Message{orig}
	got := exclusiveContent(in)
	if got[0] == orig {
		t.Fatal("the original must stay dual-set so ADK history is not mutated")
	}
	if orig.Content != "look" {
		t.Fatalf("history caption was overwritten: %q", orig.Content)
	}
	if got[0].Content != "" {
		t.Fatalf("OpenAI cannot marshal Content with MultiContent: %q", got[0].Content)
	}
	if len(got[0].UserInputMultiContent) != 2 || got[0].UserInputMultiContent[0].Text != "look" {
		t.Fatalf("caption should stay in the existing text part: %+v", got[0].UserInputMultiContent)
	}
}

func TestExclusiveContentKeepsACaptionMissingFromParts(t *testing.T) {
	// Concat of a text-only chunk and an image-only chunk leaves the caption
	// on Content and the pixels on UserInputMultiContent. Dropping Content
	// without copying it would send a blind image.
	in := []*schema.Message{{
		Role:    schema.User,
		Content: "look here",
		UserInputMultiContent: []schema.MessageInputPart{
			{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageInputImage{}},
		},
	}}
	got := exclusiveContent(in)
	if got[0].Content != "" {
		t.Fatalf("still dual-set: %q", got[0].Content)
	}
	if len(got[0].UserInputMultiContent) != 2 ||
		got[0].UserInputMultiContent[0].Type != schema.ChatMessagePartTypeText ||
		got[0].UserInputMultiContent[0].Text != "look here" {
		t.Fatalf("caption must move into a text part: %+v", got[0].UserInputMultiContent)
	}
}

func TestExclusiveContentClearsAssistantAndDeprecatedMulti(t *testing.T) {
	asst := exclusiveContentOne(&schema.Message{
		Role:    schema.Assistant,
		Content: "hi",
		AssistantGenMultiContent: []schema.MessageOutputPart{
			{Type: schema.ChatMessagePartTypeText, Text: "hi"},
		},
	})
	if asst.Content != "" || len(asst.AssistantGenMultiContent) != 1 {
		t.Fatalf("assistant dual-set: %+v", asst)
	}
	moved := exclusiveContentOne(&schema.Message{
		Role:    schema.Assistant,
		Content: "look",
		AssistantGenMultiContent: []schema.MessageOutputPart{
			{Type: schema.ChatMessagePartTypeImageURL, Image: &schema.MessageOutputImage{}},
		},
	})
	if moved.Content != "" || len(moved.AssistantGenMultiContent) != 2 ||
		moved.AssistantGenMultiContent[0].Type != schema.ChatMessagePartTypeText ||
		moved.AssistantGenMultiContent[0].Text != "look" {
		t.Fatalf("assistant caption must move into a text part: %+v", moved)
	}
	old := exclusiveContentOne(&schema.Message{
		Role:    schema.User,
		Content: "hi",
		MultiContent: []schema.ChatMessagePart{
			{Type: schema.ChatMessagePartTypeText, Text: "other"},
		},
	})
	if old.Content != "" || len(old.MultiContent) != 2 || old.MultiContent[0].Text != "hi" {
		t.Fatalf("deprecated MultiContent lost the caption: %+v", old)
	}
}

func TestExclusiveContentModelStripsBeforeGenerateAndStream(t *testing.T) {
	inner := &captureModel{out: schema.AssistantMessage("ok", nil)}
	m := &exclusiveContentModel{inner: inner}
	in := []*schema.Message{{
		Role:    schema.User,
		Content: "look",
		UserInputMultiContent: []schema.MessageInputPart{
			{Type: schema.ChatMessagePartTypeText, Text: "look"},
		},
	}}
	if _, err := m.Generate(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if inner.got[0].Content != "" {
		t.Fatalf("Generate still sent Content: %q", inner.got[0].Content)
	}
	if in[0].Content != "look" {
		t.Fatal("Generate must not mutate the caller's slice")
	}

	inner.chunks = []*schema.Message{schema.AssistantMessage("ok", nil)}
	stream, err := m.Stream(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if inner.got[0].Content != "" {
		t.Fatalf("Stream still sent Content: %q", inner.got[0].Content)
	}
}

type captureModel struct {
	got    []*schema.Message
	out    *schema.Message
	chunks []*schema.Message
}

func (c *captureModel) Generate(_ context.Context, in []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	c.got = in
	return c.out, nil
}

func (c *captureModel) Stream(_ context.Context, in []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	c.got = in
	sr, sw := schema.Pipe[*schema.Message](len(c.chunks) + 1)
	go func() {
		defer sw.Close()
		for _, chunk := range c.chunks {
			sw.Send(chunk, nil)
		}
	}()
	return sr, nil
}
