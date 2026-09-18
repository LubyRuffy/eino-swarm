package engine

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// ProposePlanTool is the manager-only tool that writes the conversation plan.
func ProposePlanTool(propose func(markdown string) (string, error)) tool.BaseTool {
	if propose == nil {
		propose = func(string) (string, error) {
			return `{"ok":false,"error":"propose_plan is not wired"}`, nil
		}
	}
	return &proposePlanTool{propose: propose}
}

type proposePlanTool struct {
	propose func(markdown string) (string, error)
}

func (t *proposePlanTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: ToolProposePlan,
		Desc: "Replace the conversation's plan with complete markdown. Call this when the plan is decision-complete. Do not start the work; write, edit, and exec are not mounted while planning. Do not ask whether to proceed inside the plan.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"markdown": {Type: schema.String, Required: true,
				Desc: "the full plan as markdown, replacing any previous draft"},
		}),
	}, nil
}

func (t *proposePlanTool) InvokableRun(_ context.Context, args string, _ ...tool.Option) (string, error) {
	var a struct {
		Markdown string `json:"markdown"`
	}
	if s := strings.TrimSpace(args); s != "" && s != "{}" {
		if err := json.Unmarshal([]byte(args), &a); err != nil {
			return askToolFailure("could not read the arguments: %v", err), nil
		}
	}
	out, err := t.propose(a.Markdown)
	if err != nil {
		return askToolFailure("%s", err.Error()), nil
	}
	if strings.TrimSpace(out) == "" {
		return `{"ok":true}`, nil
	}
	return out, nil
}
