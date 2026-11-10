package engine

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	openai "github.com/meguminnnnnnnnn/go-openai"
)

type dreamReviewModelProbe struct {
	model.BaseChatModel
	stream func(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error)
}

func (p dreamReviewModelProbe) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return p.stream(ctx, in, opts...)
}

func dreamTestStream() *schema.StreamReader[*schema.Message] {
	return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("done", nil)})
}

func TestDreamReviewModelProjectsOnlyTheFirstOfferAndBoundReply(t *testing.T) {
	dream := &dreamReview{prompt: dreamAuditHeader + "{\"test\":true}"}
	baseTools := []*schema.ToolInfo{{Name: "skill_view"}, {Name: reviewBatchToolName}}
	offerTools := []*schema.ToolInfo{baseTools[0], {Name: reviewBatchToolName, Desc: "audit schema"}}
	opts := []model.Option{model.WithMaxTokens(123), model.WithTools(offerTools)}
	earlyArgs := `{"changes":[],"dream":{"decision":"keep"}}`
	lateArgs := `{"changes":[],"dream":{"decision":"uncertain"}}`
	before := []*schema.Message{schema.SystemMessage("ordinary instruction"), nil, schema.UserMessage("ordinary review input")}
	firstReply := schema.AssistantMessage("", []schema.ToolCall{
		{ID: "apply", Function: schema.FunctionCall{Name: reviewBatchToolName, Arguments: earlyArgs}},
		{ID: "view", Function: schema.FunctionCall{Name: "skill_view", Arguments: `{"name":"other"}`}},
	})
	lateReply := schema.AssistantMessage("", []schema.ToolCall{{ID: "late", Function: schema.FunctionCall{Name: reviewBatchToolName, Arguments: lateArgs}}})
	followup := append([]*schema.Message{before[0], before[1], before[2], nil}, firstReply, schema.ToolMessage("ordinary refusal", "apply"), lateReply)
	var firstCalls, ordinaryCalls int
	m := &dreamReviewModel{dream: dream, ordinaryBatch: baseTools[1],
		first: dreamReviewModelProbe{stream: func(_ context.Context, got []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
			firstCalls++
			common := model.GetCommonOptions(nil, options...)
			if !dream.offered.Load() || got[0].Content != dreamVerdictInstruction+"\n"+before[0].Content ||
				got[2].Content != before[2].Content+dream.prompt || len(common.Tools) != 1 || common.Tools[0] != offerTools[1] ||
				common.ToolChoice == nil || *common.ToolChoice != schema.ToolChoiceForced ||
				len(common.AllowedToolNames) != 1 || common.AllowedToolNames[0] != reviewBatchToolName {
				t.Fatalf("first offer did not preserve and extend the expected input: %#v", got)
			}
			return dreamTestStream(), nil
		}},
		ordinary: dreamReviewModelProbe{stream: func(_ context.Context, got []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
			ordinaryCalls++
			common := model.GetCommonOptions(nil, options...)
			if dream.offered.Load() || got[0] != before[0] || got[2] != before[2] || common.Tools[0] != baseTools[0] ||
				common.Tools[1] != baseTools[1] || common.MaxTokens == nil || *common.MaxTokens != 123 ||
				common.ToolChoice != nil || len(common.AllowedToolNames) != 0 {
				t.Fatalf("ordinary follow-up changed its base inputs or options: %#v", got)
			}
			if got[4] == firstReply || got[4].ToolCalls[0].Function.Arguments != `{"changes":[]}` ||
				got[4].ToolCalls[1].Function.Arguments != firstReply.ToolCalls[1].Function.Arguments || got[5] != followup[5] ||
				got[6] != lateReply || got[6].ToolCalls[0].Function.Arguments != lateArgs {
				t.Fatalf("ordinary follow-up projected the wrong tool history: %#v", got)
			}
			return dreamTestStream(), nil
		}},
	}
	if _, err := m.Generate(t.Context(), before, opts...); err != nil {
		t.Fatal(err)
	}
	stream, err := m.Stream(t.Context(), followup, opts...)
	if err != nil {
		t.Fatal(err)
	}
	stream.Close()
	if firstCalls != 1 || ordinaryCalls != 1 || model.GetCommonOptions(nil, opts...).ToolChoice != nil ||
		firstReply.ToolCalls[0].Function.Arguments != earlyArgs ||
		strings.Contains(before[0].Content, dreamVerdictInstruction) || strings.Contains(before[2].Content, dreamAuditHeader) || offerTools[1].Desc != "audit schema" {
		t.Fatal("request projection mutated source history or repeated the offer")
	}
}

