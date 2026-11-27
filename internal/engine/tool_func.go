package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// funcTool adapts a Go function to an Eino tool. A failed run becomes a tool
// result the model can read instead of an error that ends the turn.
type funcTool struct {
	name        string
	description string
	params      map[string]*schema.ParameterInfo
	run         func(context.Context, string) (string, error)
}

func (t *funcTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: t.name, Desc: t.description, ParamsOneOf: schema.NewParamsOneOfByParams(t.params)}, nil
}

func (t *funcTool) InvokableRun(ctx context.Context, args string, _ ...tool.Option) (string, error) {
	out, err := t.run(ctx, args)
	if err != nil {
		return askToolFailure("%s", err), nil
	}
	return out, nil
}

// decodeToolArgs rejects unknown fields and trailing data so a model cannot
// smuggle a field the tool does not define.
func decodeToolArgs(args string, value any) error {
	if strings.TrimSpace(args) == "" {
		args = "{}"
	}
	decoder := json.NewDecoder(strings.NewReader(args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return fmt.Errorf("invalid tool arguments: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("tool arguments must contain one JSON object")
	}
	return nil
}
