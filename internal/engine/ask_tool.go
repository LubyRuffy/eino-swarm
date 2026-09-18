package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// AskUserTool is the manager-only tool that pauses this turn until the human
// answers. The TUI binds a local bridge; the app binds the conversation runtime.
func AskUserTool(ask func(ctx context.Context, questions []AskQuestion) (AskAnswers, error)) tool.BaseTool {
	if ask == nil {
		ask = func(context.Context, []AskQuestion) (AskAnswers, error) {
			return nil, fmt.Errorf("ask_user is not wired")
		}
	}
	return &askUserTool{ask: ask}
}

type askUserTool struct {
	ask func(ctx context.Context, questions []AskQuestion) (AskAnswers, error)
}

func (t *askUserTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: ToolAskUser,
		Desc: "Ask the human one to three mutually exclusive multiple-choice questions when a material preference or tradeoff would waste work if guessed. Explore first. Do not include a free-form option; the host adds it. Do not use this to confirm an obvious next step, to wait in chat, or to pause a standing objective. Workers cannot ask.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"questions": {
				Type:     schema.Array,
				Required: true,
				Desc:     "1-3 questions. Each is {id, prompt, header?, options:[{id,label,description?}]} with 2-4 mutually exclusive options. Do not add a free-form option.",
				ElemInfo: &schema.ParameterInfo{
					Type: schema.Object,
					SubParams: map[string]*schema.ParameterInfo{
						"id":     {Type: schema.String, Required: true, Desc: "stable snake_case id"},
						"header": {Type: schema.String, Desc: "optional short chip for the UI"},
						"prompt": {Type: schema.String, Required: true, Desc: "one-sentence question"},
						"options": {
							Type: schema.Array,
							ElemInfo: &schema.ParameterInfo{
								Type: schema.Object,
								SubParams: map[string]*schema.ParameterInfo{
									"id":          {Type: schema.String, Required: true},
									"label":       {Type: schema.String, Required: true},
									"description": {Type: schema.String},
								},
							},
						},
					},
				},
			},
		}),
	}, nil
}

func (t *askUserTool) InvokableRun(ctx context.Context, args string, _ ...tool.Option) (string, error) {
	questions, err := ParseAskArguments(args)
	if err != nil {
		return askToolFailure("%s", err.Error()), nil
	}
	answers, err := t.ask(ctx, questions)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return askToolFailure("cancelled"), nil
		}
		return askToolFailure("%s", err.Error()), nil
	}
	return FormatAskResult(answers), nil
}

func askToolFailure(format string, args ...any) string {
	body, _ := json.Marshal(map[string]any{
		"ok":    false,
		"error": fmt.Sprintf(format, args...),
	})
	return string(body)
}
