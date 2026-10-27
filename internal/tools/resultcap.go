package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	einoexec "github.com/LubyRuffy/eino-tools/exec"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// resultCap bounds the text one tool call hands the model. A tool result stays
// in the conversation, so an oversized one is paid for again on every later
// model call of the turn, not once.
type resultCap struct {
	inner tool.InvokableTool
	limit int
	exec  bool
}

func wrapResultCap(t tool.BaseTool, limit int) tool.BaseTool {
	inv, ok := t.(tool.InvokableTool)
	if !ok || limit <= 0 {
		return t
	}
	info, _ := t.Info(context.Background())
	return &resultCap{inner: inv, limit: limit, exec: info != nil && info.Name == einoexec.ToolName}
}

func (c *resultCap) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return c.inner.Info(ctx)
}

func (c *resultCap) InvokableRun(ctx context.Context, args string, opts ...tool.Option) (string, error) {
	out, err := c.inner.InvokableRun(ctx, args, opts...)
	if c.exec {
		return capExecResult(out, c.limit), err
	}
	return capToolResult(out, c.limit), err
}

// capExecResult shrinks text inside exec's flat JSON envelope, not the encoded
// document. Keep status/error metadata intact, even when a tiny configured cap
// cannot hold that envelope. The full command is also in the call's arguments;
// only shorten its result copy if metadata alone would otherwise exceed the cap.
func capExecResult(out string, limit int) string {
	if len(out) <= limit || utf8.RuneCountInString(out) <= limit {
		return out
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(out), &fields) != nil || fields == nil {
		return capToolResult(out, limit)
	}
	fields["truncated"] = json.RawMessage("true")
	type textField struct {
		key   string
		runes []rune
	}
	var texts []textField
	add := func(key string) {
		var text string
		if json.Unmarshal(fields[key], &text) == nil {
			texts = append(texts, textField{key: key, runes: []rune(text)})
		}
	}
	add("stdout")
	add("stderr")
	encode := func(keep int) []byte {
		for _, field := range texts {
			fields[field.key], _ = json.Marshal(clipExecText(field.runes, keep))
		}
		encoded, _ := json.Marshal(fields)
		return encoded
	}
	minimum := encode(0)
	if utf8.RuneCount(minimum) > limit {
		add("full_command")
		minimum = encode(0)
	}
	// Status, error and path fields form the soft minimum for exec. They must
	// not disappear or turn a failed command into an unparseable text fragment.
	limit = max(limit, utf8.RuneCount(minimum))
	best := minimum
	upper := 0
	for _, field := range texts {
		upper = max(upper, len(field.runes))
	}
	// Measure actual encoded characters: quotes, controls and HTML characters
	// expand in JSON. A shared text allowance keeps both streams represented.
	for lower := 0; lower <= upper; {
		keep := lower + (upper-lower)/2
		candidate := encode(keep)
		if utf8.RuneCount(candidate) <= limit {
			best = candidate
			lower = keep + 1
		} else {
			upper = keep - 1
		}
	}
	return string(best)
}

func clipExecText(text []rune, keep int) string {
	if len(text) <= keep {
		return string(text)
	}
	if keep == 0 {
		return ""
	}
	const marker = "\n[output truncated]\n"
	if keep < len(marker) {
		return "…"
	}
	budget := keep - len(marker)
	head := budget * 2 / 3
	return string(text[:head]) + marker + string(text[len(text)-(budget-head):])
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
// truncated. The outer cap then accounts for the complete JSON encoding.
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
// characters before JSON escaping; capExecResult handles the encoded size.
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
