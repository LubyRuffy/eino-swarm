package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// resultCap bounds the text one tool call hands the model. A tool result stays
// in the conversation, so an oversized one is paid for again on every later
// model call of the turn, not once.
type resultCap struct {
	inner tool.InvokableTool
	limit int
}

func wrapResultCap(t tool.BaseTool, limit int) tool.BaseTool {
	inv, ok := t.(tool.InvokableTool)
	if !ok || limit <= 0 {
		return t
	}
	return &resultCap{inner: inv, limit: limit}
}

func (c *resultCap) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return c.inner.Info(ctx)
}

func (c *resultCap) InvokableRun(ctx context.Context, args string, opts ...tool.Option) (string, error) {
	out, err := c.inner.InvokableRun(ctx, args, opts...)
	return capToolResult(out, c.limit), err
}

// capToolResult keeps the head and the tail: a listing's first entries and a
// command's last lines (where its error usually is) both survive.
func capToolResult(s string, limit int) string {
	if len(s) <= limit || utf8.RuneCountInString(s) <= limit {
		return s
	}
	r := []rune(s)
	head := limit * 2 / 3
	tail := limit - head
	return string(r[:head]) +
		fmt.Sprintf("\n\n[tool result truncated: %d of %d characters omitted here. Narrow the path or pattern, filter the command's output, or read a range with offset and limit.]\n\n", len(r)-head-tail, len(r)) +
		string(r[len(r)-tail:])
}

// execOutputBudget gives exec a per-stream byte budget derived from the result
// cap when the call names none, so exec clips a noisy stream itself and flags
// truncated, instead of the generic cap cutting through its JSON.
type execOutputBudget struct {
	inner tool.InvokableTool
	kb    int
}

func wrapExecOutputBudget(inner tool.InvokableTool, limit int) tool.InvokableTool {
	return &execOutputBudget{inner: inner, kb: execOutputKB(limit)}
}

func (g *execOutputBudget) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return g.inner.Info(ctx)
}

func (g *execOutputBudget) InvokableRun(ctx context.Context, args string, opts ...tool.Option) (string, error) {
	return g.inner.InvokableRun(ctx, withDefaultExecOutput(args, g.kb), opts...)
}

// execOutputKB splits the cap across stdout and stderr. Bytes never undercount
// characters, so both full streams still fit the cap before JSON escaping.
func execOutputKB(limit int) int {
	if kb := limit / 2 / 1024; kb > 0 {
		return kb
	}
	return 1
}

func withDefaultExecOutput(args string, kb int) string {
	var fields map[string]any
	if json.Unmarshal([]byte(args), &fields) != nil || fields == nil {
		return args
	}
	if _, ok := fields["max_output_kb"]; ok {
		return args
	}
	fields["max_output_kb"] = kb
	// Values decoded from JSON always re-encode.
	out, _ := json.Marshal(fields)
	return string(out)
}
