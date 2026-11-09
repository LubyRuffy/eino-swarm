package tools

import (
	"context"
	"encoding/json"
	"sync"

	einoexec "github.com/LubyRuffy/eino-tools/exec"
)

// liveExecNotifyLimit matches swarm.toolResultNotifyLimit: the live event is
// a UI copy, not a second filesystem.
const liveExecNotifyLimit = 64_000

type toolOutputEmitterKey struct{}

// BindToolOutput supplies the call's live output emitter to tools that opt in.
func BindToolOutput(ctx context.Context, emit func(string), toolName, callID string) context.Context {
	if emit == nil {
		return ctx
	}
	ctx = context.WithValue(ctx, toolOutputEmitterKey{}, emit)
	return BindExecOutput(ctx, emit, toolName, callID)
}

// ToolOutputEmitter returns the UI-only output sink for the current invocation.
func ToolOutputEmitter(ctx context.Context) func(string) {
	emit, _ := ctx.Value(toolOutputEmitterKey{}).(func(string))
	return emit
}

// BindExecOutput attaches exec's stdout/stderr listener so the host can emit
// NotifyToolDelta while the command still runs. Other tools are left alone.
func BindExecOutput(ctx context.Context, emit func(string), toolName, callID string) context.Context {
	if emit == nil || toolName != einoexec.ToolName {
		return ctx
	}
	var mu sync.Mutex
	var stdout, stderr string
	return einoexec.WithOutputListener(ctx, func(stream string, chunk []byte) {
		mu.Lock()
		if stream == "stderr" {
			stderr += string(chunk)
		} else {
			stdout += string(chunk)
		}
		text := liveExecJSON(stdout, stderr)
		mu.Unlock()
		emit(text)
	})
}

func liveExecJSON(stdout, stderr string) string {
	payload, _ := json.Marshal(map[string]string{
		"stdout": clipRunes(stdout, liveExecNotifyLimit),
		"stderr": clipRunes(stderr, liveExecNotifyLimit),
	})
	return string(payload)
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if n > 0 && len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