func TestDreamReviewModelOnlyFallsBackOnImmediateOptionalHTTP400(t *testing.T) {
	badRequest := &openai.APIError{HTTPStatusCode: http.StatusBadRequest}
	for _, tt := range []struct {
		name       string
		err        error
		optional   bool
		withStream bool
		cancelled  bool
		fallback   bool
	}{
		{name: "optional request refused", err: badRequest, optional: true, fallback: true},
		{name: "explicit request refused", err: badRequest},
		{name: "partial response", err: badRequest, optional: true, withStream: true},
		{name: "cancelled", err: badRequest, optional: true, cancelled: true},
		{name: "unknown error", err: errors.New("unclassified failure"), optional: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			in := []*schema.Message{schema.SystemMessage("ordinary"), schema.UserMessage("task")}
			dream := &dreamReview{prompt: dreamAuditHeader}
			plain := &schema.ToolInfo{Name: reviewBatchToolName}
			var firstCalls, ordinaryCalls int
			m := &dreamReviewModel{dream: dream, ordinaryBatch: plain, optional: tt.optional,
				first: dreamReviewModelProbe{stream: func(_ context.Context, _ []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
					firstCalls++
					common := model.GetCommonOptions(nil, options...)
					if common.ToolChoice == nil || *common.ToolChoice != schema.ToolChoiceForced ||
						len(common.AllowedToolNames) != 1 || common.AllowedToolNames[0] != reviewBatchToolName {
						t.Fatal("the first audit offer must select the named batch tool")
					}
					if tt.withStream {
						return dreamTestStream(), tt.err
					}
					return nil, tt.err
				}},
				ordinary: dreamReviewModelProbe{stream: func(_ context.Context, got []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
					ordinaryCalls++
					common := model.GetCommonOptions(nil, options...)
					if dream.offered.Load() || got[0] != in[0] || got[1] != in[1] || common.Tools[0] != plain ||
						common.ToolChoice != nil || len(common.AllowedToolNames) != 0 {
						t.Fatalf("optional fallback replayed the Dream payload: %#v", got)
					}
					return dreamTestStream(), nil
				}},
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancelled {
				cancel()
			}
			stream, err := m.Stream(ctx, in, model.WithTools([]*schema.ToolInfo{{Name: reviewBatchToolName, Desc: "audit"}}))
			if stream != nil {
				stream.Close()
			}
			if firstCalls != 1 || (ordinaryCalls == 1) != tt.fallback || (err == nil) != tt.fallback || dream.offered.Load() || m.auditReplyAt.Load() != 0 {
				t.Fatalf("first=%d ordinary=%d err=%v offered=%t", firstCalls, ordinaryCalls, err, dream.offered.Load())
			}
		})
	}
}

func TestDreamReviewModelSkipsAnIncompleteOfferAndKeepsOrdinaryArguments(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{`{ "changes" : [] }`, `{ "changes" : [] }`},
		{`{"DREAM":"ignored","changes":[]}`, `{"changes":[]}`},
		{`{"dream":`, `{}`},
		{`null`, `{}`},
	} {
		if got := withoutDreamArguments(tt.input); got != tt.want {
			t.Errorf("withoutDreamArguments(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
	dream := &dreamReview{prompt: dreamAuditHeader}
	m := &dreamReviewModel{dream: dream, ordinary: dreamReviewModelProbe{
		stream: func(_ context.Context, in []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
			if len(in) != 1 || in[0].Content != "only user input" || dream.offered.Load() || len(options) != 0 {
				t.Fatalf("incomplete audit was not skipped: %#v", in)
			}
			return dreamTestStream(), nil
		},
	}}
	stream, err := m.Stream(t.Context(), []*schema.Message{schema.UserMessage("only user input")})
	if err != nil {
		t.Fatal(err)
	}
	stream.Close()
}
